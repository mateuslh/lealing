// Package sanitize protege o chrome e o terminal de sequências emitidas por
// tools externas. Somente SGR de estilo é preservado.
package sanitize

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Frame remove controles perigosos e recorta o resultado ao retângulo da
// área central. O recorte é ANSI-aware e nunca usa bytes como colunas.
func Frame(body string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	safe := ANSI(body)
	source := strings.Split(safe, "\n")
	if len(source) > height {
		source = source[:height]
	}
	for i := range source {
		source[i] = ansi.Truncate(source[i], width, "")
	}
	return strings.Join(source, "\n")
}

// Plain prepara texto que entra no chrome ou em um modal da engine. Nem SGR
// é necessário nesses pontos: o host aplica seu próprio estilo.
func Plain(input string, width int) string {
	plain := ansi.Strip(ANSI(input))
	plain = strings.Join(strings.Fields(plain), " ")
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(plain, width, "")
}

// ANSI preserva somente CSI ... m com parâmetros SGR conhecidos. OSC,
// cursor, clear, alt-screen, modos e sequências incompletas desaparecem.
func ANSI(input string) string {
	var out strings.Builder
	out.Grow(len(input))
	for i := 0; i < len(input); {
		b := input[i]
		switch {
		case b == 0x1b:
			if i+1 >= len(input) {
				i++
				continue
			}
			switch input[i+1] {
			case '[':
				end := csiEnd(input, i+2)
				if end < 0 {
					return out.String()
				}
				if input[end] == 'm' && allowedSGR(input[i+2:end]) {
					out.WriteString(input[i : end+1])
				}
				i = end + 1
			case ']':
				i = skipOSC(input, i+2)
			default:
				// ESC seguido de um byte é uma sequência de dois bytes. Descartar
				// ambos evita que o segundo vire texto visível no chrome.
				i += 2
			}

		case b == '\n':
			out.WriteByte(b)
			i++
		case b == '\t':
			// Tabs têm largura dependente da coluna do terminal; quatro espaços
			// tornam a geometria determinística antes do recorte.
			out.WriteString("    ")
			i++
		case b < 0x20 || b == 0x7f:
			i++
		case b >= utf8.RuneSelf:
			r, size := utf8.DecodeRuneInString(input[i:])
			if r != utf8.RuneError || size > 1 {
				if !unicode.IsControl(r) {
					out.WriteRune(r)
				}
			}
			i += size
		default:
			out.WriteByte(b)
			i++
		}
	}
	return out.String()
}

func csiEnd(input string, start int) int {
	for i := start; i < len(input); i++ {
		if input[i] >= 0x40 && input[i] <= 0x7e {
			return i
		}
	}
	return -1
}

func skipOSC(input string, start int) int {
	for i := start; i < len(input); i++ {
		switch {
		case input[i] == 0x07:
			return i + 1
		case input[i] == 0x1b && i+1 < len(input) && input[i+1] == '\\':
			return i + 2
		}
	}
	return len(input)
}

func allowedSGR(parameters string) bool {
	if parameters == "" {
		return true // ESC[m é reset
	}
	parts := strings.Split(parameters, ";")
	values := make([]int, len(parts))
	for i, part := range parts {
		if part == "" {
			values[i] = 0
			continue
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return false
		}
		values[i] = value
	}
	for i := 0; i < len(values); i++ {
		value := values[i]
		switch {
		case value == 0, value == 1, value == 3, value == 4,
			value == 22, value == 23, value == 24,
			value >= 30 && value <= 37, value == 39,
			value >= 40 && value <= 47, value == 49,
			value >= 90 && value <= 97,
			value >= 100 && value <= 107:
			continue
		case value == 38 || value == 48:
			if i+1 >= len(values) {
				return false
			}
			switch values[i+1] {
			case 5:
				if i+2 >= len(values) || values[i+2] < 0 || values[i+2] > 255 {
					return false
				}
				i += 2
			case 2:
				if i+4 >= len(values) {
					return false
				}
				for _, channel := range values[i+2 : i+5] {
					if channel < 0 || channel > 255 {
						return false
					}
				}
				i += 4
			default:
				return false
			}
		default:
			return false
		}
	}
	return true
}
