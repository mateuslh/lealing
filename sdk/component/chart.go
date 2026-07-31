package component

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type Meter struct {
	Percent  float64
	Width    int
	Tone     lipgloss.TerminalColor
	Inverted bool
}

func (m Meter) Render(theme *Theme) string {
	if m.Width <= 0 {
		return ""
	}
	percent := min(max(m.Percent, 0), 100)
	tone := m.Tone
	if tone == nil {
		high, low := lipgloss.TerminalColor(theme.Danger), lipgloss.TerminalColor(theme.Success)
		if m.Inverted {
			high, low = low, high
		}
		switch {
		case percent >= 90:
			tone = high
		case percent >= 70:
			tone = theme.Warning
		default:
			tone = low
		}
	}
	exact := percent / 100 * float64(m.Width)
	full := int(exact)
	remainder := exact - float64(full)
	filled := lipgloss.NewStyle().Foreground(tone)
	empty := lipgloss.NewStyle().Foreground(theme.Border)
	var builder strings.Builder
	builder.WriteString(filled.Render(strings.Repeat("█", min(full, m.Width))))
	rest := m.Width - full
	if rest > 0 {
		blocks := []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}
		index := int(remainder * 8)
		if index > 0 && index < len(blocks) {
			builder.WriteString(filled.Render(blocks[index]))
			rest--
		}
		builder.WriteString(empty.Render(strings.Repeat("─", max(rest, 0))))
	}
	return builder.String()
}

type Sparkline struct {
	Values    []float64
	Width     int
	Tone      lipgloss.TerminalColor
	Highlight bool
}

var sparkRunes = []rune("▁▂▃▄▅▆▇█")

func (s Sparkline) Render(theme *Theme) string {
	if s.Width <= 0 || len(s.Values) == 0 {
		return ""
	}
	values := s.Values
	if len(values) > s.Width {
		values = downsample(values, s.Width)
	}
	maximum := 0.0
	for _, value := range values {
		maximum = max(maximum, value)
	}
	tone := s.Tone
	if tone == nil {
		tone = theme.Primary
	}
	style := lipgloss.NewStyle().Foreground(tone)
	last := style.Bold(true)
	var builder strings.Builder
	for i, value := range values {
		index := 0
		if maximum > 0 {
			index = min(max(int(value/maximum*float64(len(sparkRunes)-1)), 0), len(sparkRunes)-1)
			if value > 0 && index == 0 {
				index = 1
			}
		}
		renderer := style
		if s.Highlight && i == len(values)-1 {
			renderer = last
		}
		builder.WriteString(renderer.Render(string(sparkRunes[index])))
	}
	return builder.String()
}

func downsample(values []float64, width int) []float64 {
	out := make([]float64, width)
	per := float64(len(values)) / float64(width)
	for i := range width {
		start, end := int(float64(i)*per), int(float64(i+1)*per)
		end = min(end, len(values))
		if start >= end {
			if start < len(values) {
				out[i] = values[start]
			}
			continue
		}
		for _, value := range values[start:end] {
			out[i] += value
		}
		out[i] /= float64(end - start)
	}
	return out
}

type BarRow struct {
	Label, Value string
	Fraction     float64
	Tone         lipgloss.TerminalColor
}

type BarChart struct {
	Rows       []BarRow
	Width      int
	LabelWidth int
}

func (chart BarChart) Render(theme *Theme) string {
	if len(chart.Rows) == 0 || chart.Width <= 0 {
		return ""
	}
	labelWidth, valueWidth := chart.LabelWidth, 0
	for _, row := range chart.Rows {
		if chart.LabelWidth == 0 {
			labelWidth = max(labelWidth, lipgloss.Width(row.Label))
		}
		valueWidth = max(valueWidth, lipgloss.Width(row.Value))
	}
	labelWidth = min(labelWidth, chart.Width/3)
	barWidth := chart.Width - labelWidth - valueWidth - 3
	lines := make([]string, len(chart.Rows))
	for i, row := range chart.Rows {
		if barWidth < 3 {
			lines[i] = Spread(theme.Dim.Render(row.Label), theme.Body.Render(row.Value), chart.Width)
			continue
		}
		tone := row.Tone
		if tone == nil {
			tone = theme.Primary
		}
		bar := Meter{Percent: min(max(row.Fraction, 0), 1) * 100, Width: barWidth, Tone: tone}.Render(theme)
		lines[i] = theme.Dim.Render(PadRight(TruncateTail(row.Label, labelWidth), labelWidth)) + " " + bar + " " + theme.Body.Render(padLeft(row.Value, valueWidth))
	}
	return strings.Join(lines, "\n")
}
