package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/yago/smc-mixer/dispatcher"
	"github.com/yago/smc-mixer/streams"
)

// pickupLabel returns the 4-char volume/sync string for the bottom of a strip.
// When synced (or unbound): " 60%". When awaiting pickup: blinking "↑60%" / "↓60%".
func pickupLabel(c dispatcher.Channel) string {
	pct := fmt.Sprintf("%3.0f%%", c.ActualVolume*100)
	if c.StreamID == nil || c.Synced {
		return pct
	}
	// Blink the direction arrow at ~300ms on / 300ms off.
	blink := time.Now().UnixMilli()%600 < 300
	var arrow string
	switch {
	case !blink:
		arrow = " "
	case c.FaderPos > c.ActualVolume+2.0/127.0:
		arrow = "↓" // fader is above actual — move it down
	case c.FaderPos < c.ActualVolume-2.0/127.0:
		arrow = "↑" // fader is below actual — move it up
	default:
		arrow = "~" // nearly there
	}
	style := lipgloss.NewStyle().Foreground(colorWarn).Bold(true)
	return style.Render(arrow) + pct[:3]
}

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

// kindTag returns a colored 5-char type tag and its style for the bind panel.
func kindTag(k streams.NodeKind) (string, lipgloss.Style) {
	switch k {
	case streams.KindMic:
		return "[mic]", lipgloss.NewStyle().Foreground(colorMic).Bold(true)
	case streams.KindSink:
		return "[out]", lipgloss.NewStyle().Foreground(colorSink).Bold(true)
	default:
		return "[src]", lipgloss.NewStyle().Foreground(colorSrc).Bold(true)
	}
}

// kindHeader returns a full-width dim section label for the bind panel.
func kindHeader(k streams.NodeKind, _ int) string {
	var label string
	var color lipgloss.Color
	switch k {
	case streams.KindMic:
		label, color = " Microphones", colorMic
	case streams.KindSink:
		label, color = " Outputs", colorSink
	default:
		label, color = " Sources", colorSrc
	}
	style := lipgloss.NewStyle().Foreground(color).Faint(true)
	return style.Render(label)
}

// truncate clips s to max visible chars, appending "…" if clipped.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
