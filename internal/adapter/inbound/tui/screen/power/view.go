package power

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	corepower "github.com/mateuslh/lealing/internal/core/power"
)

// twoColumnMin é a largura a partir da qual os dois perfis cabem lado a
// lado. Abaixo disso, só a fonte focada é mostrada.
const twoColumnMin = 84

// View implementa tui.Screen.
func (m *Model) View(f tui.Frame) string {
	th := m.deps.Theme

	if m.loading && m.saved == (corepower.Settings{}) {
		return component.Center(f.Width, f.Height,
			th.Dim.Render("lendo as configurações de energia…"))
	}

	inner := max(f.Width-4, 20)
	// A barra de presets e o aviso de mudança ficam fora das colunas.
	footer := m.viewFooter(th, inner)
	panelsH := max(f.Height-lipgloss.Height(footer)-3, 6)
	// O painel encolhe até o conteúdo: esticado até a base, sobra um retângulo
	// vazio maior que a própria lista de campos.
	fit := max(len(m.fields(corepower.Battery)), len(m.fields(corepower.AC))) + 2
	panelsH = min(panelsH, fit)

	var panels string
	if inner >= twoColumnMin {
		colW := (inner - 1) / 2
		panels = lipgloss.JoinHorizontal(lipgloss.Top,
			m.viewProfile(th, corepower.Battery, colW, panelsH),
			" ",
			m.viewProfile(th, corepower.AC, inner-colW-1, panelsH),
		)
	} else {
		// Só uma coluna cabe: as abas são o que revela que existe outra fonte.
		panels = lipgloss.JoinVertical(lipgloss.Left,
			m.viewSourceTabs(th, inner),
			m.viewProfile(th, m.source, inner, panelsH-1),
		)
	}

	body := lipgloss.JoinVertical(lipgloss.Left, panels, "", footer)
	return lipgloss.NewStyle().
		Padding(1, 2).
		MaxWidth(f.Width).
		MaxHeight(f.Height).
		Render(body)
}

// viewSourceTabs anuncia as duas fontes e qual delas está em edição.
func (m *Model) viewSourceTabs(th *theme.Theme, width int) string {
	tab := func(src corepower.Source, glyph string) string {
		if m.source == src {
			return lipgloss.NewStyle().Foreground(th.Primary).Bold(true).
				Render("▸ " + glyph + " " + src.String())
		}
		return th.Ghost.Render("  " + glyph + " " + src.String())
	}
	tabs := tab(corepower.Battery, "⏻") + "  " + tab(corepower.AC, "⚡")
	return component.Spread(tabs, th.Ghost.Render("⇧←→"), width)
}

// viewProfile desenha a coluna de uma fonte de alimentação.
func (m *Model) viewProfile(th *theme.Theme, src corepower.Source, width, height int) string {
	focused := m.source == src
	visible := m.fields(src)
	profile := m.current.Get(src)
	savedProfile := m.saved.Get(src)

	rows := make([]component.Row, len(visible))
	for i, fld := range visible {
		rows[i] = m.viewRow(th, fld, &profile, &savedProfile, focused && i == m.cursor[src])
	}

	// Em janela baixa nem todos os campos cabem; a janela de exibição
	// acompanha o cursor para que descer nunca leve a uma linha invisível.
	selected := m.cursor[src]
	visibleRows := max(height-2, 1)
	scrolled := len(rows) > visibleRows
	if scrolled {
		start := min(max(selected-visibleRows/2, 0), len(rows)-visibleRows)
		rows = rows[start : start+visibleRows]
		selected -= start
	}

	content := component.FieldList{
		Rows:  rows,
		Width: width - 4,
		// Só a coluna em edição desenha cursor: dois marcadores na tela
		// sugerem duas seleções ativas.
		Selected:  selected,
		Focusable: focused,
		Focused:   focused,
	}.Render(th)

	glyph, accent := "⏻", th.Success
	if src == corepower.AC {
		glyph, accent = "⚡", th.Warning
	}

	footer := profile.Summary()
	if profile != savedProfile {
		footer = "• " + footer
	}
	if scrolled {
		footer = "↕ " + strconv.Itoa(m.cursor[src]+1) + "/" + strconv.Itoa(len(visible)) + " · " + footer
	}

	return component.Panel{
		Title:   src.String(),
		Glyph:   glyph,
		Accent:  accent,
		Focused: focused,
		Footer:  footer,
		Width:   width,
		Height:  height,
	}.Render(th, content)
}

// viewRow monta a linha de um campo, marcando o que difere do salvo.
//
// A marca é o rótulo em âmbar, não um símbolo depois do controle: o controle
// já ocupa boa parte da coluna, e um ponto flutuando à direita dele some no
// meio das dicas.
func (m *Model) viewRow(th *theme.Theme, fld corepower.Field, profile, saved *corepower.Profile, selected bool) component.Row {
	row := component.Row{Label: fld.Label, Hint: fld.Hint}

	switch fld.Control {
	case corepower.ControlToggle:
		row.Control = component.Toggle(th, *fld.BoolAt(profile), selected)
	case corepower.ControlMinutes:
		row.Control = component.Stepper(th, formatMinutes(*fld.IntAt(profile)), selected)
	case corepower.ControlHibernate:
		row.Control = component.Stepper(th, formatHibernate(*fld.IntAt(profile)), selected)
	}
	if !fld.Equal(profile, saved) {
		row.LabelTone = th.Warning
	}
	return row
}

// viewFooter desenha presets, estado da dispensa de senha e o aviso de
// alterações pendentes.
func (m *Model) viewFooter(th *theme.Theme, width int) string {
	var left []string

	left = append(left,
		th.KeyCap.Render("1")+" "+th.KeyLabel.Render("nunca dormir"),
		th.KeyCap.Render("2")+" "+th.KeyLabel.Render("padrões do sistema"),
	)

	// Onde aplicar não pede elevação, o cadeado não tem o que dizer: um
	// "senha dispensada" permanente só faria o usuário procurar o controle
	// que o ligaria.
	lock := ""
	if m.supportsPasswordless() {
		lock = th.Ghost.Render("⚿ pede senha ao aplicar")
		if m.passwordless {
			lock = lipgloss.NewStyle().Foreground(th.Success).Render("⚿ senha dispensada")
		}
	}

	line := component.Spread(strings.Join(left, "   "), lock, width)

	// O aviso de estado tem versão curta: em coluna estreita, a frase longa
	// estouraria a largura do frame inteiro.
	var text string
	var style lipgloss.Style
	switch {
	case m.busy:
		text, style = "◐ aguardando o sistema…", lipgloss.NewStyle().Foreground(th.Accent)
	case m.HasUnsavedChanges():
		text, style = "• alterações não aplicadas — “a” aplica, “d” descarta",
			lipgloss.NewStyle().Foreground(th.Warning)
		if width < lipgloss.Width(text) {
			text = "• não aplicado"
		}
	default:
		text, style = "tudo sincronizado com o sistema", th.Ghost
		if width < lipgloss.Width(text) {
			text = "sincronizado"
		}
	}
	state := style.Render(component.TruncateTail(text, width))

	return lipgloss.JoinVertical(lipgloss.Left,
		th.Divider.Render(strings.Repeat("╌", width)),
		line,
		state,
	)
}
