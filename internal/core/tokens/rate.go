package tokens

import (
	"context"
	"time"
)

// RateWindow é uma cota reportada pela própria CLI (as janelas de 5h e
// semanal do Codex, por exemplo). Diferente de WindowUsage, que é calculada
// por nós a partir dos registros.
type RateWindow struct {
	Provider    string
	Label       string
	UsedPercent float64
	// WindowMinutes é a duração da janela. Sem ela não há como saber quanto
	// do prazo já correu — e é a comparação entre prazo corrido e cota gasta
	// que produz o ritmo.
	WindowMinutes int
	ResetsAt      time.Time
	// ObservedAt é quando a CLI reportou este número. O percentual é uma foto
	// daquele instante: se a sessão terminou há dois dias, o gasto parou ali
	// mas o relógio da janela continuou correndo.
	ObservedAt time.Time
	// Source diz de onde o número veio ("log local", "conta"). Uma leitura
	// tirada do log tem a idade do último uso; uma consulta à conta é de
	// agora, e a tela precisa poder dizer qual é qual.
	Source string
}

// RemainingPercent é quanto ainda resta da cota.
func (w RateWindow) RemainingPercent() float64 {
	if w.UsedPercent >= 100 {
		return 0
	}
	return 100 - w.UsedPercent
}

// Duration é a janela como intervalo.
func (w RateWindow) Duration() time.Duration {
	return time.Duration(w.WindowMinutes) * time.Minute
}

// StartsAt é o começo da janela, deduzido do reset menos a duração.
func (w RateWindow) StartsAt() time.Time {
	if w.ResetsAt.IsZero() || w.WindowMinutes <= 0 {
		return time.Time{}
	}
	return w.ResetsAt.Add(-w.Duration())
}

// Expired informa que a janela já virou desde a última leitura: o percentual
// em mãos descreve um ciclo que não existe mais.
func (w RateWindow) Expired(now time.Time) bool {
	return !w.ResetsAt.IsZero() && now.After(w.ResetsAt)
}

// ResetsIn é quanto falta para a cota reiniciar. Zero quando já reiniciou ou
// quando a CLI não informou o horário.
func (w RateWindow) ResetsIn(now time.Time) time.Duration {
	if w.ResetsAt.IsZero() || !w.ResetsAt.After(now) {
		return 0
	}
	return w.ResetsAt.Sub(now)
}

// Credits é o saldo de uso extra que uma CLI cobra à parte da assinatura —
// o que continua funcionando depois que a cota da janela estoura.
//
// As CLIs contam isso de dois jeitos: uma diz quanto já foi gasto de um teto
// mensal (Used/Limit), outra diz apenas quanto sobra na carteira (Balance).
// Os dois cabem aqui porque a pergunta é a mesma — quanto ainda dá para
// gastar — e só a forma de chegar nela muda.
type Credits struct {
	Provider string
	// Used e Limit vêm na moeda da conta, já convertidos da unidade menor.
	Used  float64
	Limit float64
	// Balance é o saldo disponível, para quem publica carteira em vez de teto.
	Balance float64
	// Currency é o código ISO ("BRL", "USD"); vazio quando a CLI não informa.
	Currency string
	// Enabled distingue "não contratado" de "contratado e zerado": um saldo
	// de zero significa coisas opostas nos dois casos.
	Enabled bool
	// Unlimited marca as contas sem teto, em que percentual não faz sentido.
	Unlimited bool
}

// Metered informa se há teto — e portanto barra de preenchimento. Sem teto, a
// única leitura honesta é o saldo em si.
func (c Credits) Metered() bool { return c.Limit > 0 && !c.Unlimited }

// UsedPercent é a fração do teto já consumida.
func (c Credits) UsedPercent() float64 {
	if c.Limit <= 0 {
		return 0
	}
	return min(c.Used/c.Limit*100, 100)
}

// Remaining é quanto ainda dá para gastar: a sobra do teto para quem tem
// teto, o saldo da carteira para quem não tem.
func (c Credits) Remaining() float64 {
	if !c.Metered() {
		return c.Balance
	}
	return max(c.Limit-c.Used, 0)
}

// CreditReporter é a porta opcional dos provedores que também publicam saldo.
// Fica fora de Provider porque a maioria das CLIs não tem esse conceito, e
// obrigá-las a devolver nil só encheria os adapters de método vazio.
type CreditReporter interface {
	Credits(ctx context.Context) (*Credits, error)
}

// PaceStage classifica o consumo contra o relógio da janela.
type PaceStage uint8

const (
	// PaceUnknown é o que sobra quando falta duração ou horário de reset.
	PaceUnknown PaceStage = iota
	// PaceOnTrack: gasto e prazo andam juntos.
	PaceOnTrack
	// PaceDeficit: gastando mais rápido que o relógio — a cota acaba antes.
	PaceDeficit
	// PaceReserve: gastando mais devagar — sobra cota no reset.
	PaceReserve
)

// paceTolerance é a faixa em que gasto e prazo são considerados equivalentes.
// Sem ela, um desvio de meio ponto percentual já pintaria a tela de alerta.
const paceTolerance = 5.0

// Pace é a leitura de ritmo de uma janela: onde o consumo está em relação a
// onde o relógio está.
type Pace struct {
	Stage PaceStage
	// ExpectedPercent é quanto teria sido gasto num consumo perfeitamente
	// uniforme até agora.
	ExpectedPercent float64
	// DeltaPercent é o desvio: positivo gasta rápido demais, negativo sobra.
	DeltaPercent float64
	// ElapsedPercent é quanto da janela já correu.
	ElapsedPercent float64
	// LastsToReset diz que, mantido o ritmo, a cota chega inteira ao reset.
	LastsToReset bool
	// ETA é quanto falta para zerar a cota no ritmo atual. Só é significativo
	// quando LastsToReset é falso.
	ETA time.Duration
}

// Known informa se há ritmo calculado.
func (p Pace) Known() bool { return p.Stage != PaceUnknown }

// Pace calcula o ritmo da janela no instante dado.
//
// A conta é uma regra de três entre dois eixos: quanto da cota foi gasto e
// quanto do prazo correu. O CodexBar chama isso de deficit/reserve, e é a
// informação que falta num percentual solto — 60% usado é tranquilo na quarta
// hora de uma janela de cinco, e alarmante na primeira.
func (w RateWindow) Pace(now time.Time) Pace {
	start := w.StartsAt()
	if start.IsZero() || w.Expired(now) {
		return Pace{}
	}

	elapsed := now.Sub(start).Seconds() / w.Duration().Seconds()
	elapsed = min(max(elapsed, 0), 1)

	expected := elapsed * 100
	delta := w.UsedPercent - expected

	p := Pace{
		Stage:           PaceOnTrack,
		ExpectedPercent: expected,
		DeltaPercent:    delta,
		ElapsedPercent:  expected,
		LastsToReset:    true,
	}
	switch {
	case delta > paceTolerance:
		p.Stage = PaceDeficit
	case delta < -paceTolerance:
		p.Stage = PaceReserve
	}

	// Projeção: extrapola o gasto médio da fração já corrida sobre o resto da
	// janela. Sem prazo corrido não há média, e uma cota intocada nunca zera.
	if elapsed <= 0 || w.UsedPercent <= 0 {
		return p
	}
	if w.UsedPercent >= 100 {
		p.LastsToReset = false
		return p
	}

	perFraction := w.UsedPercent / elapsed // pontos percentuais por janela inteira
	fractionToEmpty := (100 - w.UsedPercent) / perFraction
	emptyAt := start.Add(time.Duration((elapsed + fractionToEmpty) * w.Duration().Seconds() * float64(time.Second)))

	if !emptyAt.Before(w.ResetsAt) {
		return p
	}
	p.LastsToReset = false
	p.ETA = max(emptyAt.Sub(now), 0)
	return p
}
