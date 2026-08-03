// Package update implementa a administração da atualização do lealing
// dentro da configuração da engine.
package update

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/core/selfupdate"
)

// ScreenID identifica esta tela.
const ScreenID tui.ScreenID = "engine/settings/update"

// Timeouts das duas operações. Verificar é uma requisição HTTP; aplicar pode
// ser um download de alguns MB ou um `go build` completo, e por isso tem uma
// folga muito maior.
const (
	checkTimeout = 20 * time.Second
	applyTimeout = 10 * time.Minute
)

// phase é o estado do fluxo da tela.
type phase uint8

const (
	phaseChecking phase = iota
	phaseReady
	phaseApplying
	phaseDone
)

// Model é o estado da tela.
type Model struct {
	deps    tui.Deps
	manager selfupdate.Manager
	home    string
	now     func() time.Time

	phase   phase
	status  selfupdate.Status
	outcome selfupdate.Outcome
	err     error
}

var _ tui.Screen = (*Model)(nil)

// New monta a tela com todas as dependências variáveis explícitas.
func New(deps tui.Deps, manager selfupdate.Manager, home string, now func() time.Time) *Model {
	if now == nil {
		now = time.Now
	}
	return &Model{deps: deps, manager: manager, home: home, now: now, phase: phaseChecking}
}

func (*Model) ID() tui.ScreenID { return ScreenID }
func (*Model) Title() string    { return "atualizar" }

func (m *Model) Init() tea.Cmd { return m.check() }

// checkedMsg entrega o resultado da verificação.
type checkedMsg struct {
	status selfupdate.Status
	err    error
}

// appliedMsg entrega o resultado da atualização.
type appliedMsg struct {
	outcome selfupdate.Outcome
	err     error
}

// check consulta a release mais recente fora da thread de render.
func (m *Model) check() tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		if manager == nil {
			return checkedMsg{err: errNotConfigured}
		}
		ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
		defer cancel()
		st, err := manager.Check(ctx)
		return checkedMsg{status: st, err: err}
	}
}

// apply executa a atualização. O status é capturado por valor: a tela não
// pode mudar de estado no meio e fazer o comando aplicar outra coisa.
func (m *Model) apply() tea.Cmd {
	manager, status := m.manager, m.status
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), applyTimeout)
		defer cancel()
		out, err := manager.Apply(ctx, status)
		return appliedMsg{outcome: out, err: err}
	}
}
