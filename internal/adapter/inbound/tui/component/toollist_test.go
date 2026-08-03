package component

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/core/domain"
)

func TestToolListMostraTagsNoItem(t *testing.T) {
	const width = 48
	list := ToolList{
		Items: []domain.Match{{Tool: domain.Tool{
			ID: "example-tool", Name: "Example Tool",
			Category: "dev", Tags: []domain.Tag{"bradesco"},
		}}},
		Categories: map[domain.CategoryID]domain.Category{
			"dev": {ID: "dev", Glyph: "⚙", Accent: 2},
		},
		Width: width, Height: 1,
	}.Render(theme.Default())

	if !strings.Contains(list, "#bradesco") {
		t.Fatalf("item não mostra a tag: %q", list)
	}
	if got := lipgloss.Width(list); got > width {
		t.Fatalf("item tem %d colunas, limite %d", got, width)
	}
}
