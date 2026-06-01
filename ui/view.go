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

	name := nameLabel(c, state, m.enriched, m.bindCursor)
	subtitle := subtitleLabel(es, state)
	fRows := faderRows(c.Volume, faderH, leftW)

	lines := []string{
		fmt.Sprintf("CH%-2d", ch+1),
		truncate(name, leftW+rightW),
		truncate(subtitle, leftW+rightW),
		fmt.Sprintf("◎%4d", c.Knob),
		faderBar(float64(c.Knob)/127.0, knobBarW+1),
		row("", ""),
		row(fRows[0], renderBtn("M", c.Mute, btnMuteOn)),
		row(fRows[1], renderBtn("S", c.Solo, btnSoloOn)),
		row(fRows[2], renderBtn("R", c.Rec, btnRecOn)),
		row(fRows[3], renderBtn("■", c.Stop, btnStopOn)),
		row(fRows[4], fmt.Sprintf("%3.0f%%", c.Volume*100)),
	}

	return selectStripStyle(ch == m.selected, state).Render(strings.Join(lines, "\n"))
}

// nameLabel returns the primary name for a strip given its state.
func nameLabel(c dispatcher.Channel, state channelState, enriched []streams.EnrichedStream, cursor int) string {
	switch state {
	case stateBinding:
		if len(enriched) > 0 {
			return enriched[cursor].Name
		}
		return "no streams"
	case stateUnbound:
		return "---"
	default:
		return c.Name
	}
}

// subtitleLabel returns the secondary line for a strip.
// Active MPRIS streams show track/artist; inactive streams show a tombstone.
func subtitleLabel(es *streams.EnrichedStream, state channelState) string {
	if state == stateInactive {
		return "⊗ offline"
	}
	if es == nil || state != stateActive || es.Source != streams.SourceMPRIS {
		return ""
	}
	if es.Artist != "" && es.Track != "" {
		return es.Artist + " – " + es.Track
	}
	return es.Track
}

func (m Model) renderBar() string {
	if m.bindMode {
		name := "no streams"
		if len(m.enriched) > 0 {
			name = m.enriched[m.bindCursor].Name
		}
		return bindBarStyle.Render(fmt.Sprintf(
			" Binding CH%d → %q   ↑↓ cycle   enter confirm   esc cancel",
			m.selected+1, name,
		))
	}

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


func selectStripStyle(selected bool, state channelState) lipgloss.Style {
	switch {
	case state == stateBinding:
		return stripBindMode
	case selected && (state == stateUnbound || state == stateInactive):
		return stripSelectedUnbound
	case selected:
		return stripSelected
	case state == stateUnbound || state == stateInactive:
		return stripUnbound
	default:
		return stripNormal
	}
}
