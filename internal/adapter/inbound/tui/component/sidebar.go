package component

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/core/port"
)

// Sidebar lista as categorias do catálogo com a contagem de cada uma.
type Sidebar struct {
	Items    []port.CategoryView
	Selected int
	Focused  bool
	Width    int
	Height   int
}

// Render desenha a lista, rolando para manter a seleção visível.
func (s Sidebar) Render(th *theme.Theme) string {
	if s.Width <= 0 || len(s.Items) == 0 {
		return ""
	}

	visible := len(s.Items)
	if s.Height > 0 {
		visible = min(visible, s.Height)
	}

	start := 0
	if s.Selected >= visible {
		start = s.Selected - visible + 1
	}
	end := min(start+visible, len(s.Items))

	// A maior contagem define a largura da coluna numérica, para que os
	// números fiquem alinhados à direita em vez de dançarem por linha.
	countW := 1
	for _, it := range s.Items {
		countW = max(countW, len(strconv.Itoa(it.Count)))
	}

	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		lines = append(lines, s.renderItem(th, s.Items[i], i == s.Selected, countW))
	}

	// Indicadores de que há mais categorias fora da janela.
	if start > 0 {
		lines[0] = th.Ghost.Render(strings.Repeat(" ", max(s.Width-1, 0)) + "▴")
	}
	if end < len(s.Items) && len(lines) > 0 {
		lines[len(lines)-1] = th.Ghost.Render(strings.Repeat(" ", max(s.Width-1, 0)) + "▾")
	}

	return strings.Join(lines, "\n")
}

func (s Sidebar) renderItem(th *theme.Theme, it port.CategoryView, selected bool, countW int) string {
	accent := th.SpectrumAt(it.Accent)

	caret := "  "
	nameStyle := th.Dim
	if selected {
		caret = lipgloss.NewStyle().Foreground(accent).Render("▎") + " "
		nameStyle = th.Body.Bold(true)
		if !s.Focused {
			nameStyle = th.Body
		}
	}

	glyph := it.Glyph
	if glyph == "" {
		glyph = "◇"
	}
	glyph = lipgloss.NewStyle().Foreground(accent).Render(glyph)

	count := th.Counter.Render(pad(strconv.Itoa(it.Count), countW))

	prefixW := lipgloss.Width(caret) + lipgloss.Width(glyph) + 1
	nameW := max(s.Width-prefixW-countW-1, 3)
	name := nameStyle.Render(truncate.StringWithTail(it.Name, uint(nameW), "…"))

	return Spread(caret+glyph+" "+name, count, s.Width)
}

// pad alinha um número à direita em uma coluna de largura fixa.
func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}
