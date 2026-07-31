package component

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
)

func Spread(left, right string, width int) string {
	if width <= 0 {
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
	return left + strings.Repeat(" ", max(width-lw-rw, 0)) + right
}

func PadRight(value string, width int) string {
	if gap := width - lipgloss.Width(value); gap > 0 {
		return value + strings.Repeat(" ", gap)
	}
	return value
}

func padLeft(value string, width int) string {
	if gap := width - lipgloss.Width(value); gap > 0 {
		return strings.Repeat(" ", gap) + value
	}
	return value
}

func TruncateTail(value string, width int) string {
	if lipgloss.Width(value) <= width {
		return value
	}
	return truncate.StringWithTail(value, uint(max(width, 0)), "…")
}

func Center(width, height int, lines ...string) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	fitted := make([]string, len(lines))
	for i, line := range lines {
		fitted[i] = TruncateTail(line, width)
	}
	block := lipgloss.JoinVertical(lipgloss.Center, fitted...)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, block)
}
