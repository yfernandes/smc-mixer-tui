package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/streams"
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
	if m.navStreamOpen {
		return lipgloss.JoinVertical(lipgloss.Left, row, m.renderNavStreamPanel())
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

	// focusedNav is true when this is the selected strip and we're in MIDI nav mode.
	focusedNav := ch == m.selected && !m.bindMode

	name := nameLabel(c, state, m.labels[ch])
	// Prefix the name with a cycle indicator when stream nav is focused on this strip.
	if focusedNav && m.navSetting == navStream {
		name = truncate("⇌ "+name, leftW+rightW)
	} else {
		name = truncate(name, leftW+rightW)
	}

	subtitle := subtitleLabel(es, state)
	fRows := faderRows(c.ActualVolume, faderH, leftW)
	volPct := pickupLabel(c)

	var knobLine, knobBar string
	if c.CrossSinkAName != "" && c.CrossSinkBName != "" {
		knobLine = truncate(crossfaderLabel(c.CrossSinkAName, c.CrossSinkBName), leftW+rightW)
		knobBar = crossfadeBar(c.Knob, knobBarW+1)
	} else {
		knobLine = fmt.Sprintf("◎%4d", c.Knob)
		knobBar = faderBar(float64(c.Knob)/127.0, knobBarW+1)
	}

	lines := []string{
		fmt.Sprintf("CH%-2d", ch+1),
		name,
		truncate(subtitle, leftW+rightW),
		knobLine,
		knobBar,
		row("", ""),
		row(fRows[0], renderBtnFocused("M", c.Mute||c.SoloMuted, btnMuteOn, focusedNav && m.navSetting == navMute)),
		row(fRows[1], renderBtnFocused("S", c.Solo, btnSoloOn, focusedNav && m.navSetting == navSolo)),
		row(fRows[2], renderBtn("R", c.Rec||m.ChannelAdvanced[ch], btnRecOn)),
		row(fRows[3], renderBtn("■", c.Stop, btnStopOn)),
		row(fRows[4], volPct),
	}

	var kind audio.NodeKind
	if es != nil {
		kind = es.Kind
	}
	return selectStripStyle(ch == m.selected, state, kind).Render(strings.Join(lines, "\n"))
}

// bindSubtitle returns the most useful secondary description for a stream in
// the bind picker. Returns "" when nothing useful is available.
func bindSubtitle(es streams.EnrichedStream) string {
	if s := streamSubtitle(es); s != "" {
		return s
	}
	// Hyprland window title as last resort (bind panel only — not shown in strips).
	if es.WinTitle != "" && es.WinTitle != es.Name {
		return es.WinTitle
	}
	return ""
}

// nameLabel returns the primary name for a strip given its state.
func nameLabel(c dispatcher.Channel, state channelState, label string) string {
	switch state {
	case stateBinding:
		return "---"
	case stateUnbound:
		if label != "" {
			return label
		}
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
	return streamSubtitle(*es)
}

// renderStreamRows builds the item rows for a stream list panel. It renders avail[scroll:end]
// with kind section headers at group boundaries and the item at highlightIdx marked with ▶.
// w is the total terminal width available for each row.
func renderStreamRows(avail []streams.EnrichedStream, scroll, end, highlightIdx, w int) []string {
	rows := make([]string, 0, end-scroll+1)
	for i := scroll; i < end; i++ {
		es := avail[i]
		if i == 0 || avail[i].Kind != avail[i-1].Kind {
			rows = append(rows, kindHeader(es.Kind, w))
		}
		tag, tagStyle := kindTag(es.Kind)
		label := " " + es.Name
		if sub := bindSubtitle(es); sub != "" {
			label += "  ·  " + sub
		}
		if i == highlightIdx {
			rows = append(rows, tagStyle.Render(tag)+bindCursorStyle.Width(w-2-len(tag)).Render("▶"+label))
		} else {
			rows = append(rows, tagStyle.Render(tag)+bindItemStyle.Width(w-2-len(tag)).Render(" "+label))
		}
	}
	return rows
}

// renderBindPanel renders the full-width stream picker shown below the strips
// when the user is in bind mode. Only shows streams not user-bound to other channels.
func (m Model) renderBindPanel() string {
	w := m.termW
	if w < 40 {
		w = 120 // sensible default before first WindowSizeMsg
	}

	header := bindBarStyle.Render(fmt.Sprintf(
		" Bind CH%d   ↑↓ navigate   enter confirm   esc cancel",
		m.selected+1,
	))

	avail := m.availableStreams()
	if len(avail) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, header,
			bindBarStyle.Render(" (no streams)"))
	}

	cursor := m.bindCursor
	if cursor >= len(avail) {
		cursor = len(avail) - 1
	}
	end := min(m.bindScroll+bindVisible, len(avail))

	var rows []string
	if m.bindScroll > 0 {
		rows = append(rows, bindDimStyle.Render(fmt.Sprintf("  ↑ %d more", m.bindScroll)))
	}
	rows = append(rows, renderStreamRows(avail, m.bindScroll, end, cursor, w)...)
	if below := len(avail) - end; below > 0 {
		rows = append(rows, bindDimStyle.Render(fmt.Sprintf("  ↓ %d more", below)))
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, lipgloss.JoinVertical(lipgloss.Left, rows...))
}

// renderNavStreamPanel renders the stream list used while navigating with ◀▶ in navStream mode.
// The currently-bound stream is highlighted; cycling with ◀▶ binds immediately.
func (m Model) renderNavStreamPanel() string {
	w := m.termW
	if w < 40 {
		w = 120
	}

	header := bindBarStyle.Render(fmt.Sprintf(
		" CH%d stream   ◀▶ cycle   ↑↓ function   enter bind list   u unbind   q quit",
		m.selected+1,
	))

	avail := m.availableStreams()
	if len(avail) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, header,
			bindDimStyle.Render(" (no streams available)"))
	}

	// Find the currently-bound stream in the available list.
	boundIdx := -1
	if id := m.channels[m.selected].StreamID; id != nil {
		for i, s := range avail {
			if s.ID == *id {
				boundIdx = i
				break
			}
		}
	}

	// Scroll window centred on the bound stream.
	scroll := 0
	if boundIdx >= 0 {
		scroll = max(0, boundIdx-bindVisible/2)
		if scroll+bindVisible > len(avail) {
			scroll = max(0, len(avail)-bindVisible)
		}
	}
	end := min(scroll+bindVisible, len(avail))

	var rows []string
	if scroll > 0 {
		rows = append(rows, bindDimStyle.Render(fmt.Sprintf("  ↑ %d more", scroll)))
	}
	rows = append(rows, renderStreamRows(avail, scroll, end, boundIdx, w)...)
	if below := len(avail) - end; below > 0 {
		rows = append(rows, bindDimStyle.Render(fmt.Sprintf("  ↓ %d more", below)))
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func (m Model) renderBar() string {
	if !m.deviceConnected {
		return styleNoDevice.Render(" ⚠ no MIDI device — waiting for SMC…   q quit")
	}
	names := [navSettingCount]string{"stream", "mute", "solo"}
	dim := globalBarStyle
	var settingParts [navSettingCount]string
	for i := 0; i < int(navSettingCount); i++ {
		if navSetting(i) == m.navSetting {
			settingParts[i] = bindBarStyle.Render(names[i])
		} else {
			settingParts[i] = dim.Render(names[i])
		}
	}
	return dim.Render(fmt.Sprintf(" page: %-12s   ←→ channel   ↑↓ ", m.ActivePage)) +
		settingParts[0] + dim.Render("/") + settingParts[1] + dim.Render("/") + settingParts[2] +
		dim.Render("   enter bind   u unbind   q quit")
}

func selectStripStyle(selected bool, state channelState, kind audio.NodeKind) lipgloss.Style {
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
		case audio.KindMic:
			return stripMicSelected
		case audio.KindSink:
			return stripSinkSelected
		default:
			return stripSrcSelected
		}
	default:
		switch kind {
		case audio.KindMic:
			return stripMic
		case audio.KindSink:
			return stripSinkBound
		default:
			return stripSrcBound
		}
	}
}
