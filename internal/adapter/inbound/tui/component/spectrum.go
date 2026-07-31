package component

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/core/port/inbound"
)

// SpectrumBar desenha a composição do catálogo como uma única barra
// segmentada, cada categoria na sua cor e com largura proporcional.
//
// É uma leitura que uma lista de números não dá: em um segundo o usuário vê
// se o acervo está concentrado em dois domínios ou bem distribuído.
type SpectrumBar struct {
	Items []inbound.CategoryView
	Width int
	// Highlight destaca uma categoria escurecendo as demais; vazio pinta
	// todas com intensidade cheia.
	Highlight inbound.CategoryView
}

// Render devolve a barra pronta.
func (s SpectrumBar) Render(th *theme.Theme) string {
	if s.Width <= 0 || len(s.Items) == 0 {
		return ""
	}

	total := 0
	for _, it := range s.Items {
		total += it.Count
	}
	if total == 0 {
		return th.Ghost.Render(strings.Repeat("─", s.Width))
	}

	var (
		b    strings.Builder
		used int
		// Distribui o resto da divisão inteira acumulando o erro, em vez de
		// arredondar cada segmento isolado — assim a barra sempre fecha
		// exatamente na largura pedida.
		acc float64
	)
	for i, it := range s.Items {
		acc += float64(it.Count) / float64(total) * float64(s.Width)
		want := int(acc) - used
		if i == len(s.Items)-1 {
			want = s.Width - used // o último absorve qualquer sobra
		}
		if want <= 0 {
			continue
		}

		style := lipgloss.NewStyle().Foreground(th.SpectrumAt(it.Accent))
		if s.Highlight.ID != "" && s.Highlight.ID != it.ID {
			style = lipgloss.NewStyle().Foreground(th.Faint)
		}
		b.WriteString(style.Render(strings.Repeat("█", want)))
		used += want
	}

	return b.String()
}

// Legend renderiza os rótulos da barra em linha, na ordem das categorias,
// descartando o que não couber.
func (s SpectrumBar) Legend(th *theme.Theme) string {
	if s.Width <= 0 {
		return ""
	}

	var (
		parts []string
		used  int
	)
	for _, it := range s.Items {
		dot := lipgloss.NewStyle().Foreground(th.SpectrumAt(it.Accent)).Render("▪")
		label := dot + " " + th.Ghost.Render(it.Name)
		w := lipgloss.Width(label) + 2
		if used+w > s.Width {
			break
		}
		parts = append(parts, label)
		used += w
	}
	return strings.Join(parts, "  ")
}
