package component

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Overlay centraliza foreground sobre background preservando sequências ANSI.
//
// O lipgloss posiciona blocos, mas não os sobrepõe. Recortar por bytes aqui
// quebraria cores e caracteres largos justamente nas bordas do modal.
func Overlay(background, foreground string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	foregroundWidth := min(lipgloss.Width(foreground), width)
	foregroundHeight := min(lipgloss.Height(foreground), height)
	left := max((width-foregroundWidth)/2, 0)
	top := max((height-foregroundHeight)/2, 0)

	backgroundLines := fitLines(background, width, height)
	foregroundLines := fitLines(foreground, foregroundWidth, foregroundHeight)
	for row := range foregroundHeight {
		target := top + row
		before := ansi.Cut(backgroundLines[target], 0, left)
		after := ansi.Cut(backgroundLines[target], left+foregroundWidth, width)
		backgroundLines[target] = lipgloss.NewStyle().Width(left).Render(before) +
			foregroundLines[row] + after
	}
	return strings.Join(backgroundLines, "\n")
}

func fitLines(block string, width, height int) []string {
	source := strings.Split(block, "\n")
	lines := make([]string, height)
	style := lipgloss.NewStyle().Width(width)
	for row := range height {
		line := ""
		if row < len(source) {
			line = ansi.Cut(source[row], 0, width)
		}
		lines[row] = style.Render(line)
	}
	return lines
}
