package component_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
)

// A geometria é o que quebra primeiro em uma TUI, e quebra silenciosamente:
// uma linha uma coluna mais curta que as outras só aparece como um painel
// vizinho deslocado, três telas adiante. Estes testes travam o invariante.

func TestPanelGeometria(t *testing.T) {
	th := theme.Default()

	sizes := []struct{ w, h int }{
		{20, 5}, {34, 8}, {40, 12}, {80, 24}, {120, 40},
	}
	variants := []struct {
		name  string
		panel component.Panel
	}{
		{"sem título nem rodapé", component.Panel{}},
		{"com título", component.Panel{Title: "favoritas"}},
		{"com título e glyph", component.Panel{Title: "favoritas", Glyph: "★"}},
		{"com rodapé", component.Panel{Title: "resultados", Footer: "12"}},
		{"rodapé longo", component.Panel{Title: "resultados", Footer: "mostrando 60 de 141"}},
		{"título longo demais", component.Panel{Title: strings.Repeat("titulo ", 12)}},
		{"focado", component.Panel{Title: "busca", Focused: true}},
	}

	content := strings.Repeat("linha de conteúdo\n", 6)

	for _, v := range variants {
		for _, s := range sizes {
			t.Run(v.name, func(t *testing.T) {
				p := v.panel
				p.Width, p.Height = s.w, s.h

				out := p.Render(th, content)
				lines := strings.Split(out, "\n")

				if len(lines) != s.h {
					t.Fatalf("%dx%d: %d linhas, quero %d", s.w, s.h, len(lines), s.h)
				}
				for i, line := range lines {
					if got := lipgloss.Width(line); got != s.w {
						t.Errorf("%dx%d: linha %d tem largura %d, quero %d\n%q",
							s.w, s.h, i, got, s.w, stripANSI(line))
					}
				}
			})
		}
	}
}

func TestPanelLarguraMinima(t *testing.T) {
	th := theme.Default()
	// Abaixo de 4 colunas não há miolo: o painel se recusa a desenhar em vez
	// de emitir bordas sobrepostas.
	for w := 0; w < 4; w++ {
		p := component.Panel{Width: w, Height: 5}
		if got := p.Render(th, "x"); got != "" {
			t.Errorf("largura %d rendeu %q, quero vazio", w, got)
		}
	}
}

func TestSpreadRespeitaLargura(t *testing.T) {
	tests := []struct {
		name        string
		left, right string
		width       int
	}{
		{"cabe folgado", "esquerda", "direita", 40},
		{"exatamente justo", "esquerda", "direita", 15},
		{"estoura pela esquerda", "um texto bem longo à esquerda", "direita", 20},
		{"estoura pelos dois lados", "esquerda longa", "direita longa", 10},
		{"largura zero", "a", "b", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := lipgloss.Width(component.Spread(tc.left, tc.right, tc.width))
			if got > tc.width {
				t.Errorf("Spread devolveu largura %d, excede %d", got, tc.width)
			}
		})
	}
}

func TestStatusbarNuncaExcedeALargura(t *testing.T) {
	th := theme.Default()
	hints := []component.Hint{
		{Key: "/", Label: "buscar"},
		{Key: "↵", Label: "executar"},
		{Key: "f", Label: "favoritar"},
		{Key: "↹", Label: "painel"},
		{Key: "hjkl", Label: "navegar"},
		{Key: "r", Label: "recarregar"},
		{Key: "?", Label: "ajuda"},
		{Key: "q", Label: "sair"},
	}

	for _, w := range []int{20, 40, 60, 80, 120, 200} {
		bar := component.Statusbar{
			Hints: hints,
			Right: "declara um incidente e notifica o plantão",
		}.Render(th, w)

		for i, line := range strings.Split(bar, "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Errorf("largura %d: linha %d tem %d colunas\n%q", w, i, got, stripANSI(line))
			}
		}
	}
}

// stripANSI remove as sequências de escape para que a mensagem de falha
// mostre o texto e não o código de cor.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
