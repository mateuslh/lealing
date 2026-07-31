// Package requirements mostra ferramentas externas ausentes.
package requirements

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	"github.com/mateuslh/lealing/internal/core/domain"
)

// Model é o diagnóstico de pré-requisitos de uma tool.
type Model struct {
	deps    tui.Deps
	tool    domain.Tool
	missing []domain.Requirement
}

var _ tui.Screen = (*Model)(nil)

// New monta a tela.
func New(deps tui.Deps, tool domain.Tool, missing []domain.Requirement) *Model {
	return &Model{deps: deps, tool: tool, missing: missing}
}

// ID implementa tui.Screen.
func (m *Model) ID() tui.ScreenID {
	return tui.ScreenID("tool/requirements/" + string(m.tool.ID))
}

// Title implementa tui.Screen.
func (m *Model) Title() string { return "pré-requisitos" }

// Init implementa tui.Screen.
func (*Model) Init() tea.Cmd { return nil }

// Update implementa tui.Screen.
func (m *Model) Update(tea.Msg) (tui.Screen, tea.Cmd) { return m, nil }

// View implementa tui.Screen.
func (m *Model) View(f tui.Frame) string {
	th := m.deps.Theme
	inner := max(f.Width-4, 20)

	header := component.TruncateTail(
		"“"+m.tool.Title()+"” precisa das ferramentas abaixo antes de iniciar.",
		inner-4)
	lines := []string{lipgloss.NewStyle().Foreground(th.Warning).Render(header), ""}
	for _, requirement := range m.missing {
		line := th.Strong.Render("✗ " + requirement.Label())
		if requirement.Executable != requirement.Label() {
			line += th.Ghost.Render("  comando: " + requirement.Executable)
		}
		lines = append(lines, component.TruncateTail(line, inner-4))
		if requirement.InstallHint != "" {
			lines = append(lines, th.Dim.Render(component.TruncateTail(
				"  "+requirement.InstallHint, inner-4)))
		}
	}

	height := max(min(len(lines)+2, f.Height-2), 3)
	panel := component.Panel{
		Title: "Ferramentas ausentes", Glyph: "!", Accent: th.Warning,
		Width: inner, Height: height,
	}.Render(th, strings.Join(lines, "\n"))

	return lipgloss.NewStyle().
		Padding(1, 2).
		MaxWidth(f.Width).
		MaxHeight(f.Height).
		Render(panel)
}

// Hints implementa tui.Screen.
func (*Model) Hints() []tui.Hint {
	return []tui.Hint{{Key: "esc", Label: "voltar"}}
}

// Meta informa qual tool foi bloqueada.
func (m *Model) Meta() []string {
	return []string{component.TruncateTail(m.tool.Title(), 32)}
}

// Status resume a quantidade de ausências.
func (m *Model) Status() (string, lipgloss.TerminalColor) {
	label := " ferramentas ausentes"
	if len(m.missing) == 1 {
		label = " ferramenta ausente"
	}
	return strconv.Itoa(len(m.missing)) + label, m.deps.Theme.Warning
}
