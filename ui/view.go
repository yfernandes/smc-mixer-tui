package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yago/smc-mixer/dispatcher"
	"github.com/yago/smc-mixer/streams"
)

func (m Model) View() string {
	strips := make([]string, 8)
	for i := range 8 {
		strips[i] = m.renderStrip(i)
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, strips...)
	if m.bindMode {
		return lipgloss.JoinVertical(lipgloss.Left, row, m.renderBindPanel())
	}
	return lipgloss.JoinVertical(lipgloss.Left, row, m.renderBar())
}

// channelState classifies a strip for styling purposes.
type channelState int

const (
	stateUnbound  channelState = iota
	stateActive                // bound, stream present in current list
	stateInactive              // bound, stream has disappeared
	stateBinding               // bind-mode is open on this strip
)

func (m Model) channelStateFor(ch int) channelState {
	c := m.channels[ch]
	if m.bindMode && ch == m.selected {
		return stateBinding
	}
	if c.StreamID == nil {
		return stateUnbound
	}
	for _, s := range m.enriched {
		if s.ID == *c.StreamID {
			return stateActive
		}
	}
	return stateInactive
}

// enrichedFor returns the EnrichedStream currently bound to channel ch, if any.
func (m Model) enrichedFor(ch int) *streams.EnrichedStream {
	c := m.channels[ch]
	if c.StreamID == nil {
		return nil
	}
	for i := range m.enriched {
		if m.enriched[i].ID == *c.StreamID {
			return &m.enriched[i]
		}
	}
	return nil
}

func (m Model) renderStrip(ch int) string {
	c := m.channels[ch]
	state := m.channelStateFor(ch)
	es := m.enrichedFor(ch)

	left := lipgloss.NewStyle().Width(leftW)
	right := lipgloss.NewStyle().Width(rightW)
	row := func(l, r string) string { return left.Render(l) + right.Render(r) }

	name := nameLabel(c, state)
	subtitle := subtitleLabel(es, state)
	fRows := faderRows(c.ActualVolume, faderH, leftW)
	volPct := pickupLabel(c)

	lines := []string{
		fmt.Sprintf("CH%-2d", ch+1),
		truncate(name, leftW+rightW),
		truncate(subtitle, leftW+rightW),
		fmt.Sprintf("◎%4d", c.Knob),
		faderBar(float64(c.Knob)/127.0, knobBarW+1),
		row("", ""),
		row(fRows[0], renderBtn("M", c.Mute || c.SoloMuted, btnMuteOn)),
		row(fRows[1], renderBtn("S", c.Solo, btnSoloOn)),
		row(fRows[2], renderBtn("R", c.Rec, btnRecOn)),
		row(fRows[3], renderBtn("■", c.Stop, btnStopOn)),
		row(fRows[4], volPct),
	}

	var kind streams.NodeKind
	if es != nil {
		kind = es.Kind
	}
	return selectStripStyle(ch == m.selected, state, kind).Render(strings.Join(lines, "\n"))
}

// bindSubtitle returns the most useful secondary description for a stream in
// the bind picker. Returns "" when nothing useful is available.
func bindSubtitle(es streams.EnrichedStream) string {
	// media.name is per-stream (set by the audio context of each tab/source),
	// so it correctly distinguishes multiple streams from the same process.
	// Always prefer it over MPRIS, which is per-process and can bleed across tabs.
	if es.MediaName != "" && es.MediaName != es.Name &&
		es.MediaName != "playback" && es.MediaName != "Playback" {
		return es.MediaName
	}
	// MPRIS fallback: only useful when there is no per-stream media.name.
	if es.Source == streams.SourceMPRIS {
		if es.Artist != "" && es.Track != "" {
			return es.Artist + " – " + es.Track
		}
		return es.Track
	}
	// Hyprland window title as last resort
	if es.WinTitle != "" && es.WinTitle != es.Name {
		return es.WinTitle
	}
	return ""
}

// nameLabel returns the primary name for a strip given its state.
func nameLabel(c dispatcher.Channel, state channelState) string {
	switch state {
	case stateBinding, stateUnbound:
		return "---"
	default:
		return c.Name
	}
}

// subtitleLabel returns the secondary line for a strip.
func subtitleLabel(es *streams.EnrichedStream, state channelState) string {
	if state == stateInactive {
		return "⊗ offline"
	}
	if es == nil || state != stateActive {
		return ""
	}
	// Prefer per-stream media.name; MPRIS artist/track is per-process and bleeds
	// across all streams from the same PID when a browser has multiple audio tabs.
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

// renderBindPanel renders the full-width stream picker shown below the strips
// when the user is in bind mode. It lists all enriched streams with the cursor
// highlighted, scrolling so the cursor is always visible.
func (m Model) renderBindPanel() string {
	w := m.termW
	if w < 40 {
		w = 120 // sensible default before first WindowSizeMsg
	}

	header := bindBarStyle.Render(fmt.Sprintf(
		" Bind CH%d   ↑↓ navigate   enter confirm   esc cancel",
		m.selected+1,
	))

	if len(m.enriched) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, header,
			bindBarStyle.Render(" (no streams)"))
	}

	end := m.bindScroll + bindVisible
	if end > len(m.enriched) {
		end = len(m.enriched)
	}

	rows := make([]string, 0, end-m.bindScroll+2)
	if m.bindScroll > 0 {
		rows = append(rows, bindDimStyle.Render(fmt.Sprintf("  ↑ %d more", m.bindScroll)))
	}
	for i := m.bindScroll; i < end; i++ {
		es := m.enriched[i]
		// Insert a section header at the start of each kind group.
		if i == 0 || m.enriched[i].Kind != m.enriched[i-1].Kind {
			rows = append(rows, kindHeader(es.Kind, w))
		}
		tag, tagStyle := kindTag(es.Kind)
		label := " " + es.Name
		if sub := bindSubtitle(es); sub != "" {
			label += "  ·  " + sub
		}
		if i == m.bindCursor {
			rows = append(rows, tagStyle.Render(tag)+bindCursorStyle.Width(w-2-len(tag)).Render("▶"+label))
		} else {
			rows = append(rows, tagStyle.Render(tag)+bindItemStyle.Width(w-2-len(tag)).Render(" "+label))
		}
	}
	below := len(m.enriched) - end
	if below > 0 {
		rows = append(rows, bindDimStyle.Render(fmt.Sprintf("  ↓ %d more", below)))
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func (m Model) renderBar() string {
	if !m.deviceConnected {
		return styleNoDevice.Render(" ⚠ no MIDI device — waiting for SMC…   q quit")
	}

	playInd := " ▶"
	if m.playing {
		playInd = stylePlay.Render(" ▶")
	}
	recInd := " ⏺"
	if m.recording {
		recInd = styleRec.Render(" ⏺")
	}
	return globalBarStyle.Render(fmt.Sprintf(
		"%s%s   ⏮ ⏭   ←→ select   enter bind   u unbind   q quit",
		playInd, recInd,
	))
}


func selectStripStyle(selected bool, state channelState, kind streams.NodeKind) lipgloss.Style {
	switch {
	case state == stateBinding:
		return stripBindMode
	case state == stateUnbound || state == stateInactive:
		if selected {
			return stripSelectedUnbound
		}
		return stripUnbound
	case selected:
		switch kind {
		case streams.KindMic:
			return stripMicSelected
		case streams.KindSink:
			return stripSinkSelected
		default:
			return stripSrcSelected
		}
	default:
		switch kind {
		case streams.KindMic:
			return stripMic
		case streams.KindSink:
			return stripSinkBound
		default:
			return stripSrcBound
		}
	}
}
