package component

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
)

// Panel é uma moldura arredondada com o título embutido na borda superior e
// um contador opcional na inferior.
//
// A borda é montada à mão em vez de usar lipgloss.Border porque o lipgloss
// não sabe interromper a linha de topo para inserir texto — e é justamente
// esse detalhe que separa um painel com cara de acabado de uma caixa comum.
type Panel struct {
	Title   string
	Glyph   string
	Accent  lipgloss.TerminalColor
	Focused bool
	// Footer aparece encostado no canto inferior direito da moldura.
	Footer string
	// Width e Height são as dimensões externas, moldura inclusa.
	Width  int
	Height int
}

// Render envolve o conteúdo na moldura, recortando ou preenchendo as linhas
// até a altura pedida.
func (p Panel) Render(th *theme.Theme, content string) string {
	if p.Width < 4 {
		return ""
	}

	inner := p.Width - 2 // colunas entre as barras verticais
	borderColor := th.Border
	if p.Focused {
		borderColor = th.BorderFocus
	}
	border := lipgloss.NewStyle().Foreground(borderColor)

	var b strings.Builder
	b.WriteString(p.top(th, border, inner))
	b.WriteByte('\n')

	body := p.body(content, inner)
	for i, line := range body {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(border.Render("│"))
		b.WriteString(line)
		b.WriteString(border.Render("│"))
	}

	b.WriteByte('\n')
	b.WriteString(p.bottom(th, border, inner))
	return b.String()
}

// top monta a borda superior com o título embutido.
func (p Panel) top(th *theme.Theme, border lipgloss.Style, inner int) string {
	if p.Title == "" {
		return border.Render("╭" + strings.Repeat("─", inner) + "╮")
	}

	accent := p.Accent
	if accent == nil {
		accent = th.Muted
	}
	label := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(strings.ToUpper(p.Title))
	if p.Glyph != "" {
		label = lipgloss.NewStyle().Foreground(accent).Render(p.Glyph) + " " + label
	}

	// " ⟨glyph⟩ TÍTULO " ocupa o rótulo mais um espaço de cada lado.
	labelW := lipgloss.Width(label) + 2
	if labelW+3 > inner {
		label = truncate.StringWithTail(label, uint(max(inner-5, 0)), "…")
		labelW = lipgloss.Width(label) + 2
	}

	fill := inner - labelW - 1
	if fill < 0 {
		fill = 0
	}
	return border.Render("╭─ ") + label + border.Render(" "+strings.Repeat("─", fill)+"╮")
}

// bottom monta a borda inferior com o rodapé encostado à direita.
func (p Panel) bottom(th *theme.Theme, border lipgloss.Style, inner int) string {
	if p.Footer == "" {
		return border.Render("╰" + strings.Repeat("─", inner) + "╯")
	}

	label := th.Ghost.Render(p.Footer)
	// labelW conta o rótulo mais o espaço de respiro de cada lado.
	labelW := lipgloss.Width(label) + 2
	if labelW+2 > inner {
		return border.Render("╰" + strings.Repeat("─", inner) + "╯")
	}

	// A linha soma: "╰" + fill + " " + label + " " + "╯". Para fechar em
	// inner+2 colunas, fill precisa ser exatamente inner-labelW.
	fill := inner - labelW
	return border.Render("╰"+strings.Repeat("─", fill)+" ") + label + border.Render(" ╯")
}

// body normaliza o conteúdo para exatamente as linhas e colunas do miolo.
func (p Panel) body(content string, inner int) []string {
	lines := strings.Split(content, "\n")

	// Altura interna: total menos as duas linhas de borda.
	want := p.Height - 2
	if p.Height <= 0 {
		want = len(lines)
	}
	if want < 0 {
		want = 0
	}

	out := make([]string, want)
	pad := lipgloss.NewStyle().Width(inner)
	for i := range want {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		if lipgloss.Width(line) > inner {
			line = truncate.StringWithTail(line, uint(inner), "…")
		}
		out[i] = pad.Render(line)
	}
	return out
}
