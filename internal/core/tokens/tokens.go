// Package tokens é o domínio da tool "Uso de Tokens".
//
// Normaliza o consumo relatado por várias CLIs de IA em um formato só e
// agrega tudo em um relatório. Não conhece disco nem formato de log: cada
// CLI entra por um Provider.
package tokens

import (
	"context"
	"sort"
	"time"
)

// Record é um evento de uso já normalizado, emitido por um Provider.
type Record struct {
	Provider  string
	Model     string
	Day       string // "YYYY-MM-DD", como reportado pela CLI
	Timestamp time.Time
	Project   string

	Input         int // input NÃO cacheado
	Output        int // inclui tokens de raciocínio quando aplicável
	CacheCreation int // escrita de cache (Claude); 0 na OpenAI
	CacheRead     int // leitura de cache / input cacheado
	Cost          float64
}

// Totals são os agregados de um recorte qualquer.
type Totals struct {
	Input         int
	Output        int
	CacheCreation int
	CacheRead     int
	Messages      int
	Cost          float64
}

// TotalTokens soma todas as categorias de token.
func (t Totals) TotalTokens() int {
	return t.Input + t.Output + t.CacheCreation + t.CacheRead
}

// Add acumula um registro.
func (t *Totals) Add(r Record) {
	t.Input += r.Input
	t.Output += r.Output
	t.CacheCreation += r.CacheCreation
	t.CacheRead += r.CacheRead
	t.Cost += r.Cost
	t.Messages++
}

// Slice é um recorte rotulado (por modelo, provedor ou projeto).
type Slice struct {
	Label  string
	Totals Totals
}

// DayPoint é o consumo de um dia, para a série temporal.
type DayPoint struct {
	Date   time.Time
	Day    string
	Totals Totals
}

// WindowUsage é o consumo dentro de uma janela de tempo.
type WindowUsage struct {
	Label string
	// Minutes é a duração de uma janela móvel; zero nos recortes de
	// calendário ("hoje", "este mês"), cujo tamanho depende da data. É o que
	// permite comparar uma janela nossa com a cota que a CLI publica.
	Minutes    int
	Totals     Totals
	ByProvider []Slice
}

// Report é o relatório completo consumido pela tela.
type Report struct {
	Overall     Totals
	ByProvider  []Slice
	ByModel     []Slice
	ByProject   []Slice
	ByDay       []DayPoint // crescente por data
	Providers   []string
	Windows     []WindowUsage
	RateWindows []RateWindow
	Credits     []Credits
	// Errs guarda falhas parciais: um provedor quebrado não pode zerar o
	// relatório dos outros, mas o usuário precisa saber que faltou dado.
	Errs []error
}

// HasData informa se houve qualquer consumo registrado.
func (r Report) HasData() bool { return r.Overall.Messages > 0 }

// Provider é a porta de saída: uma CLI de IA que sabe relatar seu consumo.
type Provider interface {
	Name() string
	Collect(ctx context.Context) ([]Record, error)
	// RateWindows devolve as cotas que a CLI reporta. Provedores que não
	// expõem cota devolvem nil.
	RateWindows(ctx context.Context) ([]RateWindow, error)
}

// Service agrega os provedores em um relatório.
type Service struct {
	providers []Provider
	now       func() time.Time
}

// NewService monta o serviço. Um now nil usa o relógio do sistema.
func NewService(now func() time.Time, providers ...Provider) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{providers: providers, now: now}
}

// Generate coleta de todos os provedores e monta o relatório.
//
// Um provedor que falha não interrompe os demais: o erro é anexado ao
// relatório e o resto do painel continua útil.
func (s *Service) Generate(ctx context.Context) (Report, error) {
	var (
		records []Record
		rates   []RateWindow
		credits []Credits
		errs    []error
	)

	for _, p := range s.providers {
		recs, err := p.Collect(ctx)
		if err != nil {
			errs = append(errs, err)
		}
		records = append(records, recs...)

		windows, err := p.RateWindows(ctx)
		if err != nil {
			errs = append(errs, err)
		}
		rates = append(rates, windows...)

		if reporter, ok := p.(CreditReporter); ok {
			balance, err := reporter.Credits(ctx)
			if err != nil {
				errs = append(errs, err)
			}
			if balance != nil {
				credits = append(credits, *balance)
			}
		}

		if ctx.Err() != nil {
			return Report{Errs: append(errs, ctx.Err())}, ctx.Err()
		}
	}

	report := s.aggregate(records)
	report.RateWindows = rates
	report.Credits = credits
	report.Errs = errs
	return report, nil
}

// aggregate cruza os registros em todos os recortes do relatório.
func (s *Service) aggregate(records []Record) Report {
	var report Report
	if len(records) == 0 {
		return report
	}

	byProvider := map[string]*Totals{}
	byModel := map[string]*Totals{}
	byProject := map[string]*Totals{}
	byDay := map[string]*Totals{}

	for _, r := range records {
		report.Overall.Add(r)
		accumulate(byProvider, r.Provider, r)
		accumulate(byModel, r.Model, r)
		accumulate(byProject, r.Project, r)
		if r.Day != "" {
			accumulate(byDay, r.Day, r)
		}
	}

	report.ByProvider = slicesByCost(byProvider)
	report.ByModel = slicesByCost(byModel)
	report.ByProject = slicesByCost(byProject)

	report.Providers = make([]string, len(report.ByProvider))
	for i, sl := range report.ByProvider {
		report.Providers[i] = sl.Label
	}

	for day, totals := range byDay {
		date, err := time.ParseInLocation(time.DateOnly, day, time.UTC)
		if err != nil {
			continue
		}
		report.ByDay = append(report.ByDay, DayPoint{Date: date, Day: day, Totals: *totals})
	}
	sort.Slice(report.ByDay, func(i, j int) bool {
		return report.ByDay[i].Date.Before(report.ByDay[j].Date)
	})

	report.Windows = s.computeWindows(records)
	return report
}

// computeWindows recorta o consumo em janelas móveis de tempo.
func (s *Service) computeWindows(records []Record) []WindowUsage {
	now := s.now()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	defs := []struct {
		label   string
		since   time.Time
		minutes int
	}{
		{"Últimas 5h", now.Add(-5 * time.Hour), 300},
		{"Hoje", startOfToday, 0},
		{"7 dias", now.AddDate(0, 0, -7), 7 * 24 * 60},
		{"Este mês", startOfMonth, 0},
	}

	out := make([]WindowUsage, len(defs))
	for i, def := range defs {
		var totals Totals
		byProvider := map[string]*Totals{}
		for _, r := range records {
			if r.Timestamp.IsZero() || r.Timestamp.Before(def.since) {
				continue
			}
			totals.Add(r)
			accumulate(byProvider, r.Provider, r)
		}
		out[i] = WindowUsage{
			Label:      def.label,
			Minutes:    def.minutes,
			Totals:     totals,
			ByProvider: slicesByCost(byProvider),
		}
	}
	return out
}

// accumulate soma um registro no balde da chave, criando-o se preciso.
func accumulate(m map[string]*Totals, key string, r Record) {
	t, ok := m[key]
	if !ok {
		t = &Totals{}
		m[key] = t
	}
	t.Add(r)
}

// slicesByCost converte o mapa em fatias ordenadas por custo decrescente.
// O desempate por rótulo mantém a saída estável entre execuções — sem isso,
// dois modelos de custo zero trocariam de lugar a cada atualização.
func slicesByCost(m map[string]*Totals) []Slice {
	out := make([]Slice, 0, len(m))
	for label, totals := range m {
		out = append(out, Slice{Label: label, Totals: *totals})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Totals.Cost != out[j].Totals.Cost {
			return out[i].Totals.Cost > out[j].Totals.Cost
		}
		return out[i].Label < out[j].Label
	})
	return out
}
