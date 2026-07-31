package domain

import "time"

// Args são os parâmetros passados a uma tool na invocação. Mantidos como
// mapa aberto de propósito: o contrato real é da tool, não do catálogo.
type Args map[string]string

// SessionID identifica uma execução em andamento.
type SessionID string

// Phase é o estágio no ciclo de vida de uma execução.
type Phase uint8

const (
	// PhasePending: aceita, ainda não começou.
	PhasePending Phase = iota
	// PhaseRunning: em execução.
	PhaseRunning
	// PhaseSucceeded: terminou com sucesso.
	PhaseSucceeded
	// PhaseFailed: terminou com erro.
	PhaseFailed
	// PhaseCanceled: interrompida pelo usuário.
	PhaseCanceled
)

var phaseNames = [...]string{"pendente", "executando", "concluída", "falhou", "cancelada"}

// String implementa fmt.Stringer.
func (p Phase) String() string {
	if int(p) < len(phaseNames) {
		return phaseNames[p]
	}
	return "desconhecida"
}

// Terminal informa se a fase é final (não haverá mais transições).
func (p Phase) Terminal() bool { return p >= PhaseSucceeded }

// Session é o registro de uma execução. O domínio não sabe *como* a tool
// roda — só acompanha o ciclo de vida reportado pela porta ToolRunner.
type Session struct {
	ID       SessionID
	ToolID   ToolID
	Args     Args
	Phase    Phase
	Started  time.Time
	Finished time.Time
	ExitCode int
	Err      error
}

// Duration devolve o tempo decorrido; para sessões vivas, mede até agora.
func (s Session) Duration(now time.Time) time.Duration {
	if s.Started.IsZero() {
		return 0
	}
	if s.Finished.IsZero() {
		return now.Sub(s.Started)
	}
	return s.Finished.Sub(s.Started)
}

// Highlights é o material da home: os recortes do catálogo que merecem
// destaque antes de qualquer busca.
type Highlights struct {
	Favorites []Match
	Recent    []Match
	Suggested []Match
	// TotalTools e TotalCategories alimentam o cabeçalho.
	TotalTools      int
	TotalCategories int
}
