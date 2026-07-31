package sanitize_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/sanitize"
)

func TestPreservaSomenteEstiloSGR(t *testing.T) {
	input := "\x1b[1mnegrito\x1b[0m \x1b[38;2;10;20;30mcor\x1b[39m"
	if got := sanitize.ANSI(input); got != input {
		t.Errorf("SGR seguro mudou: %q", got)
	}
}

func TestBloqueiaEscapeDeChromeETerminal(t *testing.T) {
	cases := []string{
		"antes\x1b]0;título roubado\x07depois",
		"antes\x1b]52;c;Y2xpcGJvYXJk\x07depois",
		"antes\x1b[2Jdepois",
		"antes\x1b[Hdepois",
		"antes\x1b[?1049hdepois",
		"antes\x1b[?25ldepois",
		"antes\x1b[999zdepois",
	}
	for _, input := range cases {
		got := sanitize.ANSI(input)
		if strings.ContainsRune(got, '\x1b') || got != "antesdepois" {
			t.Errorf("sequência perigosa sobreviveu: %q → %q", input, got)
		}
	}
}

func TestRemoveRetornoEC0QuePodemSobrescreverChrome(t *testing.T) {
	got := sanitize.ANSI("a\rb\x00c\td")
	if got != "abc    d" {
		t.Errorf("resultado = %q", got)
	}
}

func TestPreservaUnicodeSemConfundirContinuacaoUTF8ComC1(t *testing.T) {
	input := "ação · gráfico ▂▃▄ · mouse 🖱️"
	if got := sanitize.ANSI(input); got != input {
		t.Errorf("unicode mudou: %q", got)
	}
}

func TestFrameRecortaLarguraEAlturaComANSI(t *testing.T) {
	body := "\x1b[31mabcdef\x1b[0m\nlinha2\nlinha3"
	got := sanitize.Frame(body, 4, 2)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("%d linhas", len(lines))
	}
	for i, line := range lines {
		if width := lipgloss.Width(line); width > 4 {
			t.Errorf("linha %d tem %d colunas", i, width)
		}
	}
}

func BenchmarkSanitizacaoANSI(b *testing.B) {
	body := strings.Repeat("\x1b[38;2;122;162;247mtexto seguro\x1b[0m ", 300) +
		"\x1b]52;c;Y2xpcGJvYXJk\x07"
	b.ReportAllocs()
	for b.Loop() {
		_ = sanitize.Frame(body, 150, 42)
	}
}
