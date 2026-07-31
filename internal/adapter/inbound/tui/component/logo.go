// Package component reúne os widgets reutilizáveis da TUI. Nenhum deles
// guarda estado de aplicação: recebem dados e devolvem string.
package component

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
)

// As três variantes do wordmark. A escolha é por largura disponível: um logo
// que estoura a coluna quebra o layout inteiro, então há sempre um degrau
// menor para onde cair.
const (
	logoFull = "" +
		"██╗     ███████╗ █████╗ ██╗     ██╗███╗   ██╗ ██████╗\n" +
		"██║     ██╔════╝██╔══██╗██║     ██║████╗  ██║██╔════╝\n" +
		"██║     █████╗  ███████║██║     ██║██╔██╗ ██║██║  ███╗\n" +
		"██║     ██╔══╝  ██╔══██║██║     ██║██║╚██╗██║██║   ██║\n" +
		"███████╗███████╗██║  ██║███████╗██║██║ ╚████║╚██████╔╝\n" +
		"╚══════╝╚══════╝╚═╝  ╚═╝╚══════╝╚═╝╚═╝  ╚═══╝ ╚═════╝"

	logoMid = "" +
		"█   █▀▀ ▄▀█ █   █ █▄ █ █▀▀\n" +
		"█▄▄ █▄▄ █▀█ █▄▄ █ █ ▀█ █▄█"

	logoMark = "◈ lealing"
)

// Larguras mínimas de cada variante, medidas com folga para o padding do
// painel que as contém.
const (
	logoFullWidth = 54
	logoMidWidth  = 27
)

// Logo renderiza o wordmark em gradiente, escolhendo a maior variante que
// couber na largura informada.
func Logo(t *theme.Theme, width int) string {
	switch {
	case width >= logoFullWidth:
		return theme.Block(logoFull, t.Gradient)
	case width >= logoMidWidth:
		return theme.Block(logoMid, t.Gradient)
	default:
		return theme.Text(logoMark, t.Gradient)
	}
}

// LogoWidth informa a largura da variante que Logo escolheria, sem
// renderizar — útil para o layout decidir antes de compor.
func LogoWidth(width int) int {
	switch {
	case width >= logoFullWidth:
		return logoFullWidth
	case width >= logoMidWidth:
		return logoMidWidth
	default:
		return lipgloss.Width(logoMark)
	}
}

// Hero é o cartão de abertura da home: wordmark, saudação e uma régua em
// degradê que amarra o bloco à largura do conteúdo.
type Hero struct {
	Greeting string
	Tagline  string
	Stats    []string
}

// Render desenha o hero centralizado na largura disponível.
func (h Hero) Render(t *theme.Theme, width int) string {
	if width <= 0 {
		return ""
	}

	parts := []string{Logo(t, width)}

	if h.Greeting != "" {
		parts = append(parts, "", t.Title.Render(h.Greeting))
	}
	if h.Tagline != "" {
		parts = append(parts, t.Subtitle.Render(h.Tagline))
	}
	if len(h.Stats) > 0 {
		parts = append(parts, "", h.renderStats(t))
	}

	block := lipgloss.JoinVertical(lipgloss.Center, parts...)
	return lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(block)
}

// renderStats compõe os números do cabeçalho separados por um ponto médio.
func (h Hero) renderStats(t *theme.Theme) string {
	sep := t.Ghost.Render(" · ")
	rendered := make([]string, len(h.Stats))
	for i, s := range h.Stats {
		rendered[i] = t.Dim.Render(s)
	}
	return strings.Join(rendered, sep)
}
