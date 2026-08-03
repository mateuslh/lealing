package settings

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	core "github.com/mateuslh/lealing/internal/core/settings"
)

const (
	sidebarWidth  = 24
	splitMinWidth = 74
)

func (m *Model) View(frame tui.Frame) string {
	th := m.deps.Theme
	if frame.Width < 24 || frame.Height < 6 {
		return th.Ghost.Render("janela pequena demais")
	}

	inner := max(frame.Width-4, 20)
	height := max(frame.Height-2, 4)

	body := m.viewBody(th, inner, height)
	return lipgloss.NewStyle().
		Padding(1, 2).MaxWidth(frame.Width).MaxHeight(frame.Height).
		Render(body)
}

func (m *Model) viewBody(th *theme.Theme, width, height int) string {
	if m.manager == nil || (m.err != nil && len(m.values) == 0) {
		message := errNotConfigured.Error()
		if m.err != nil {
			message = firstLine(m.err.Error())
		}
		return component.Center(width, height,
			lipgloss.NewStyle().Foreground(th.Danger).Render("✗ "+message))
	}
	// Sem largura para duas colunas, a lista de seções sai: os campos são o
	// conteúdo, e a navegação entre seções continua pelas setas.
	if width < splitMinWidth {
		return m.viewFields(th, width, height)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.viewSections(th, sidebarWidth, height),
		" ",
		m.viewFields(th, width-sidebarWidth-1, height),
	)
}

func (m *Model) viewSections(th *theme.Theme, width, height int) string {
	inner := width - 2
	lines := make([]string, 0, len(m.sections))
	for index, section := range m.sections {
		caret, label := "  ", th.Item.Render(section.Name)
		if index == m.section {
			label = th.ItemSelected.Render(section.Name)
			caret = "  "
			if m.focus == zoneSections {
				caret = lipgloss.NewStyle().Foreground(th.Primary).Render("▎") + " "
			}
		}
		glyph := lipgloss.NewStyle().Foreground(th.SpectrumAt(index)).Render(section.Glyph)
		count := th.Counter.Render(fmt.Sprintf("%d", m.sectionCount(section)))
		lines = append(lines, component.TruncateTail(
			component.Spread(caret+glyph+" "+label, count, inner), inner))
	}

	return component.Panel{
		Title: "seções", Glyph: "⚙", Accent: th.Primary,
		Focused: m.focus == zoneSections, Width: width, Height: height,
	}.Render(th, strings.Join(lines, "\n"))
}

func (m *Model) viewFields(th *theme.Theme, width, height int) string {
	section, ok := m.currentSection()
	if !ok {
		return ""
	}
	inner := width - 2
	entries := m.sectionEntries()

	lines := []string{th.Ghost.Render(component.TruncateTail(section.Description, inner)), ""}
	for index, current := range entries {
		if current.isAction {
			lines = append(lines, m.viewAction(th, current.action, index == m.field, m.focus == zoneFields, inner)...)
			continue
		}
		lines = append(lines, m.viewField(th, current.value, index == m.field, inner)...)
	}
	for _, row := range m.sectionInfo() {
		lines = append(lines, component.TruncateTail(
			component.FieldList{
				Rows:  []component.Row{{Label: row.Label, Value: row.Value, Tone: th.Faint}},
				Width: inner, LabelWidth: 14,
			}.Render(th), inner))
	}
	if len(entries) == 0 && len(m.sectionInfo()) == 0 {
		lines = append(lines, th.Ghost.Render("nada configurável aqui ainda"))
	}
	if message := m.viewFeedback(th, inner); message != "" {
		lines = append(lines, "", message)
	}

	footer := "↵ editar · r padrão"
	if current, ok := m.currentEntry(); ok && current.isAction {
		footer = "↵ abrir"
	}
	if m.editing {
		footer = "↵ salvar · esc cancelar"
	}
	return component.Panel{
		Title: section.Name, Glyph: section.Glyph, Accent: th.Secondary,
		Focused: m.focus == zoneFields, Footer: footer, Width: width, Height: height,
	}.Render(th, strings.Join(lines, "\n"))
}

// viewAction desenha uma capacidade administrativa como um cartão, não como
// mais uma linha da lista: glifo próprio, chamada em forma de selo e a
// descrição sempre visível — nunca só depois de selecionada — porque é
// abrindo mão desse contexto que a capacidade se confunde com um ajuste
// qualquer e passa despercebida.
func (m *Model) viewAction(th *theme.Theme, action Action, selected, focused bool, width int) []string {
	glyph := action.Glyph
	if glyph == "" {
		glyph = "→"
	}
	caret := "  "
	label := th.Item.Render(glyph + " " + action.Label)
	if selected {
		label = th.ItemSelected.Render(glyph + " " + action.Label)
		if focused {
			caret = lipgloss.NewStyle().Foreground(th.Primary).Render("▎") + " "
		}
	}

	value := action.Value
	if value == "" {
		value = "abrir"
	}
	badge := th.Pill.Render(value)
	if selected && focused {
		badge = lipgloss.NewStyle().
			Foreground(th.OnPrimary).Background(th.Primary).Bold(true).Padding(0, 1).
			Render(value)
	}

	lines := []string{component.TruncateTail(component.Spread(caret+label, badge, width), width)}
	if description := wrap(th.Ghost, action.Description, width-4, 2); description != "" {
		lines = append(lines, description)
	}
	return append(lines, "")
}

// viewField desenha rótulo, valor e — abaixo, só no selecionado — a
// explicação e a origem. Mostrar a descrição de todos de uma vez transformaria
// a coluna em um muro de texto.
func (m *Model) viewField(th *theme.Theme, value core.Value, selected bool, width int) []string {
	caret := "  "
	label := th.Item.Render(value.Label)
	if selected {
		label = th.ItemSelected.Render(value.Label)
		if m.focus == zoneFields {
			caret = lipgloss.NewStyle().Foreground(th.Secondary).Render("▎") + " "
		}
	}

	control := m.viewControl(th, value, selected, width)
	lines := []string{component.TruncateTail(
		component.Spread(caret+label, control, width), width)}

	if !selected {
		return lines
	}
	lines = append(lines, wrap(th.Ghost, value.Description, width-4, 3))

	origin := th.Ghost.Render("    " + value.Source.Label())
	if value.Restart && m.restarted[value.Key] {
		origin += lipgloss.NewStyle().Foreground(th.Warning).
			Render("  ▲ vale ao reabrir o lealing")
	} else if value.Restart {
		origin += th.Ghost.Render("  · só vale ao reabrir")
	}
	lines = append(lines, component.TruncateTail(origin, width), "")
	return lines
}

// viewControl é o valor renderizado conforme o tipo do campo.
func (m *Model) viewControl(th *theme.Theme, value core.Value, selected bool, width int) string {
	if value.Kind == core.KindToggle {
		return component.Toggle(th, value.Bool(), selected && m.focus == zoneFields)
	}
	if selected && m.editing {
		m.input.Width = max(width/2, 12)
		return th.Cursor.Render("› ") + m.input.View()
	}

	text := value.Current
	if text == "" {
		text = value.Placeholder
		return th.Ghost.Render(component.TruncateTail(text, max(width/2, 12)))
	}
	style := th.Body
	if value.Source == core.SourceDefault {
		style = th.Dim
	}
	return style.Render(component.TruncateTail(text, max(width/2, 12)))
}

func (m *Model) viewFeedback(th *theme.Theme, width int) string {
	switch {
	case m.err != nil:
		return wrap(lipgloss.NewStyle().Foreground(th.Danger), "✗ "+firstLine(m.err.Error()), width, 3)
	case m.message != "" && m.success:
		return lipgloss.NewStyle().Foreground(th.Success).
			Render(component.TruncateTail("✓ "+m.message, width))
	}
	return ""
}

func (m *Model) Hints() []tui.Hint {
	if m.editing {
		return []tui.Hint{{Key: "↵", Label: "salvar"}, {Key: "esc", Label: "cancelar"}}
	}
	if m.focus == zoneSections {
		return []tui.Hint{
			{Key: "↑↓", Label: "seção"}, {Key: "→", Label: "ajustes"},
			{Key: "esc", Label: "voltar"},
		}
	}
	return []tui.Hint{
		{Key: "↑↓", Label: "item"}, {Key: "↵", Label: "abrir/editar"},
		{Key: "r", Label: "padrão"}, {Key: "←", Label: "seções"},
		{Key: "esc", Label: "voltar"},
	}
}

func (m *Model) Meta() []string {
	changed := 0
	for _, value := range m.values {
		if value.Source == core.SourceUser {
			changed++
		}
	}
	if changed == 0 {
		return []string{"tudo no padrão"}
	}
	return []string{fmt.Sprintf("%d alterados", changed)}
}

func (m *Model) Status() (string, lipgloss.TerminalColor) {
	th := m.deps.Theme
	switch {
	case m.err != nil:
		return "ajuste recusado", th.Danger
	case len(m.restarted) > 0:
		return "há ajustes que valem ao reabrir", th.Warning
	case m.editing:
		return "editando", th.Accent
	default:
		if section, ok := m.currentSection(); ok {
			return section.Description, th.Faint
		}
		return "", nil
	}
}

var (
	_ interface {
		Status() (string, lipgloss.TerminalColor)
	} = (*Model)(nil)
	_ interface{ Meta() []string }  = (*Model)(nil)
	_ interface{ Capturing() bool } = (*Model)(nil)
)

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}

// wrap quebra o texto em até maxLines linhas, cortando a última quando ainda
// sobra conteúdo.
func wrap(style lipgloss.Style, text string, width, maxLines int) string {
	if width <= 0 || maxLines <= 0 || strings.TrimSpace(text) == "" {
		return ""
	}
	var lines []string
	current := "    "
	for _, word := range strings.Fields(text) {
		candidate := word
		if strings.TrimSpace(current) != "" {
			candidate = current + " " + word
		} else {
			candidate = current + word
		}
		if lipgloss.Width(candidate) > width && strings.TrimSpace(current) != "" {
			lines = append(lines, current)
			current = "    " + word
			continue
		}
		current = candidate
	}
	if strings.TrimSpace(current) != "" {
		lines = append(lines, current)
	}
	if len(lines) > maxLines {
		lines = append(lines[:maxLines-1], strings.Join(lines[maxLines-1:], " "))
	}
	for index, line := range lines {
		lines[index] = component.TruncateTail(line, width)
	}
	return style.Render(strings.Join(lines, "\n"))
}
