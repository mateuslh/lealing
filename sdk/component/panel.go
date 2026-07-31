package component

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
)

type Panel struct {
	Title, Glyph, Footer string
	Accent               lipgloss.TerminalColor
	Focused              bool
	Width, Height        int
}

func (p Panel) Render(theme *Theme, content string) string {
	if p.Width < 4 {
		return ""
	}
	inner := p.Width - 2
	borderColor := lipgloss.TerminalColor(theme.Border)
	if p.Focused {
		borderColor = theme.BorderFocus
	}
	border := lipgloss.NewStyle().Foreground(borderColor)
	var builder strings.Builder
	builder.WriteString(p.top(theme, border, inner))
	builder.WriteByte('\n')
	for i, line := range p.body(content, inner) {
		if i > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(border.Render("│"))
		builder.WriteString(line)
		builder.WriteString(border.Render("│"))
	}
	builder.WriteByte('\n')
	builder.WriteString(p.bottom(theme, border, inner))
	return builder.String()
}

func (p Panel) top(theme *Theme, border lipgloss.Style, inner int) string {
	if p.Title == "" {
		return border.Render("╭" + strings.Repeat("─", inner) + "╮")
	}
	accent := p.Accent
	if accent == nil {
		accent = theme.Muted
	}
	label := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(strings.ToUpper(p.Title))
	if p.Glyph != "" {
		label = lipgloss.NewStyle().Foreground(accent).Render(p.Glyph) + " " + label
	}
	labelWidth := lipgloss.Width(label) + 2
	if labelWidth+3 > inner {
		label = truncate.StringWithTail(label, uint(max(inner-5, 0)), "…")
		labelWidth = lipgloss.Width(label) + 2
	}
	return border.Render("╭─ ") + label + border.Render(" "+strings.Repeat("─", max(inner-labelWidth-1, 0))+"╮")
}

func (p Panel) bottom(theme *Theme, border lipgloss.Style, inner int) string {
	if p.Footer == "" {
		return border.Render("╰" + strings.Repeat("─", inner) + "╯")
	}
	label := theme.Ghost.Render(p.Footer)
	labelWidth := lipgloss.Width(label) + 2
	if labelWidth+2 > inner {
		return border.Render("╰" + strings.Repeat("─", inner) + "╯")
	}
	return border.Render("╰"+strings.Repeat("─", inner-labelWidth)+" ") + label + border.Render(" ╯")
}

func (p Panel) body(content string, inner int) []string {
	lines := strings.Split(content, "\n")
	want := p.Height - 2
	if p.Height <= 0 {
		want = len(lines)
	}
	want = max(want, 0)
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
