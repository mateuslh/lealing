// Package plugin implementa a única tela usada por todas as tools screen-v1.
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/sanitize"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/hostaction"
	"github.com/mateuslh/lealing/internal/core/interactive"
)

const operationTimeout = 5 * time.Second
const topbarRows = 2

type Model struct {
	deps    tui.Deps
	opener  interactive.Opener
	actions hostaction.Actions
	tool    domain.Tool

	ctx    context.Context
	cancel context.CancelFunc

	width, height int
	state         interactive.State
	session       interactive.Session
	snapshot      interactive.Snapshot
	err           error
	notification  string
	confirmation  *confirmation
}

type confirmation struct {
	request interactive.HostRequest
	params  confirmationParams
}

type confirmationParams struct {
	Title        string `json:"title"`
	Message      string `json:"message"`
	ConfirmLabel string `json:"confirmLabel"`
	CancelLabel  string `json:"cancelLabel"`
}

type openedMsg struct {
	session interactive.Session
	err     error
}

type sessionMsg struct{ update interactive.Update }
type sentMsg struct{ err error }
type hostActionMsg struct {
	id     string
	result json.RawMessage
	err    error
}

var _ tui.Screen = (*Model)(nil)

func New(deps tui.Deps, opener interactive.Opener, actions hostaction.Actions, tool domain.Tool) *Model {
	ctx, cancel := context.WithCancel(context.Background())
	return &Model{deps: deps, opener: opener, actions: actions, tool: tool, ctx: ctx, cancel: cancel, state: interactive.StateStarting}
}

func (m *Model) ID() tui.ScreenID { return tui.ScreenID("tool/" + string(m.tool.ID)) }
func (m *Model) Title() string    { return strings.ToLower(m.tool.Title()) }
func (m *Model) Init() tea.Cmd    { return m.open() }

func (m *Model) open() tea.Cmd {
	opener := m.opener
	ctx := m.ctx
	id := m.tool.ID
	options := interactive.OpenOptions{Frame: interactive.Frame{Width: m.width, Height: m.height}, Theme: themeDTO(m.deps)}
	return func() tea.Msg {
		if opener == nil {
			return openedMsg{err: errors.New("serviço de tools interativas não configurado")}
		}
		session, err := opener.Open(ctx, id, options)
		return openedMsg{session: session, err: err}
	}
}

func (m *Model) Update(message tea.Msg) (tui.Screen, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
		if m.session != nil && m.state == interactive.StateRunning {
			return m, m.send(interactive.Event{Kind: interactive.EventResize, Resize: &interactive.ResizeEvent{Frame: interactive.Frame{Width: message.Width, Height: message.Height}}})
		}
		return m, nil

	case openedMsg:
		if message.err != nil {
			m.state, m.err = interactive.StateFailed, message.err
			return m, nil
		}
		m.session, m.state, m.err = message.session, interactive.StateRunning, nil
		commands := []tea.Cmd{m.waitUpdate()}
		if m.width > 0 && m.height > 0 {
			commands = append(commands, m.send(interactive.Event{Kind: interactive.EventResize, Resize: &interactive.ResizeEvent{Frame: interactive.Frame{Width: m.width, Height: m.height}}}))
		}
		return m, tea.Batch(commands...)

	case sessionMsg:
		m.state = message.update.State
		if message.update.Snapshot != nil {
			m.snapshot = *message.update.Snapshot
		}
		if message.update.Err != nil {
			m.err = message.update.Err
		}
		var command tea.Cmd
		if request := message.update.HostRequest; request != nil {
			command = m.handleHostRequest(*request)
		}
		if m.state == interactive.StateRunning {
			return m, tea.Batch(command, m.waitUpdate())
		}
		return m, command

	case hostActionMsg:
		return m, m.respond(message.id, message.result, message.err)

	case sentMsg:
		if message.err != nil && m.state == interactive.StateRunning {
			m.err = message.err
		}
		return m, nil

	case tea.KeyMsg:
		if m.confirmation != nil {
			return m.handleConfirmation(message)
		}
		if m.state == interactive.StateFailed && message.String() == "r" {
			m.restart()
			return m, m.open()
		}
		if m.state != interactive.StateRunning || m.session == nil {
			return m, nil
		}
		event := keyEvent(message)
		return m, m.send(event)

	case tea.MouseMsg:
		if m.state != interactive.StateRunning || m.session == nil {
			return m, nil
		}
		event := tea.MouseEvent(message)
		if event.X < 0 || event.X >= m.width || event.Y < topbarRows || event.Y >= topbarRows+m.height {
			return m, nil
		}
		return m, m.send(interactive.Event{Kind: interactive.EventMouse, Mouse: &interactive.MouseEvent{
			X: event.X, Y: event.Y - topbarRows, Button: mouseButton(event.Button), Action: mouseAction(event.Action),
			Ctrl: event.Ctrl, Alt: event.Alt, Shift: event.Shift,
		}})

	case tea.FocusMsg:
		if m.state != interactive.StateRunning || m.session == nil {
			return m, nil
		}
		return m, m.send(interactive.Event{Kind: interactive.EventFocus})
	case tea.BlurMsg:
		if m.state != interactive.StateRunning || m.session == nil {
			return m, nil
		}
		return m, m.send(interactive.Event{Kind: interactive.EventBlur})
	}
	return m, nil
}

func (m *Model) View(frame tui.Frame) string {
	th := m.deps.Theme
	switch m.state {
	case interactive.StateStarting:
		return component.Center(frame.Width, frame.Height, th.Dim.Render("iniciando "+m.tool.Title()+"…"))
	case interactive.StateFailed:
		message := "a tool encerrou inesperadamente"
		if m.err != nil {
			message = m.err.Error()
		}
		return component.Center(frame.Width, frame.Height,
			lipgloss.NewStyle().Foreground(th.Danger).Render("✗ "+component.TruncateTail(message, max(frame.Width-4, 1))),
			"", th.Dim.Render("r reinicia · esc volta"))
	case interactive.StateStopped:
		return component.Center(frame.Width, frame.Height, th.Dim.Render("tool encerrada"))
	}
	body := sanitize.Frame(m.snapshot.Body, frame.Width, frame.Height)
	if body == "" {
		body = component.Center(frame.Width, frame.Height, th.Dim.Render("aguardando o primeiro snapshot…"))
	}
	if m.confirmation != nil {
		body = component.Overlay(body, m.confirmationView(frame), frame.Width, frame.Height)
	}
	return body
}

func (m *Model) Hints() []tui.Hint {
	if m.confirmation != nil {
		return []tui.Hint{{Key: "y", Label: "confirmar"}, {Key: "n/esc", Label: "cancelar"}}
	}
	if m.state == interactive.StateFailed {
		return []tui.Hint{{Key: "r", Label: "reiniciar"}, {Key: "esc", Label: "voltar"}}
	}
	hints := make([]tui.Hint, 0, len(m.snapshot.Hints)+1)
	escapeIndex := -1
	for _, hint := range m.snapshot.Hints {
		key := sanitize.Plain(hint.Key, 24)
		label := sanitize.Plain(hint.Label, 80)
		if key == "" {
			continue
		}
		hints = append(hints, tui.Hint{Key: key, Label: label})
		if escapeIndex < 0 && strings.Contains(strings.ToLower(key), "esc") {
			escapeIndex = len(hints) - 1
		}
	}
	if escapeIndex < 0 {
		hints = append([]tui.Hint{{Key: "esc", Label: "voltar"}}, hints...)
	} else if escapeIndex > 0 {
		escape := hints[escapeIndex]
		hints = append([]tui.Hint{escape}, append(hints[:escapeIndex], hints[escapeIndex+1:]...)...)
	}
	return hints
}

func (m *Model) Status() (string, lipgloss.TerminalColor) {
	switch {
	case m.notification != "":
		return m.notification, m.deps.Theme.Accent
	case m.state == interactive.StateStarting:
		return "negociando protocolo…", m.deps.Theme.Accent
	case m.state == interactive.StateFailed:
		return "processo da tool falhou", m.deps.Theme.Danger
	default:
		return sanitize.Plain(m.snapshot.Status, 240), m.deps.Theme.Faint
	}
}

func (m *Model) Capturing() bool { return m.confirmation != nil || m.snapshot.Capturing }

// Close é chamado pelo Router quando a tela sai definitivamente da pilha.
// O comando tenta shutdown antes de cancelar o contexto que força o processo.
func (m *Model) Close() tea.Cmd {
	session := m.session
	cancel := m.cancel
	return func() tea.Msg {
		if session != nil {
			ctx, stop := context.WithTimeout(context.Background(), operationTimeout)
			_ = session.Shutdown(ctx)
			stop()
		}
		cancel()
		return nil
	}
}

// WantsMouse sinaliza ao Router se o mouse deve ser capturado enquanto esta
// tela estiver no topo da pilha, para repassar cliques e arraste à tool. É a
// tool quem decide, via ui.wantsMouse no manifest: por padrão o mouse fica
// livre para o terminal, permitindo seleção de texto nativa.
func (m *Model) WantsMouse() bool { return m.tool.WantsMouse() }

func (m *Model) waitUpdate() tea.Cmd {
	session := m.session
	return func() tea.Msg {
		if session == nil {
			return sessionMsg{update: interactive.Update{State: interactive.StateFailed, Err: errors.New("sessão ausente")}}
		}
		select {
		case update := <-session.Updates():
			return sessionMsg{update: update}
		case <-m.ctx.Done():
			return nil
		}
	}
}

func (m *Model) send(event interactive.Event) tea.Cmd {
	session := m.session
	return func() tea.Msg {
		if session == nil {
			return sentMsg{err: errors.New("sessão ausente")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
		defer cancel()
		return sentMsg{err: session.Send(ctx, event)}
	}
}

func (m *Model) respond(id string, result json.RawMessage, responseErr error) tea.Cmd {
	session := m.session
	return func() tea.Msg {
		if session == nil {
			return sentMsg{err: errors.New("sessão ausente")}
		}
		response := interactive.HostResponse{ID: id, Result: result}
		if responseErr != nil {
			response.Error = &interactive.ProtocolError{Code: "host_error", Message: responseErr.Error()}
		}
		ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
		defer cancel()
		return sentMsg{err: session.Respond(ctx, response)}
	}
}

func (m *Model) handleHostRequest(request interactive.HostRequest) tea.Cmd {
	switch request.Method {
	case interactive.CapabilityNavigationBack:
		return tea.Sequence(m.respond(request.ID, json.RawMessage(`{"ok":true}`), nil), func() tea.Msg { return tui.Back() })

	case interactive.CapabilityNotificationShow:
		var params struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil || strings.TrimSpace(params.Message) == "" {
			return m.respond(request.ID, nil, errors.New("notificação inválida"))
		}
		m.notification = sanitize.Plain(params.Message, 240)
		return m.respond(request.ID, json.RawMessage(`{"ok":true}`), nil)

	case interactive.CapabilityClipboardWrite:
		var params struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return m.respond(request.ID, nil, err)
		}
		return m.runHostAction(request.ID, func(ctx context.Context) error {
			if m.actions == nil {
				return errors.New("clipboard indisponível")
			}
			return m.actions.WriteClipboard(ctx, params.Text)
		})

	case interactive.CapabilityBrowserOpen:
		var params struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return m.respond(request.ID, nil, err)
		}
		return m.runHostAction(request.ID, func(ctx context.Context) error {
			if m.actions == nil {
				return errors.New("browser indisponível")
			}
			return m.actions.OpenBrowser(ctx, params.URL)
		})

	case interactive.CapabilityConfirmationRequest:
		var params confirmationParams
		if err := json.Unmarshal(request.Params, &params); err != nil || strings.TrimSpace(params.Message) == "" {
			return m.respond(request.ID, nil, errors.New("confirmação inválida"))
		}
		m.confirmation = &confirmation{request: request, params: params}
		m.confirmation.params.Title = sanitize.Plain(params.Title, 80)
		m.confirmation.params.Message = sanitize.Plain(params.Message, 4<<10)
		m.confirmation.params.ConfirmLabel = sanitize.Plain(params.ConfirmLabel, 40)
		m.confirmation.params.CancelLabel = sanitize.Plain(params.CancelLabel, 40)
		return nil
	default:
		return m.respond(request.ID, nil, fmt.Errorf("comando de host desconhecido: %s", request.Method))
	}
}

func (m *Model) runHostAction(id string, action func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
		defer cancel()
		err := action(ctx)
		return hostActionMsg{id: id, result: json.RawMessage(`{"ok":true}`), err: err}
	}
}

func (m *Model) handleConfirmation(key tea.KeyMsg) (tui.Screen, tea.Cmd) {
	confirmed := false
	switch strings.ToLower(key.String()) {
	case "y", "s", "enter":
		confirmed = true
	case "n", "esc":
	default:
		return m, nil
	}
	id := m.confirmation.request.ID
	m.confirmation = nil
	result, _ := json.Marshal(map[string]bool{"confirmed": confirmed})
	return m, m.respond(id, result, nil)
}

func (m *Model) confirmationView(frame tui.Frame) string {
	params := m.confirmation.params
	title := params.Title
	if title == "" {
		title = "confirmação"
	}
	confirmLabel, cancelLabel := params.ConfirmLabel, params.CancelLabel
	if confirmLabel == "" {
		confirmLabel = "confirmar"
	}
	if cancelLabel == "" {
		cancelLabel = "cancelar"
	}
	width := min(max(frame.Width-8, 20), 64)
	message := component.TruncateTail(params.Message, max(width-4, 1))
	content := lipgloss.JoinVertical(lipgloss.Left, message, "", m.deps.Theme.Dim.Render("y "+confirmLabel+" · n/esc "+cancelLabel))
	return component.Panel{Title: title, Glyph: "!", Accent: m.deps.Theme.Warning, Focused: true, Width: width, Height: min(lipgloss.Height(content)+2, frame.Height)}.Render(m.deps.Theme, content)
}

func (m *Model) restart() {
	if m.cancel != nil {
		m.cancel()
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.session = nil
	m.snapshot = interactive.Snapshot{}
	m.err = nil
	m.state = interactive.StateStarting
	m.notification = ""
}

func keyEvent(message tea.KeyMsg) interactive.Event {
	key := tea.Key(message)
	if key.Paste {
		return interactive.Event{Kind: interactive.EventPaste, Paste: &interactive.PasteEvent{Text: string(key.Runes)}}
	}
	code := key.Type.String()
	text := ""
	if key.Type == tea.KeyRunes {
		code = "rune"
		text = string(key.Runes)
	}
	ctrl := strings.HasPrefix(message.String(), "ctrl+")
	shift := strings.HasPrefix(message.String(), "shift+")
	return interactive.Event{Kind: interactive.EventKey, Key: &interactive.KeyEvent{Code: code, Text: text, Ctrl: ctrl, Alt: key.Alt, Shift: shift}}
}

func mouseButton(button tea.MouseButton) string {
	switch button {
	case tea.MouseButtonLeft:
		return "left"
	case tea.MouseButtonMiddle:
		return "middle"
	case tea.MouseButtonRight:
		return "right"
	case tea.MouseButtonWheelUp:
		return "wheelUp"
	case tea.MouseButtonWheelDown:
		return "wheelDown"
	case tea.MouseButtonWheelLeft:
		return "wheelLeft"
	case tea.MouseButtonWheelRight:
		return "wheelRight"
	case tea.MouseButtonBackward:
		return "backward"
	case tea.MouseButtonForward:
		return "forward"
	default:
		return "none"
	}
}

func mouseAction(action tea.MouseAction) string {
	switch action {
	case tea.MouseActionRelease:
		return "release"
	case tea.MouseActionMotion:
		return "motion"
	default:
		return "press"
	}
}

func themeDTO(deps tui.Deps) interactive.Theme {
	if deps.Theme == nil {
		return interactive.Theme{}
	}
	p := deps.Theme.Palette
	return interactive.Theme{
		Primary: p.Primary.Dark, Secondary: p.Secondary.Dark, Accent: p.Accent.Dark,
		Success: p.Success.Dark, Warning: p.Warning.Dark, Danger: p.Danger.Dark,
		Text: p.Text.Dark, Muted: p.Muted.Dark, Faint: p.Faint.Dark,
		Border: p.Border.Dark, Surface: p.Surface.Dark,
	}
}
