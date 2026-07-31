package component

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/core/domain"
)

// ToolList desenha uma sequência de matches. É stateless de propósito: o
// índice selecionado vem de fora, o que permite a mesma lista aparecer na
// home, na busca e no browser de categoria sem duplicar lógica de render.
type ToolList struct {
	Items      []domain.Match
	Selected   int
	Focused    bool
	Width      int
	Height     int
	Categories map[domain.CategoryID]domain.Category
	// Detailed adiciona a linha de resumo sob cada item.
	Detailed bool
	// Empty é a mensagem exibida quando não há itens.
	Empty string
}

// Render devolve as linhas da lista, já recortadas para Width e Height.
func (l ToolList) Render(th *theme.Theme) string {
	if len(l.Items) == 0 {
		return th.Ghost.Render(l.Empty)
	}

	perItem := 1
	if l.Detailed {
		perItem = 2
	}

	visible := len(l.Items)
	if l.Height > 0 {
		visible = min(visible, max(l.Height/perItem, 1))
	}

	// Scroll: mantém o item selecionado dentro da janela visível.
	start := 0
	if l.Selected >= visible {
		start = l.Selected - visible + 1
	}
	end := min(start+visible, len(l.Items))

	lines := make([]string, 0, (end-start)*perItem)
	for i := start; i < end; i++ {
		lines = append(lines, l.renderItem(th, l.Items[i], i == l.Selected)...)
	}
	return strings.Join(lines, "\n")
}

// renderItem desenha um item: caret, glyph, nome, marcadores e resumo.
func (l ToolList) renderItem(th *theme.Theme, m domain.Match, selected bool) []string {
	cat := l.Categories[m.Tool.Category]

	caret := "  "
	nameStyle := th.Item
	glyphStyle := lipgloss.NewStyle().Foreground(th.SpectrumAt(cat.Accent))

	if selected {
		// O caret sólido na cor da categoria é o indicador primário; o bold
		// no nome reforça sem depender de cor de fundo, que nem todo
		// terminal renderiza bem.
		caret = lipgloss.NewStyle().Foreground(th.SpectrumAt(cat.Accent)).Render("▎") + " "
		nameStyle = th.ItemSelected
		if !l.Focused {
			nameStyle = th.Item.Bold(true)
		}
	}

	glyph := glyphStyle.Render(ToolGlyph(m.Tool, cat))

	// Marcadores à direita: favorito e risco.
	var marks []string
	if m.Usage.Favorite {
		marks = append(marks, lipgloss.NewStyle().Foreground(th.Warning).Render("★"))
	}
	if m.Tool.Risk != domain.RiskSafe {
		g, st := RiskGlyph(th, m.Tool.Risk)
		marks = append(marks, st.Render(g))
	}
	if m.Tool.Experimental {
		marks = append(marks, th.Ghost.Render("β"))
	}
	suffix := strings.Join(marks, " ")

	prefixW := lipgloss.Width(caret) + lipgloss.Width(glyph) + 1
	suffixW := lipgloss.Width(suffix)
	if suffixW > 0 {
		suffixW++
	}
	nameW := max(l.Width-prefixW-suffixW, 4)

	name := highlight(th, m.Tool.Title(), m.Positions, nameStyle, nameW)
	head := Spread(caret+glyph+" "+name, suffix, l.Width)

	if !l.Detailed {
		return []string{head}
	}

	indent := strings.Repeat(" ", min(prefixW, l.Width))
	summary := truncate.StringWithTail(m.Tool.Summary, uint(max(l.Width-prefixW, 0)), "…")
	return []string{head, indent + th.ItemDesc.Render(summary)}
}

// highlight aplica o estilo de realce nas posições que casaram com a busca.
//
// O truncamento acontece antes do realce para que as posições continuem
// válidas em relação ao texto efetivamente desenhado.
func highlight(th *theme.Theme, s string, positions []int, base lipgloss.Style, width int) string {
	if lipgloss.Width(s) > width {
		s = truncate.StringWithTail(s, uint(max(width, 0)), "…")
	}
	if len(positions) == 0 {
		return base.Render(s)
	}

	hit := make(map[int]bool, len(positions))
	for _, p := range positions {
		hit[p] = true
	}

	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s) * 4)
	for i, r := range runes {
		if hit[i] {
			b.WriteString(th.MatchHint.Render(string(r)))
		} else {
			b.WriteString(base.Render(string(r)))
		}
	}
	return b.String()
}
