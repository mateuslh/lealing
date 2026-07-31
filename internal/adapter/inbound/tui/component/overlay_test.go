package component_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
)

func TestOverlayPreservaGeometria(t *testing.T) {
	background := strings.Repeat("abcdefghij\n", 4) + "abcdefghij"
	foreground := "╭──╮\n│oi│\n╰──╯"

	got := component.Overlay(background, foreground, 10, 5)
	lines := strings.Split(got, "\n")
	if len(lines) != 5 {
		t.Fatalf("linhas = %d", len(lines))
	}
	for i, line := range lines {
		if width := lipgloss.Width(line); width != 10 {
			t.Errorf("linha %d tem largura %d: %q", i, width, line)
		}
	}
	if !strings.Contains(got, "│oi│") {
		t.Fatalf("modal não apareceu centralizado:\n%s", got)
	}
}
