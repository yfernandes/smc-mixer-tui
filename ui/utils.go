package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/streams"
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
	case c.FaderPos > c.ActualVolume+dispatcher.PickupThreshold:
		arrow = "↓" // fader is above actual — move it down
	case c.FaderPos < c.ActualVolume-dispatcher.PickupThreshold:
		arrow = "↑" // fader is below actual — move it up
	default:
		arrow = "~" // nearly there
	}
	return pickupArrowStyle.Render(arrow) + pct[:3]
}

// renderBtn returns a 3-char button string "[X]", styled when active.
func renderBtn(label string, active bool, on lipgloss.Style) string {
	s := "[" + label + "]"
	if active {
		return on.Render(s)
	}
	return btnInactive.Render(s)
}

// renderBtnFocused is like renderBtn but renders in gold when focused and inactive,
// indicating this button is the currently-selected MIDI nav setting.
func renderBtnFocused(label string, active bool, on lipgloss.Style, focused bool) string {
	s := "[" + label + "]"
	if active {
		return on.Render(s)
	}
	if focused {
		return navFocusStyle.Render(s)
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

// kindTag returns a 5-char type tag and its pre-built style for the bind panel.
func kindTag(k audio.NodeKind) (string, lipgloss.Style) {
	switch k {
	case audio.KindMic:
		return "[mic]", kindTagMicStyle
	case audio.KindSink:
		return "[out]", kindTagSinkStyle
	default:
		return "[src]", kindTagSrcStyle
	}
}

// kindHeader returns a dim section label for the bind panel.
func kindHeader(k audio.NodeKind, _ int) string {
	switch k {
	case audio.KindMic:
		return kindHdrMicStyle.Render(" Microphones")
	case audio.KindSink:
		return kindHdrSinkStyle.Render(" Outputs")
	default:
		return kindHdrSrcStyle.Render(" Sources")
	}
}

// streamSubtitle picks the best secondary description for a stream.
// Prefers per-stream media.name; MPRIS artist/track is per-process and can
// bleed across streams from the same PID (e.g. browser tabs).
func streamSubtitle(es streams.EnrichedStream) string {
	if es.MediaName != "" && es.MediaName != es.Name &&
		es.MediaName != "playback" && es.MediaName != "Playback" {
		return es.MediaName
	}
	if es.Source == streams.SourceMPRIS {
		if es.Artist != "" && es.Track != "" {
			return es.Artist + " – " + es.Track
		}
		return es.Track
	}
	return ""
}

// crossfadeBar renders a knob position indicator suited for a crossfader.
// Width includes the ◄ and ► end caps; the cursor ┼ moves between them.
func crossfadeBar(knob, width int) string {
	inner := width - 2
	if inner < 1 {
		return "◄►"
	}
	pos := int(math.Round(float64(knob) / 127.0 * float64(inner-1)))
	return "◄" + strings.Repeat("─", pos) + "┼" + strings.Repeat("─", inner-1-pos) + "►"
}

// crossfaderLabel formats a short A→B label for the knob row when crossfader is active.
func crossfaderLabel(nameA, nameB string) string {
	const maxEach = 4
	a := truncate(nameA, maxEach)
	b := truncate(nameB, maxEach)
	if a == "" {
		a = "A"
	}
	if b == "" {
		b = "B"
	}
	return "⇄ " + a + "↔" + b
}

// capitalize uppercases the first rune of s.
func capitalize(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return s
	}
	return strings.ToUpper(string(runes[0])) + string(runes[1:])
}

// truncate clips s to max visible chars, appending "…" if clipped.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// wrapTwo splits s into two lines of at most width runes each, breaking at a
// word boundary when possible. The second line is truncated with "…" if needed.
func wrapTwo(s string, width int) (line1, line2 string) {
	runes := []rune(s)
	if len(runes) <= width {
		return s, ""
	}
	// Find the last space at or before position width.
	split := width
	for i := width - 1; i > 0; i-- {
		if runes[i] == ' ' {
			split = i
			break
		}
	}
	line1 = strings.TrimRight(string(runes[:split]), " ")
	rest := strings.TrimLeft(string(runes[split:]), " ")
	line2 = truncate(rest, width)
	return line1, line2
}
