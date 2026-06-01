package ui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderBtn returns a 3-char button string "[X]", styled when active.
func renderBtn(label string, active bool, on lipgloss.Style) string {
	s := "[" + label + "]"
	if active {
		return on.Render(s)
	}
	return btnInactive.Render(s)
}

// faderBar returns a horizontal bar of width chars representing val (0.0–1.0).
func faderBar(val float64, width int) string {
	filled := max(0, min(width, int(math.Round(val*float64(width)))))
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// faderRows returns height strings forming a vertical fader (zero at bottom).
// Each row is width chars wide with a centered 4-char block bar.
func faderRows(vol float64, height, width int) []string {
	filled := max(0, min(height, int(math.Round(vol*float64(height)))))
	barW := min(faderBarW, width)
	pad := strings.Repeat(" ", (width-barW)/2)
	rows := make([]string, height)
	for i := range height {
		if i >= height-filled {
			rows[i] = pad + strings.Repeat("█", barW) + pad
		} else {
			rows[i] = pad + strings.Repeat("░", barW) + pad
		}
	}
	return rows
}

// truncate clips s to max visible chars, appending "…" if clipped.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
