package component

import (
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
)

// ColorFrame envolve uma tela inteira com uma moldura em degradê.
//
// A moldura é um componente, e não um estilo aplicado pela home, porque suas
// dimensões são externas: centralizar ou adicionar padding depois da borda
// faria o bloco ultrapassar o frame nos terminais estreitos.
type ColorFrame struct {
	Title         string
	Width, Height int
}

// Render normaliza o conteúdo para ocupar exatamente o miolo da moldura.
func (f ColorFrame) Render(th *theme.Theme, content string) string {
	if f.Width < 4 || f.Height < 3 {
		return TruncateTail(content, max(f.Width, 0))
	}

	innerW, innerH := f.Width-2, f.Height-2
	lines := strings.Split(content, "\n")
	pad := lipgloss.NewStyle().Width(innerW)
	sides := theme.Ramp(th.Gradient, max(innerH, 1))

	var b strings.Builder
	b.WriteString(theme.Text(f.top(innerW), th.Gradient))
	for row := range innerH {
		line := ""
		if row < len(lines) {
			line = TruncateTail(lines[row], innerW)
		}
		b.WriteByte('\n')
		left := lipgloss.NewStyle().Foreground(sides[row]).Render("│")
		right := lipgloss.NewStyle().Foreground(sides[len(sides)-1-row]).Render("│")
		b.WriteString(left)
		b.WriteString(pad.Render(line))
		b.WriteString(right)
	}

	reversed := slices.Clone(th.Gradient)
	slices.Reverse(reversed)
	b.WriteByte('\n')
	b.WriteString(theme.Text("╰"+strings.Repeat("─", innerW)+"╯", reversed))
	return b.String()
}

func (f ColorFrame) top(inner int) string {
	label := strings.TrimSpace(f.Title)
	if label == "" || lipgloss.Width(label)+6 > inner {
		return "╭" + strings.Repeat("─", inner) + "╮"
	}

	used := lipgloss.Width(label) + 3 // "─ " + label + " "
	return "╭─ " + label + " " + strings.Repeat("─", inner-used) + "╮"
}
