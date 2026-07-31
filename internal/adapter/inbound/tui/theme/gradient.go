package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/lucasb-eyer/go-colorful"
)

// Ramp pré-calcula um degradê com n passos entre os pontos de parada.
//
// A interpolação acontece em Lab, não em RGB: um gradiente RGB entre azul e
// rosa passa por um cinza lavado no meio, enquanto Lab é perceptualmente
// uniforme e mantém a saturação ao longo de toda a régua.
func Ramp(stops []string, n int) []lipgloss.Color {
	if n <= 0 {
		return nil
	}
	parsed := parseStops(stops)
	if len(parsed) == 0 {
		return make([]lipgloss.Color, n)
	}
	if n == 1 || len(parsed) == 1 {
		out := make([]lipgloss.Color, n)
		c := lipgloss.Color(parsed[0].Hex())
		for i := range out {
			out[i] = c
		}
		return out
	}

	out := make([]lipgloss.Color, n)
	// segments é o número de trechos entre stops; t percorre [0, segments].
	segments := float64(len(parsed) - 1)
	for i := range n {
		t := float64(i) / float64(n-1) * segments
		idx := int(t)
		if idx >= len(parsed)-1 {
			idx = len(parsed) - 2
		}
		local := t - float64(idx)
		blended := parsed[idx].BlendLab(parsed[idx+1], local).Clamped()
		out[i] = lipgloss.Color(blended.Hex())
	}
	return out
}

// Text aplica um gradiente horizontal caractere a caractere.
//
// Opera sobre runes, não bytes, para não partir acentos nem glyphs Unicode
// no meio — o que produziria lixo no terminal.
func Text(s string, stops []string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return s
	}
	ramp := Ramp(stops, len(runes))

	var b strings.Builder
	b.Grow(len(s) * 12) // cada rune ganha uma sequência ANSI
	for i, r := range runes {
		if r == ' ' || r == '\n' {
			// Espaço não carrega cor de primeiro plano: pintar é desperdício
			// de bytes e atrapalha a seleção com o mouse.
			b.WriteRune(r)
			continue
		}
		b.WriteString(lipgloss.NewStyle().Foreground(ramp[i]).Render(string(r)))
	}
	return b.String()
}

// Block aplica o gradiente a cada linha de um texto multilinha, mantendo o
// degradê alinhado pela coluna — é o que faz um logo ASCII parecer uma peça
// única em vez de várias linhas coloridas por acaso.
func Block(s string, stops []string) string {
	lines := strings.Split(s, "\n")
	width := 0
	for _, ln := range lines {
		width = max(width, len([]rune(ln)))
	}
	if width == 0 {
		return s
	}
	ramp := Ramp(stops, width)

	var b strings.Builder
	for i, ln := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		runes := []rune(ln)
		for col, r := range runes {
			if r == ' ' {
				b.WriteRune(r)
				continue
			}
			b.WriteString(lipgloss.NewStyle().Foreground(ramp[col]).Render(string(r)))
		}
		// Completa a linha até a largura do bloco. Sem isso, um alinhamento
		// centralizado aplicado depois trataria cada linha como um bloco de
		// largura própria e desencontraria as colunas do desenho.
		if pad := width - len(runes); pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
	}
	return b.String()
}

// Rule desenha uma régua horizontal com degradê, usada como separador.
func Rule(width int, stops []string, glyph string) string {
	if width <= 0 {
		return ""
	}
	if glyph == "" {
		glyph = "─"
	}
	return Text(strings.Repeat(glyph, width), stops)
}

// Fade interpola do tom cheio até o apagado, para vinhetas e barras que
// devem sumir na borda.
func Fade(s string, from, to lipgloss.TerminalColor) string {
	fromHex, ok1 := hexOf(from)
	toHex, ok2 := hexOf(to)
	if !ok1 || !ok2 {
		return s
	}
	return Text(s, []string{fromHex, toHex})
}

// parseStops converte hexadecimais em cores, descartando os inválidos.
func parseStops(stops []string) []colorful.Color {
	out := make([]colorful.Color, 0, len(stops))
	for _, s := range stops {
		if c, err := colorful.Hex(s); err == nil {
			out = append(out, c)
		}
	}
	return out
}

// Hex resolve uma cor do lipgloss para hexadecimal, respeitando o modo do
// terminal em cores adaptativas. Devolve string vazia para tipos que não
// carregam um valor RGB conhecido.
func Hex(c lipgloss.TerminalColor) string {
	hex, _ := hexOf(c)
	return hex
}

// hexOf extrai o hexadecimal de uma cor do lipgloss quando possível.
func hexOf(c lipgloss.TerminalColor) (string, bool) {
	switch v := c.(type) {
	case lipgloss.Color:
		return string(v), true
	case lipgloss.AdaptiveColor:
		if lipgloss.HasDarkBackground() {
			return v.Dark, true
		}
		return v.Light, true
	default:
		return "", false
	}
}
