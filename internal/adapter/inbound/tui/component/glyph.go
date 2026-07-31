package component

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/core/domain"
)

// Este arquivo concentra a tradução domínio → visual. É a única fronteira
// onde um valor de domain vira glyph ou cor; espalhar esses switches pelas
// telas é como um tema deixa de ser consistente.

// RiskGlyph devolve o marcador de risco e seu estilo.
func RiskGlyph(t *theme.Theme, r domain.Risk) (string, lipgloss.Style) {
	switch r {
	case domain.RiskDestructive:
		return "▲", lipgloss.NewStyle().Foreground(t.Danger)
	case domain.RiskCaution:
		return "●", lipgloss.NewStyle().Foreground(t.Warning)
	default:
		return "·", lipgloss.NewStyle().Foreground(t.Faint)
	}
}

// KindGlyph devolve o marcador do modo de execução.
func KindGlyph(k domain.Kind) string {
	switch k {
	case domain.KindBuiltin:
		return "◈"
	case domain.KindProcess:
		return "▶"
	case domain.KindScript:
		return "⌘"
	case domain.KindRemote:
		return "⇅"
	default:
		return "·"
	}
}

// ToolGlyph devolve o ícone de uma tool, caindo para o da categoria e depois
// para o do Kind.
func ToolGlyph(t domain.Tool, cat domain.Category) string {
	switch {
	case t.Glyph != "":
		return t.Glyph
	case cat.Glyph != "":
		return cat.Glyph
	default:
		return KindGlyph(t.Kind)
	}
}

// PhaseStyle devolve glyph e estilo para a fase de uma sessão.
func PhaseStyle(t *theme.Theme, p domain.Phase) (string, lipgloss.Style) {
	switch p {
	case domain.PhaseRunning:
		return "◐", lipgloss.NewStyle().Foreground(t.Accent)
	case domain.PhaseSucceeded:
		return "✓", lipgloss.NewStyle().Foreground(t.Success)
	case domain.PhaseFailed:
		return "✗", lipgloss.NewStyle().Foreground(t.Danger)
	case domain.PhaseCanceled:
		return "⊘", lipgloss.NewStyle().Foreground(t.Warning)
	default:
		return "○", lipgloss.NewStyle().Foreground(t.Faint)
	}
}
