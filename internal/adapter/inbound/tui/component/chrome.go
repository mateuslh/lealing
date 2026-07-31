package component

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
)

// Spread posiciona um bloco à esquerda e outro à direita dentro da largura,
// preenchendo o miolo com espaço.
//
// Mede com lipgloss.Width (que ignora sequências ANSI) — usar len() aqui
// contaria os bytes de cor e desalinharia tudo. Quando não cabe, o lado
// direito tem prioridade: ele costuma carregar o número, e um número pela
// metade é pior que um título truncado.
func Spread(left, right string, width int) string {
	if width <= 0 {
		// Sem colunas não há o que posicionar. Truncar para zero devolveria
		// só as reticências, que ainda ocupam uma coluna.
		return ""
	}
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	if lw+rw >= width {
		if rw >= width {
			return truncate.StringWithTail(right, uint(max(width, 0)), "…")
		}
		left = truncate.StringWithTail(left, uint(max(width-rw-1, 0)), "…")
		lw = lipgloss.Width(left)
	}
	gap := width - lw - rw
	if gap < 0 {
		gap = 0
	}
	return left + strings.Repeat(" ", gap) + right
}

// Topbar é a faixa superior fixa: marca, trilha de navegação e metadados.
type Topbar struct {
	Trail []string
	Meta  []string
}

// Render desenha a topbar e a régua que a separa do conteúdo.
func (t Topbar) Render(th *theme.Theme, width int) string {
	if width <= 0 {
		return ""
	}

	left := th.Logo()
	if len(t.Trail) > 0 {
		sep := th.Ghost.Render(" › ")
		crumbs := make([]string, len(t.Trail))
		for i, c := range t.Trail {
			style := th.Dim
			if i == len(t.Trail)-1 {
				style = th.Body // o nível atual é o único em destaque
			}
			crumbs[i] = style.Render(c)
		}
		left += th.Ghost.Render("  ·  ") + strings.Join(crumbs, sep)
	}

	right := ""
	if len(t.Meta) > 0 {
		parts := make([]string, len(t.Meta))
		for i, m := range t.Meta {
			parts[i] = th.Meta.Render(m)
		}
		right = strings.Join(parts, th.Ghost.Render(" · "))
	}

	inner := max(width-4, 0) // desconta o padding horizontal do estilo
	bar := th.Topbar.Render(Spread(left, right, inner))
	// A régua em degradê é o que dá acabamento à faixa: uma linha sólida
	// pareceria um separador de terminal genérico.
	rule := theme.Rule(width, []string{
		th.Gradient[0],
		theme.Hex(th.Palette.Border),
		th.Gradient[len(th.Gradient)-1],
	}, "─")

	return lipgloss.JoinVertical(lipgloss.Left, bar, rule)
}

// Statusbar é a faixa inferior com os atalhos disponíveis.
type Statusbar struct {
	Hints []Hint
	// Right é o texto opcional da direita (contador, estado, mensagem).
	Right string
	// Tone colore o Right; vazio usa o tom neutro.
	Tone lipgloss.TerminalColor
}

// Hint é um par tecla/rótulo pronto para render.
type Hint struct {
	Key   string
	Label string
}

// Render desenha a statusbar, descartando atalhos que não couberem.
//
// Descartar do fim é intencional: as telas listam os atalhos por ordem de
// importância, então o que sobra em uma janela estreita é o essencial.
func (s Statusbar) Render(th *theme.Theme, width int) string {
	if width <= 0 {
		return ""
	}

	inner := max(width-4, 0)
	rightW := lipgloss.Width(s.Right)
	if rightW > 0 {
		rightW += 2
	}

	var (
		b     strings.Builder
		used  int
		sepW  = 2
		avail = max(inner-rightW, 0)
	)
	for i, h := range s.Hints {
		item := th.Key(h.Key, h.Label)
		w := lipgloss.Width(item)
		if i > 0 {
			w += sepW
		}
		if used+w > avail {
			break
		}
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(item)
		used += w
	}

	right := s.Right
	if right != "" {
		style := th.Dim
		if s.Tone != nil {
			style = lipgloss.NewStyle().Foreground(s.Tone)
		}
		right = style.Render(right)
	}

	rule := th.Divider.Render(strings.Repeat("─", width))
	return lipgloss.JoinVertical(lipgloss.Left, rule, th.Status.Render(Spread(b.String(), right, inner)))
}
