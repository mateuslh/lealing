// Package confirmation implementa a confirmação global que precede tools
// destrutivas. O diálogo pertence à engine; uma tool externa nunca decide
// como contornar esta barreira.
package confirmation

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	"github.com/mateuslh/lealing/internal/core/domain"
)

type Model struct {
	deps      tui.Deps
	tool      domain.Tool
	confirmed func() tea.Cmd
}

var _ tui.Screen = (*Model)(nil)

func New(deps tui.Deps, tool domain.Tool, confirmed func() tea.Cmd) *Model {
	return &Model{deps: deps, tool: tool, confirmed: confirmed}
}

func (m *Model) ID() tui.ScreenID { return tui.ScreenID("confirm/" + string(m.tool.ID)) }
func (m *Model) Title() string    { return "confirmar " + strings.ToLower(m.tool.Title()) }
func (m *Model) Init() tea.Cmd    { return nil }

func (m *Model) Update(message tea.Msg) (tui.Screen, tea.Cmd) {
	key, ok := message.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch strings.ToLower(key.String()) {
	case "y", "s", "enter":
		var next tea.Cmd
		if m.confirmed != nil {
			next = m.confirmed()
		}
		return m, tea.Sequence(func() tea.Msg { return tui.Back() }, next)
	case "n":
		return m, func() tea.Msg { return tui.Back() }
	default:
		return m, nil
	}
}

func (m *Model) View(frame tui.Frame) string {
	th := m.deps.Theme
	detail := component.TruncateTail(m.tool.Detail, max(frame.Width-12, 1))
	content := lipgloss.JoinVertical(lipgloss.Left,
		th.Strong.Render(m.tool.Title()),
		"",
		th.Dim.Render(detail),
		"",
		lipgloss.NewStyle().Foreground(th.Danger).Bold(true).Render("Esta ação foi classificada como destrutiva."),
	)
	width := min(max(frame.Width-8, 22), 72)
	panel := component.Panel{
		Title: "confirmação global", Glyph: "!", Accent: th.Danger,
		Focused: true, Width: width, Height: min(lipgloss.Height(content)+2, frame.Height),
	}.Render(th, content)
	return component.Center(frame.Width, frame.Height, panel)
}

func (*Model) Hints() []tui.Hint {
	return []tui.Hint{{Key: "y/↵", Label: "confirmar"}, {Key: "n/esc", Label: "cancelar"}}
}
