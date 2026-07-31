package tokens_test

import (
	"testing"
	"time"

	"github.com/mateuslh/lealing/cmd/tools/token-usage/internal/tokens"
)

// window monta uma janela de 5h já na metade do prazo, com o consumo dado.
func window(usedPercent float64) (tokens.RateWindow, time.Time) {
	now := time.Date(2026, 7, 30, 15, 0, 0, 0, time.UTC)
	return tokens.RateWindow{
		Provider:      "Codex",
		Label:         "5h",
		UsedPercent:   usedPercent,
		WindowMinutes: 300,
		ResetsAt:      now.Add(150 * time.Minute),
		ObservedAt:    now,
	}, now
}

func TestPaceAcompanhaORelogioDaJanela(t *testing.T) {
	w, now := window(50)
	p := w.Pace(now)

	if p.Stage != tokens.PaceOnTrack {
		t.Errorf("metade da cota na metade do prazo devia ser ritmo em dia, veio %v", p.Stage)
	}
	if p.ExpectedPercent != 50 {
		t.Errorf("esperado = %v, queria 50", p.ExpectedPercent)
	}
	if !p.LastsToReset {
		t.Error("no ritmo exato a cota chega ao reset")
	}
}

func TestPaceDetectaDeficitEProjetaOEsgotamento(t *testing.T) {
	w, now := window(90)
	p := w.Pace(now)

	if p.Stage != tokens.PaceDeficit {
		t.Errorf("90%% gastos na metade do prazo é déficit, veio %v", p.Stage)
	}
	if p.DeltaPercent != 40 {
		t.Errorf("delta = %v, queria 40", p.DeltaPercent)
	}
	if p.LastsToReset {
		t.Fatal("nesse ritmo a cota não chega ao reset")
	}
	// Ritmo de 180 pontos por janela: os 10 pontos restantes duram 1/18 de
	// janela, ou seja, pouco menos de 17 minutos.
	if got := p.ETA.Round(time.Minute); got != 17*time.Minute {
		t.Errorf("ETA = %v, queria ~17min", got)
	}
}

func TestPaceDetectaReserva(t *testing.T) {
	w, now := window(10)
	p := w.Pace(now)

	if p.Stage != tokens.PaceReserve {
		t.Errorf("10%% gastos na metade do prazo é reserva, veio %v", p.Stage)
	}
	if !p.LastsToReset {
		t.Error("sobrando cota, ela chega inteira ao reset")
	}
}

// Uma janela cujo reset já passou descreve um ciclo encerrado: extrapolar
// dali produziria um alarme sobre cota que já voltou ao topo.
func TestPaceIgnoraJanelaJaResetada(t *testing.T) {
	w, now := window(95)
	later := now.Add(4 * time.Hour)

	if !w.Expired(later) {
		t.Fatal("janela devia estar expirada")
	}
	if p := w.Pace(later); p.Known() {
		t.Errorf("janela expirada não tem ritmo, veio %+v", p)
	}
	if got := w.ResetsIn(later); got != 0 {
		t.Errorf("ResetsIn = %v, queria 0", got)
	}
}

func TestPaceExigeDuracaoDaJanela(t *testing.T) {
	w := tokens.RateWindow{Provider: "Codex", UsedPercent: 40, ResetsAt: time.Now().Add(time.Hour)}
	if p := w.Pace(time.Now()); p.Known() {
		t.Error("sem window_minutes não há como saber quanto do prazo correu")
	}
}

func TestCreditsMedemQuantoFaltaParaOTeto(t *testing.T) {
	c := tokens.Credits{Provider: "Claude Code", Used: 83.89, Limit: 275, Currency: "BRL", Enabled: true}

	if got := c.Remaining(); got != 275-83.89 {
		t.Errorf("Remaining = %v, queria %v", got, 275-83.89)
	}
	if got := c.UsedPercent(); got < 30.5 || got > 30.6 {
		t.Errorf("UsedPercent = %v, queria ~30.5", got)
	}
}

// Um teto zerado é conta sem crédito contratado, não divisão por zero.
func TestCreditsSemTetoNaoExplodem(t *testing.T) {
	c := tokens.Credits{Used: 10}
	if got := c.UsedPercent(); got != 0 {
		t.Errorf("UsedPercent = %v, queria 0", got)
	}
	if got := c.Remaining(); got != 0 {
		t.Errorf("Remaining = %v, queria 0", got)
	}
}

// Estourar o teto satura em 100%: uma barra além do fim desenharia fora do
// painel.
func TestCreditsSaturamEmCem(t *testing.T) {
	c := tokens.Credits{Used: 400, Limit: 275}
	if got := c.UsedPercent(); got != 100 {
		t.Errorf("UsedPercent = %v, queria 100", got)
	}
	if got := c.Remaining(); got != 0 {
		t.Errorf("Remaining = %v, queria 0", got)
	}
}

func TestRemainingPercentNuncaFicaNegativo(t *testing.T) {
	w := tokens.RateWindow{UsedPercent: 120}
	if got := w.RemainingPercent(); got != 0 {
		t.Errorf("RemainingPercent = %v, queria 0", got)
	}
}
