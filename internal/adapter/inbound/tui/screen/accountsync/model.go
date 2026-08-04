// Package accountsync implementa a administração da conta e da sincronização
// dentro da configuração da engine.
package accountsync

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/core/hostaction"
	"github.com/mateuslh/lealing/internal/core/usersync"
)

const ScreenID tui.ScreenID = "engine/settings/sync"

const (
	statusTimeout     = 20 * time.Second
	requestTimeout    = 30 * time.Second
	operationTimeout  = 10 * time.Minute
	maxLoginWait      = 20 * time.Minute
	hostActionTimeout = 5 * time.Second
)

type operation uint8

const (
	operationPush operation = iota + 1
	operationPull
	operationLogout
)

type confirmation struct {
	operation operation
	force     bool
	// acceptPermissions só se aplica a operationPull: sinaliza que o usuário
	// já viu e aprovou a ampliação de permissão mostrada por
	// permissionEscalationConfirmation.
	acceptPermissions bool
	title             string
	message           string
}

type Model struct {
	deps    tui.Deps
	manager usersync.Manager
	actions hostaction.Actions

	status usersync.Status
	loaded bool
	cursor int

	busy    string
	err     error
	message string

	device          *usersync.DeviceCode
	deviceFeedback  string
	deviceActionErr error
	clipboardCopied bool
	loginAttempt    uint64
	cancelIO        context.CancelFunc
	confirmation    *confirmation
}

var _ tui.Screen = (*Model)(nil)

func New(deps tui.Deps, manager usersync.Manager, actions hostaction.Actions) *Model {
	return &Model{deps: deps, manager: manager, actions: actions}
}

func (*Model) ID() tui.ScreenID { return ScreenID }
func (*Model) Title() string    { return "sincronização" }

func (m *Model) Init() tea.Cmd {
	m.busy = "consultando esta máquina e o GitHub…"
	return m.loadStatus()
}

// Refresh permite recarregar o retrato se esta tela voltar a ficar visível.
func (m *Model) Refresh() tea.Cmd {
	m.busy = "atualizando sincronização…"
	return m.loadStatus()
}

// Capturing faz esc cancelar primeiro o device flow ou a confirmação, sem
// sair da tela e deixar uma operação ambígua em segundo plano.
func (m *Model) Capturing() bool {
	return m.confirmation != nil || m.device != nil
}

// Close interrompe o polling do device flow quando o router remove a tela.
func (m *Model) Close() tea.Cmd {
	cancel := m.cancelIO
	m.cancelIO = nil
	if cancel == nil {
		return nil
	}
	return func() tea.Msg {
		cancel()
		return nil
	}
}

type statusMsg struct {
	status usersync.Status
	err    error
}

type loginCodeMsg struct {
	attempt uint64
	code    usersync.DeviceCode
	err     error
}

type loginFinishedMsg struct {
	attempt  uint64
	identity usersync.Identity
	err      error
}

type deviceAction uint8

const (
	deviceActionCopy deviceAction = iota + 1
	deviceActionOpen
)

type deviceActionMsg struct {
	attempt uint64
	action  deviceAction
	err     error
}

type sectionMsg struct {
	section usersync.Section
	enabled bool
	err     error
}

type syncMsg struct {
	operation operation
	force     bool
	result    usersync.Result
	err       error
}

type logoutMsg struct{ err error }

func (m *Model) loadStatus() tea.Cmd {
	manager := m.manager
	ctx, cancel := m.timeoutContext(statusTimeout)
	return func() tea.Msg {
		defer cancel()
		if manager == nil {
			return statusMsg{err: errNotConfigured}
		}
		status, err := manager.Status(ctx)
		return statusMsg{status: status, err: err}
	}
}

func (m *Model) beginLogin() tea.Cmd {
	m.loginAttempt++
	attempt := m.loginAttempt
	ctx, cancel := m.timeoutContext(requestTimeout)
	manager := m.manager
	return func() tea.Msg {
		defer cancel()
		code, err := manager.StartLogin(ctx)
		return loginCodeMsg{attempt: attempt, code: code, err: err}
	}
}

func (m *Model) awaitLogin(code usersync.DeviceCode, attempt uint64) tea.Cmd {
	wait := maxLoginWait
	if !code.ExpiresAt.IsZero() {
		wait = time.Until(code.ExpiresAt)
		if wait <= 0 {
			wait = time.Second
		}
		wait = min(wait, maxLoginWait)
	}
	ctx, cancel := m.timeoutContext(wait)
	manager := m.manager
	return func() tea.Msg {
		defer cancel()
		identity, err := manager.CompleteLogin(ctx, code)
		return loginFinishedMsg{attempt: attempt, identity: identity, err: err}
	}
}

func (m *Model) copyDeviceCode(code string, attempt uint64) tea.Cmd {
	actions := m.actions
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), hostActionTimeout)
		defer cancel()
		if actions == nil {
			return deviceActionMsg{attempt: attempt, action: deviceActionCopy, err: errHostActionsUnavailable}
		}
		err := actions.WriteClipboard(ctx, code)
		return deviceActionMsg{attempt: attempt, action: deviceActionCopy, err: err}
	}
}

func (m *Model) openLoginURL(target string, attempt uint64) tea.Cmd {
	actions := m.actions
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), hostActionTimeout)
		defer cancel()
		if actions == nil {
			return deviceActionMsg{attempt: attempt, action: deviceActionOpen, err: errHostActionsUnavailable}
		}
		err := actions.OpenBrowser(ctx, target)
		return deviceActionMsg{attempt: attempt, action: deviceActionOpen, err: err}
	}
}

func (m *Model) setSection(section usersync.Section, enabled bool) tea.Cmd {
	manager := m.manager
	ctx, cancel := m.timeoutContext(statusTimeout)
	return func() tea.Msg {
		defer cancel()
		err := manager.SetSection(ctx, section, enabled)
		return sectionMsg{section: section, enabled: enabled, err: err}
	}
}

func (m *Model) synchronize(selected operation, force, acceptPermissions bool) tea.Cmd {
	manager := m.manager
	ctx, cancel := m.timeoutContext(operationTimeout)
	return func() tea.Msg {
		defer cancel()
		var result usersync.Result
		var err error
		if selected == operationPush {
			result, err = manager.Push(ctx, force)
		} else {
			result, err = manager.Pull(ctx, force, acceptPermissions)
		}
		return syncMsg{operation: selected, force: force, result: result, err: err}
	}
}

func (m *Model) logout() tea.Cmd {
	manager := m.manager
	ctx, cancel := m.timeoutContext(statusTimeout)
	return func() tea.Msg {
		defer cancel()
		return logoutMsg{err: manager.Logout(ctx)}
	}
}

func (m *Model) timeoutContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if m.cancelIO != nil {
		m.cancelIO()
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	m.cancelIO = cancel
	return ctx, cancel
}

func (m *Model) rowCount() int {
	if !m.status.Connected {
		return 1
	}
	return len(usersync.AllSections) + 3
}

func clamp(value, length int) int {
	if value < 0 || length == 0 {
		return 0
	}
	return min(value, length-1)
}
