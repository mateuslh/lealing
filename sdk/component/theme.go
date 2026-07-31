// Package component oferece primitivas visuais estáveis para tools screen-v1.
// A engine envia as cores; a tool materializa estilos sem importar seu tema
// interno.
package component

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/sdk/protocol"
)

type Theme struct {
	Primary, Secondary, Accent lipgloss.Color
	Success, Warning, Danger   lipgloss.Color
	Text, Muted, Faint         lipgloss.Color
	Border, BorderFocus        lipgloss.Color
	Surface                    lipgloss.Color

	Title, Body, Dim, Ghost lipgloss.Style
}

// ThemeFrom materializa a paleta negociada. Campos vazios recebem o tema
// público padrão para uma tool continuar legível em hosts incompletos.
func ThemeFrom(theme protocol.Theme) *Theme {
	fallback := DefaultTheme()
	color := func(value string, defaultColor lipgloss.Color) lipgloss.Color {
		if value == "" {
			return defaultColor
		}
		return lipgloss.Color(value)
	}
	t := &Theme{
		Primary: color(theme.Primary, fallback.Primary), Secondary: color(theme.Secondary, fallback.Secondary),
		Accent: color(theme.Accent, fallback.Accent), Success: color(theme.Success, fallback.Success),
		Warning: color(theme.Warning, fallback.Warning), Danger: color(theme.Danger, fallback.Danger),
		Text: color(theme.Text, fallback.Text), Muted: color(theme.Muted, fallback.Muted),
		Faint: color(theme.Faint, fallback.Faint), Border: color(theme.Border, fallback.Border),
		BorderFocus: color(theme.Border, fallback.BorderFocus), Surface: color(theme.Surface, fallback.Surface),
	}
	t.materialize()
	return t
}

// DefaultTheme é a reserva usada em testes e em hosts que omitiram uma cor.
func DefaultTheme() *Theme {
	t := &Theme{
		Primary: "#7AA2F7", Secondary: "#BB9AF7", Accent: "#7DCFFF",
		Success: "#9ECE6A", Warning: "#E0AF68", Danger: "#F7768E",
		Text: "#C8D0E4", Muted: "#7A849F", Faint: "#4A5370",
		Border: "#232A3B", BorderFocus: "#3D4869", Surface: "#141926",
	}
	t.materialize()
	return t
}

func (t *Theme) materialize() {
	t.Title = lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	t.Body = lipgloss.NewStyle().Foreground(t.Text)
	t.Dim = lipgloss.NewStyle().Foreground(t.Muted)
	t.Ghost = lipgloss.NewStyle().Foreground(t.Faint)
}
