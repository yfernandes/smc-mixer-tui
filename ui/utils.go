package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

// pickupLabel returns the 4-char volume/sync string for the bottom of a strip.
// Unbound or synced channels show " 60%". Awaiting-pickup channels render the
// percentage in orange — the dual fader bars already convey direction.
func pickupLabel(c dispatcher.Channel) string {
	pct := fmt.Sprintf("%3.0f%%", c.ActualVolume*100)
	if c.StreamID != nil && !c.Synced {
		return pickupArrowStyle.Render(pct)
	}
	return pct
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

// knobPct converts a raw MIDI knob value (0–127) to a 0–100 percentage.
func knobPct(knob int) int {
	return int(math.Round(float64(knob) / 127.0 * 100))
}

// faderBar returns a horizontal bar of width chars representing val (0.0–1.0).
func faderBar(val float64, width int) string {
	filled := max(0, min(width, int(math.Round(val*float64(width)))))
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// dualFaderRows returns height strings forming two vertical bars side by side (zero at bottom).
//
// Left bar  (▓▓ red)   = HW: raw hardware fader position from MIDI CC.
//
//	Always rendered when hwKnown; shown as '─ ' dim until the first CC arrives.
//	Independent of channel binding — reflects the physical fader at all times.
//
// Right bar (░░ green) = APP: PipeWire-reported volume / pickup target.
//
//	Rendered only when appBound (a stream is assigned to this channel); blank otherwise.
//	These two values are intentionally independent; see dispatcher.Channel for why.
//
// When synced both bars render white to signal agreement.
// '▓' vs '░' keeps bars distinct on color-free terminals; color is the primary cue.
func dualFaderRows(hw, app float64, hwKnown, appBound, synced bool, height, width int) []string {
	const (
		hwChar        = "▓▓" // dense block — hardware position (2 chars wide)
		appChar       = "░░" // light block — application target (2 chars wide)
		barW          = 2    // each bar is 2 chars wide
		tickChar      = " ▔ " // U+2594 upper-eighth block, 3 chars total
		finalTickChar = " 🮀 " // U+1FB80 upper+lower eighth block, 3 chars total
		tickW         = 3    // visual width of tickChar / finalTickChar
	)
	rightPad := strings.Repeat(" ", max(0, width-tickW-barW*2))

	hwFilled := max(0, min(height, int(math.Round(hw*float64(height)))))
	appFilled := max(0, min(height, int(math.Round(app*float64(height)))))

	rows := make([]string, height)
	for i := range height {
		fromBottom := height - 1 - i

		var hwS string
		if !hwKnown {
			if fromBottom == 0 {
				hwS = hwUnknownStyle.Render(hwChar)
			} else {
				hwS = "  "
			}
		} else if fromBottom < hwFilled {
			if synced {
				hwS = syncFaderStyle.Render(hwChar)
			} else {
				hwS = hwFaderStyle.Render(hwChar)
			}
		} else {
			hwS = "  "
		}

		var appS string
		if appBound && fromBottom < appFilled {
			if synced {
				appS = syncFaderStyle.Render(appChar)
			} else {
				appS = appFaderStyle.Render(appChar)
			}
		} else {
			appS = "  "
		}

		tick := tickChar
		if fromBottom == 0 {
			tick = finalTickChar
		}
		rows[i] = tick + hwS + appS + rightPad
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
