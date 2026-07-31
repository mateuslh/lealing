package component_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/sdk/component"
)

func TestComponentesPublicosRespeitamGeometria(t *testing.T) {
	theme := component.DefaultTheme()
	content := component.BarChart{Width: 26, Rows: []component.BarRow{{Label: "rótulo longo", Value: "$12", Fraction: .7}}}.Render(theme)
	panel := component.Panel{Title: "dados", Width: 30, Height: 5}.Render(theme, content)
	for i, line := range strings.Split(panel, "\n") {
		if width := lipgloss.Width(line); width > 30 {
			t.Errorf("linha %d = %d colunas", i, width)
		}
	}
}
