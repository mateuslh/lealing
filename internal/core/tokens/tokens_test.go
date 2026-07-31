package tokens_test

import (
	"context"
	"testing"
	"time"

	"github.com/mateuslh/lealing/internal/core/tokens"
)

var now = time.Date(2026, 7, 30, 15, 0, 0, 0, time.UTC)

func clock() time.Time { return now }

// fakeProvider devolve registros fixos e, opcionalmente, um erro.
type fakeProvider struct {
	name    string
	records []tokens.Record
	windows []tokens.RateWindow
	err     error
}

func (p fakeProvider) Name() string { return p.name }

func (p fakeProvider) Collect(context.Context) ([]tokens.Record, error) {
	return p.records, p.err
}

func (p fakeProvider) RateWindows(context.Context) ([]tokens.RateWindow, error) {
	return p.windows, nil
}

// creditProvider também publica saldo, como as CLIs que cobram uso extra.
type creditProvider struct {
	fakeProvider
	credits tokens.Credits
}

func (p creditProvider) Credits(context.Context) (*tokens.Credits, error) {
	return &p.credits, nil
}

// Só quem implementa CreditReporter é consultado — os outros provedores não
// precisam ganhar um método vazio por causa disso.
func TestGenerateColetaSaldoDeQuemPublica(t *testing.T) {
	svc := tokens.NewService(clock,
		fakeProvider{name: "sem saldo"},
		creditProvider{
			fakeProvider: fakeProvider{name: "com saldo"},
			credits:      tokens.Credits{Provider: "com saldo", Used: 10, Limit: 50, Enabled: true},
		},
	)

	report, err := svc.Generate(context.Background())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(report.Credits) != 1 {
		t.Fatalf("%d saldos, queria 1", len(report.Credits))
	}
	if got := report.Credits[0].Remaining(); got != 40 {
		t.Errorf("saldo restante = %v, queria 40", got)
	}
}

func record(provider, model, project string, at time.Time, in, out int, cost float64) tokens.Record {
	return tokens.Record{
		Provider: provider, Model: model, Project: project,
		Day: at.Format(time.DateOnly), Timestamp: at,
		Input: in, Output: out, Cost: cost,
	}
}

func TestGenerateAgregaTodosOsRecortes(t *testing.T) {
	svc := tokens.NewService(clock,
		fakeProvider{name: "A", records: []tokens.Record{
			record("A", "opus", "proj1", now.Add(-time.Hour), 100, 50, 3),
			record("A", "sonnet", "proj2", now.Add(-2*time.Hour), 200, 80, 1),
		}},
		fakeProvider{name: "B", records: []tokens.Record{
			record("B", "gpt", "proj1", now.Add(-30*time.Minute), 10, 5, 0.5),
		}},
	)

	report, err := svc.Generate(context.Background())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if report.Overall.Messages != 3 {
		t.Errorf("mensagens = %d, quero 3", report.Overall.Messages)
	}
	if got := report.Overall.Cost; got != 4.5 {
		t.Errorf("custo = %v, quero 4.5", got)
	}
	if got := report.Overall.TotalTokens(); got != 445 {
		t.Errorf("tokens = %d, quero 445", got)
	}

	// Provedores vêm ordenados por custo decrescente.
	if len(report.ByProvider) != 2 || report.ByProvider[0].Label != "A" {
		t.Errorf("byProvider = %+v, quero A primeiro", report.ByProvider)
	}
	if len(report.ByModel) != 3 {
		t.Errorf("modelos = %d, quero 3", len(report.ByModel))
	}
	// proj1 recebeu registros de dois provedores e deve aparecer uma vez só.
	if len(report.ByProject) != 2 {
		t.Errorf("projetos = %d, quero 2", len(report.ByProject))
	}
}

func TestGenerateJanelasMoveis(t *testing.T) {
	svc := tokens.NewService(clock, fakeProvider{name: "A", records: []tokens.Record{
		record("A", "opus", "p", now.Add(-time.Hour), 10, 10, 1),    // 5h, hoje, 7d, mês
		record("A", "opus", "p", now.Add(-10*time.Hour), 10, 10, 2), // hoje, 7d, mês
		record("A", "opus", "p", now.AddDate(0, 0, -3), 10, 10, 4),  // 7d, mês
		record("A", "opus", "p", now.AddDate(0, 0, -60), 10, 10, 8), // nenhuma
	}})

	report, err := svc.Generate(context.Background())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	want := map[string]float64{
		"Últimas 5h": 1,
		"Hoje":       3,
		"7 dias":     7,
		"Este mês":   7,
	}
	for _, w := range report.Windows {
		if got := want[w.Label]; w.Totals.Cost != got {
			t.Errorf("janela %q = %v, quero %v", w.Label, w.Totals.Cost, got)
		}
	}
}

func TestGenerateSobreviveAProvedorQuebrado(t *testing.T) {
	boom := fakeProvider{name: "quebrado", err: errBoom}
	ok := fakeProvider{name: "ok", records: []tokens.Record{
		record("ok", "opus", "p", now, 10, 10, 2),
	}}

	report, err := tokens.NewService(clock, boom, ok).Generate(context.Background())
	if err != nil {
		t.Fatalf("um provedor com erro não pode derrubar o relatório: %v", err)
	}
	if report.Overall.Messages != 1 {
		t.Errorf("mensagens = %d, quero 1 (o provedor bom)", report.Overall.Messages)
	}
	if len(report.Errs) != 1 {
		t.Errorf("erros = %d, quero 1 registrado", len(report.Errs))
	}
}

func TestGenerateSemDados(t *testing.T) {
	report, err := tokens.NewService(clock).Generate(context.Background())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if report.HasData() {
		t.Error("HasData = true sem provedores")
	}
	if len(report.ByDay) != 0 || len(report.ByModel) != 0 {
		t.Error("relatório vazio devolveu recortes preenchidos")
	}
}

func TestByDayOrdenadoCrescente(t *testing.T) {
	svc := tokens.NewService(clock, fakeProvider{name: "A", records: []tokens.Record{
		record("A", "opus", "p", now.AddDate(0, 0, -1), 1, 1, 1),
		record("A", "opus", "p", now.AddDate(0, 0, -5), 1, 1, 1),
		record("A", "opus", "p", now, 1, 1, 1),
	}})

	report, _ := svc.Generate(context.Background())
	if len(report.ByDay) != 3 {
		t.Fatalf("dias = %d, quero 3", len(report.ByDay))
	}
	for i := 1; i < len(report.ByDay); i++ {
		if !report.ByDay[i-1].Date.Before(report.ByDay[i].Date) {
			t.Errorf("série fora de ordem em %d: %s antes de %s",
				i, report.ByDay[i-1].Day, report.ByDay[i].Day)
		}
	}
}

func TestPricing(t *testing.T) {
	t.Run("modelo conhecido usa a tabela", func(t *testing.T) {
		// 1M de input em claude-sonnet-5 ($3/M) = $3.
		if got := tokens.Cost("claude-sonnet-5", 1_000_000, 0, 0, 0); got != 3 {
			t.Errorf("custo = %v, quero 3", got)
		}
	})

	t.Run("modelo desconhecido cai na família", func(t *testing.T) {
		// Um opus que ainda não está na tabela precisa estimar, não zerar.
		got, ok := tokens.RateFor("claude-opus-9-9")
		if !ok || got.Input != 5 {
			t.Errorf("RateFor = %+v (%v), quero a família opus", got, ok)
		}
	})

	t.Run("modelo irreconhecível não inventa custo", func(t *testing.T) {
		if _, ok := tokens.RateFor("llama-3"); ok {
			t.Error("RateFor aceitou um modelo fora das famílias conhecidas")
		}
		if got := tokens.Cost("llama-3", 1_000_000, 1_000_000, 0, 0); got != 0 {
			t.Errorf("custo = %v, quero 0", got)
		}
	})

	t.Run("cache tem multiplicadores próprios", func(t *testing.T) {
		// input $3/M: escrita 1,25× = $3.75/M, leitura 0,1× = $0.30/M.
		write := tokens.Cost("claude-sonnet-5", 0, 0, 1_000_000, 0)
		read := tokens.Cost("claude-sonnet-5", 0, 0, 0, 1_000_000)
		if write != 3.75 {
			t.Errorf("escrita de cache = %v, quero 3.75", write)
		}
		if read != 0.30000000000000004 && read != 0.3 {
			t.Errorf("leitura de cache = %v, quero 0.3", read)
		}
	})
}

func TestTotalsAdd(t *testing.T) {
	var totals tokens.Totals
	totals.Add(tokens.Record{Input: 10, Output: 20, CacheCreation: 5, CacheRead: 7, Cost: 1.5})
	totals.Add(tokens.Record{Input: 1, Output: 2, Cost: 0.5})

	if totals.Messages != 2 {
		t.Errorf("mensagens = %d, quero 2", totals.Messages)
	}
	if totals.TotalTokens() != 45 {
		t.Errorf("tokens = %d, quero 45", totals.TotalTokens())
	}
	if totals.Cost != 2 {
		t.Errorf("custo = %v, quero 2", totals.Cost)
	}
}

func TestRateWindowRemaining(t *testing.T) {
	tests := map[float64]float64{0: 100, 42: 58, 100: 0, 130: 0}
	for used, want := range tests {
		got := tokens.RateWindow{UsedPercent: used}.RemainingPercent()
		if got != want {
			t.Errorf("usado %v%% → restante %v%%, quero %v%%", used, got, want)
		}
	}
}

var errBoom = errTest("provedor indisponível")

type errTest string

func (e errTest) Error() string { return string(e) }
