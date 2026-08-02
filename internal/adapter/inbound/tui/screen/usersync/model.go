// Package usersync implementa a tela de conta e sincronização.
//
// A tela conhece apenas a porta de entrada do caso de uso: device flow, API
// do GitHub e cofre ficam do outro lado. Todo I/O acontece dentro de tea.Cmd,
// porque o polling do login dura minutos e não pode congelar o frame.
package usersync

import (
	"context"
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/core/hostaction"
	core "github.com/mateuslh/lealing/internal/core/usersync"
)

// ScreenID é o identificador da tela, também usado pelo catálogo.
const ScreenID tui.ScreenID = "tool/account-sync"

// phase é o que a tela está fazendo.
type phase int

const (
	phaseLoading phase = iota
	phaseReady
	// phaseAwaiting mostra o código e espera a aprovação no browser.
	phaseAwaiting
	phaseWorking
)

// pending descreve uma operação que esbarrou em divergência e aguarda o
// usuário decidir sobrescrever.
type pending struct {
	push bool
	// remote descreve o que está do outro lado, para a pergunta não ser
	// abstrata.
	remote core.State
}

type Model struct {
	deps    tui.Deps
	manager core.Manager
	host    hostaction.Actions
	now     func() time.Time

	phase  phase
	status core.Status
	code   core.DeviceCode

	cursor  int
	confirm *pending

	err     error
	message string
	success bool
}

var _ tui.Screen = (*Model)(nil)

func New(deps tui.Deps, manager core.Manager, host hostaction.Actions, now func() time.Time) *Model {
	if now == nil {
		now = time.Now
	}
	return &Model{deps: deps, manager: manager, host: host, now: now, phase: phaseLoading}
}

func (*Model) ID() tui.ScreenID { return ScreenID }
func (*Model) Title() string    { return "conta e sincronização" }

func (m *Model) Init() tea.Cmd { return m.load() }

// Refresh recarrega ao voltar de outra tela: favoritos podem ter mudado no
// meio-tempo, e o painel compara local com remoto.
func (m *Model) Refresh() tea.Cmd { return m.load() }

type statusMsg struct {
	status core.Status
	err    error
}

type deviceMsg struct {
	code core.DeviceCode
	err  error
}

type loggedMsg struct {
	identity core.Identity
	err      error
}

type syncMsg struct {
	result core.Result
	push   bool
	err    error
	// remote carrega o estado do outro lado quando houve divergência, para a
	// confirmação dizer o que está em jogo.
	remote core.State
}

type actionMsg struct {
	message string
	err     error
}

func (m *Model) load() tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		if manager == nil {
			return statusMsg{err: errDisabled}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		status, err := manager.Status(ctx)
		return statusMsg{status: status, err: err}
	}
}

func (m *Model) startLogin() tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		code, err := manager.StartLogin(ctx)
		return deviceMsg{code: code, err: err}
	}
}

// waitLogin espera a aprovação. O timeout acompanha a validade do código:
// encerrar antes deixaria o usuário aprovando algo que ninguém mais escuta.
func (m *Model) waitLogin(code core.DeviceCode) tea.Cmd {
	manager := m.manager
	deadline := code.ExpiresAt
	return func() tea.Msg {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		defer cancel()
		identity, err := manager.CompleteLogin(ctx, code)
		return loggedMsg{identity: identity, err: err}
	}
}

func (m *Model) logout() tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := manager.Logout(ctx); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{message: "conta desconectada desta máquina"}
	}
}

func (m *Model) sync(push, force bool) tea.Cmd {
	manager, remote := m.manager, m.status.Remote
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		var result core.Result
		var err error
		if push {
			result, err = manager.Push(ctx, force)
		} else {
			result, err = manager.Pull(ctx, force)
		}
		return syncMsg{result: result, push: push, err: err, remote: remote}
	}
}

func (m *Model) toggleSection(section core.Section, enabled bool) tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := manager.SetSection(ctx, section, enabled); err != nil {
			return actionMsg{err: err}
		}
		state := "sincronizada"
		if !enabled {
			state = "fora da sincronização"
		}
		return actionMsg{message: section.Label() + " " + state}
	}
}

// copyCode e openBrowser existem porque digitar oito caracteres em outro
// dispositivo é o ponto do fluxo em que as pessoas erram.
func (m *Model) copyCode() tea.Cmd {
	host, code := m.host, m.code.UserCode
	return func() tea.Msg {
		if host == nil {
			return actionMsg{err: errors.New("clipboard indisponível nesta plataforma")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := host.WriteClipboard(ctx, code); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{message: "código copiado"}
	}
}

func (m *Model) openBrowser() tea.Cmd {
	host, target := m.host, m.code.VerificationURL
	return func() tea.Msg {
		if host == nil {
			return actionMsg{err: errors.New("abrir o browser não está disponível nesta plataforma")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := host.OpenBrowser(ctx, target); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{message: "browser aberto em " + target}
	}
}

func (m *Model) currentSection() (core.Section, bool) {
	if m.cursor < 0 || m.cursor >= len(core.AllSections) {
		return "", false
	}
	return core.AllSections[m.cursor], true
}
