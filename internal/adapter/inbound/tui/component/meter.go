package component

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
)

// Meter é uma barra de preenchimento para percentuais (cotas, carga).
type Meter struct {
	// Percent vai de 0 a 100; valores fora são recortados.
	Percent float64
	Width   int
	// Tone força a cor; nil escolhe pelo nível de preenchimento.
	Tone lipgloss.TerminalColor
	// Inverted trata valores altos como bons (carga de bateria) em vez de
	// ruins (cota consumida).
	Inverted bool
}

// Render devolve a barra.
//
// Usa oitavos de bloco para representar a fração residual: com barras
// estreitas, arredondar para o caractere cheio mais próximo faz 12% e 20%
// parecerem idênticos.
func (m Meter) Render(th *theme.Theme) string {
	if m.Width <= 0 {
		return ""
	}
	pct := min(max(m.Percent, 0), 100)

	tone := m.Tone
	if tone == nil {
		tone = m.levelTone(th, pct)
	}

	exact := pct / 100 * float64(m.Width)
	full := int(exact)
	remainder := exact - float64(full)

	var b strings.Builder
	filled := lipgloss.NewStyle().Foreground(tone)
	empty := lipgloss.NewStyle().Foreground(th.Border)

	b.WriteString(filled.Render(strings.Repeat("█", min(full, m.Width))))

	rest := m.Width - full
	if rest > 0 {
		if partial := eighth(remainder); partial != "" {
			b.WriteString(filled.Render(partial))
			rest--
		}
		if rest > 0 {
			b.WriteString(empty.Render(strings.Repeat("─", rest)))
		}
	}
	return b.String()
}

// levelTone escolhe verde/âmbar/vermelho conforme o preenchimento.
func (m Meter) levelTone(th *theme.Theme, pct float64) lipgloss.TerminalColor {
	high, low := th.Danger, th.Success
	if m.Inverted {
		high, low = th.Success, th.Danger
	}
	switch {
	case pct >= 90:
		return high
	case pct >= 70:
		return th.Warning
	default:
		return low
	}
}

// eighth mapeia uma fração no bloco parcial correspondente.
func eighth(f float64) string {
	blocks := []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}
	i := int(f * 8)
	if i <= 0 || i >= len(blocks) {
		return ""
	}
	return blocks[i]
}

// Sparkline desenha uma série numérica em uma única linha.
type Sparkline struct {
	Values []float64
	Width  int
	Tone   lipgloss.TerminalColor
	// Highlight destaca o último ponto, que é o "agora".
	Highlight bool
}

// sparkRunes vão do mais baixo ao mais alto.
var sparkRunes = []rune("▁▂▃▄▅▆▇█")

// Render devolve a sparkline.
//
// Quando há mais pontos que colunas, agrega por média em vez de amostrar:
// amostrar esconderia justamente os picos que a série existe para mostrar.
func (s Sparkline) Render(th *theme.Theme) string {
	if s.Width <= 0 || len(s.Values) == 0 {
		return ""
	}

	values := s.Values
	if len(values) > s.Width {
		values = downsample(values, s.Width)
	}

	maxV := 0.0
	for _, v := range values {
		maxV = max(maxV, v)
	}

	tone := s.Tone
	if tone == nil {
		tone = th.Primary
	}
	style := lipgloss.NewStyle().Foreground(tone)
	last := lipgloss.NewStyle().Foreground(tone).Bold(true)

	var b strings.Builder
	for i, v := range values {
		idx := 0
		if maxV > 0 {
			idx = int(v / maxV * float64(len(sparkRunes)-1))
			idx = min(max(idx, 0), len(sparkRunes)-1)
			// Qualquer valor não-nulo merece pelo menos o traço mais baixo:
			// um dia com consumo real não pode desaparecer da série.
			if v > 0 && idx == 0 {
				idx = 1
			}
		}
		r := string(sparkRunes[idx])
		if s.Highlight && i == len(values)-1 {
			b.WriteString(last.Render(r))
			continue
		}
		b.WriteString(style.Render(r))
	}
	return b.String()
}

// downsample comprime a série na largura disponível, por média de balde.
func downsample(values []float64, width int) []float64 {
	out := make([]float64, width)
	per := float64(len(values)) / float64(width)
	for i := range width {
		start := int(float64(i) * per)
		end := int(float64(i+1) * per)
		if end > len(values) {
			end = len(values)
		}
		if start >= end {
			if start < len(values) {
				out[i] = values[start]
			}
			continue
		}
		sum := 0.0
		for _, v := range values[start:end] {
			sum += v
		}
		out[i] = sum / float64(end-start)
	}
	return out
}

// BarRow é uma linha de gráfico de barras horizontal com rótulo e valor.
type BarRow struct {
	Label string
	Value string
	// Fraction vai de 0 a 1 e define o comprimento da barra.
	Fraction float64
	Tone     lipgloss.TerminalColor
}

// BarChart desenha barras horizontais comparáveis entre si.
type BarChart struct {
	Rows  []BarRow
	Width int
	// LabelWidth fixa a coluna de rótulos; zero calcula pelo maior.
	LabelWidth int
}

// Render devolve o gráfico.
func (c BarChart) Render(th *theme.Theme) string {
	if len(c.Rows) == 0 || c.Width <= 0 {
		return ""
	}

	labelW := c.LabelWidth
	valueW := 0
	for _, r := range c.Rows {
		if c.LabelWidth == 0 {
			labelW = max(labelW, lipgloss.Width(r.Label))
		}
		valueW = max(valueW, lipgloss.Width(r.Value))
	}
	labelW = min(labelW, c.Width/3)

	barW := c.Width - labelW - valueW - 3
	if barW < 3 {
		// Sem espaço para barra: degrada para rótulo e número, que ainda
		// informam, em vez de desenhar um traço enganoso.
		lines := make([]string, len(c.Rows))
		for i, r := range c.Rows {
			lines[i] = Spread(th.Dim.Render(r.Label), th.Body.Render(r.Value), c.Width)
		}
		return strings.Join(lines, "\n")
	}

	lines := make([]string, len(c.Rows))
	for i, r := range c.Rows {
		tone := r.Tone
		if tone == nil {
			tone = th.Primary
		}
		bar := Meter{Percent: min(max(r.Fraction, 0), 1) * 100, Width: barW, Tone: tone}.Render(th)
		label := padRight(truncateTail(r.Label, labelW), labelW)
		value := padLeft(r.Value, valueW)
		lines[i] = th.Dim.Render(label) + " " + bar + " " + th.Body.Render(value)
	}
	return strings.Join(lines, "\n")
}

func padLeft(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return strings.Repeat(" ", gap) + s
	}
	return s
}
