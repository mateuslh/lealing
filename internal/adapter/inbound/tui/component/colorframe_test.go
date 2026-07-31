package component

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
)

func TestColorFrameFechaNasDimensoesExternas(t *testing.T) {
	th := theme.Default()
	for _, size := range [][2]int{{150, 38}, {60, 16}, {30, 8}, {12, 3}} {
		width, height := size[0], size[1]
		out := (ColorFrame{
			Title: "LEALING / HOME", Width: width, Height: height,
		}).Render(th, "conteúdo maior que o miolo\nsegunda linha")

		lines := strings.Split(out, "\n")
		if len(lines) != height {
			t.Errorf("%dx%d: altura = %d", width, height, len(lines))
		}
		for i, line := range lines {
			if got := lipgloss.Width(line); got != width {
				t.Errorf("%dx%d: linha %d tem %d colunas", width, height, i, got)
			}
		}
	}
}
