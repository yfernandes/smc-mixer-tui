package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

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

// kindToType maps a NodeKind to the config type string used by deviceTypeTag.
func kindToType(k audio.NodeKind) string {
	switch k {
	case audio.KindMic:
		return "input"
	case audio.KindSink:
		return "output"
	default:
		return "playback"
	}
}

// deviceTypeTag returns a short, colored inline label for a split strip header.
func deviceTypeTag(typ string) string {
	switch typ {
	case "input":
		return subtypeInputStyle.Render("input")
	case "playback":
		return subtypePlaybackStyle.Render("source")
	case "output":
		return subtypeOutputStyle.Render("output")
	default:
		return subtypeDimStyle.Render("---")
	}
}

type stripColumns struct {
	left  lipgloss.Style
	right lipgloss.Style
}

func newStripColumns() stripColumns {
	return stripColumns{
		left:  lipgloss.NewStyle().Width(leftW),
		right: lipgloss.NewStyle().Width(rightW),
	}
}

func (c stripColumns) row(left, right string) string {
	return c.left.Render(left) + c.right.Render(right)
}

// renderSplitStrip renders the two-zone (knob | fader) layout used on the main page
// when a channel's knob and fader are bound to different config device keys.
func (m Model) renderSplitStrip(ch int) string {
	cfg := m.stripCfgs[ch]
	c := m.channels[ch]
	state := m.channelStateFor(ch)

	const innerW = leftW + rightW

	knobLabel := cfg.KnobLabel
	if knobLabel == "" {
		knobLabel = "---"
	}
	knobHeader := fmt.Sprintf("K%d · ", ch+1) + deviceTypeTag(cfg.KnobType)

	var knobValLine, knobBarLine string
	if c.CrossSinkAName != "" && c.CrossSinkBName != "" {
		knobValLine = truncate(crossfaderLabel(c.CrossSinkAName, c.CrossSinkBName), innerW)
		knobBarLine = crossfadeBar(c.Knob, innerW+1)
	} else {
		knobValLine = fmt.Sprintf("○%3d%%", knobPct(c.Knob)) + faderBar(float64(c.Knob)/127.0, innerW-5)
	}

	faderHeader, faderLabelStr := splitFaderHeader(ch, cfg, c, state)
	cols := newStripColumns()
	hwKnown := c.FaderPosKnown
	appBound := c.StreamID != nil
	// synced is only meaningful when a stream is bound; without an APP target there is nothing to sync to.
	fRows := dualFaderRows(c.FaderPos, c.ActualVolume, hwKnown, appBound, c.Synced && appBound, splitFaderH, leftW)
	focusedNav := ch == m.selected && !m.bindMode

	lines := []string{
		knobHeader,
		truncate(knobLabel, innerW),
		knobValLine,
	}
	if knobBarLine != "" {
		lines = append(lines, knobBarLine)
	}
	lines = append(
		lines,
		splitDividerStyle.Width(innerW).Render(""),
		faderHeader,
		faderLabelStr,
		"",
		cols.row(fRows[0], renderBtnFocused("M", c.Mute || c.SoloMuted, btnMuteOn, focusedNav && m.navSetting == navMute)),
		cols.row(fRows[1], renderBtnFocused("S", c.Solo, btnSoloOn, focusedNav && m.navSetting == navSolo)),
		cols.row(fRows[2], renderBtn("R", c.Rec || m.ChannelAdvanced[ch], btnRecOn)),
		cols.row(fRows[3], renderBtn("■", c.Stop, btnStopOn)),
		cols.row(fRows[4], pickupLabel(c)),
	)

	var kind audio.NodeKind
	if es := m.enrichedFor(ch); es != nil {
		kind = es.Kind
	}
	return selectStripStyle(ch == m.selected, state, kind).Render(strings.Join(lines, "\n"))
}

func splitFaderHeader(ch int, cfg StripConfig, c dispatcher.Channel, state channelState) (header, label string) {
	const innerW = leftW + rightW

	if cfg.FaderType != "" {
		label = cfg.FaderLabel
		if label == "" {
			label = "---"
		}
		return fmt.Sprintf("F%d · ", ch+1) + deviceTypeTag(cfg.FaderType), truncate(label, innerW)
	}
	if c.StreamID == nil {
		return fmt.Sprintf("F%d · ", ch+1) + deviceTypeTag(""), subtypeDimStyle.Render("---")
	}
	if state == stateInactive {
		return fmt.Sprintf("F%d · ", ch+1) + deviceTypeTag(kindToType(c.Kind)), subtypeDimStyle.Render("⊗ offline")
	}
	return fmt.Sprintf("F%d · ", ch+1) + deviceTypeTag(kindToType(c.Kind)), truncate(c.Name, innerW)
}

func (m Model) renderStrip(ch int) string {
	if m.ActivePage == "main" && m.stripCfgs[ch].IsSplit {
		return m.renderSplitStrip(ch)
	}

	c := m.channels[ch]
	state := m.channelStateFor(ch)
	es := m.enrichedFor(ch)
	cols := newStripColumns()
	focusedNav := ch == m.selected && !m.bindMode

	knobLine, knobBar := knobRows(c)
	header, nameLine, subLine := m.stripHeader(ch, c, es, state, focusedNav)
	hwKnown := c.FaderPosKnown
	appBound := c.StreamID != nil
	// synced is only meaningful when a stream is bound; without an APP target there is nothing to sync to.
	fRows := dualFaderRows(c.FaderPos, c.ActualVolume, hwKnown, appBound, c.Synced && appBound, unifiedFaderH, leftW)

	lines := []string{
		header,
		nameLine,
		subLine,
		knobLine,
		knobBar,
		cols.row("", ""),
		cols.row(fRows[0], ""),
		cols.row(fRows[1], renderBtnFocused("M", c.Mute || c.SoloMuted, btnMuteOn, focusedNav && m.navSetting == navMute)),
		cols.row(fRows[2], renderBtnFocused("S", c.Solo, btnSoloOn, focusedNav && m.navSetting == navSolo)),
		cols.row(fRows[3], renderBtn("R", c.Rec || m.ChannelAdvanced[ch], btnRecOn)),
		cols.row(fRows[4], renderBtn("■", c.Stop, btnStopOn)),
		cols.row(fRows[5], ""),
		cols.row(fRows[6], pickupLabel(c)),
	}

	var kind audio.NodeKind
	if es != nil {
		kind = es.Kind
	}
	return selectStripStyle(ch == m.selected, state, kind).Render(strings.Join(lines, "\n"))
}

func knobRows(c dispatcher.Channel) (line, bar string) {
	if c.CrossSinkAName != "" && c.CrossSinkBName != "" {
		return truncate(crossfaderLabel(c.CrossSinkAName, c.CrossSinkBName), leftW+rightW),
			crossfadeBar(c.Knob, knobBarW+1)
	}
	return fmt.Sprintf("◎%3d%%", knobPct(c.Knob)), faderBar(float64(c.Knob)/127.0, knobBarW+1)
}

func (m Model) stripHeader(ch int, c dispatcher.Channel, es *streams.EnrichedStream, state channelState, focusedNav bool) (header, nameLine, subLine string) {
	subtitle := subtitleLabel(es, state)

	hasMPRISTrack := es != nil && es.Source == streams.SourceMPRIS && (es.Artist != "" || es.Track != "")
	useEnriched := es != nil && es.AppName != "" && (es.AppName != es.Name || hasMPRISTrack)
	if !useEnriched {
		header = fmt.Sprintf("CH%-2d", ch+1)
		name := nameLabel(c, state, m.labels[ch])
		if focusedNav && m.navSetting == navStream {
			name = "⇌ " + name
		}
		return header, truncate(name, leftW+rightW), truncate(subtitle, leftW+rightW)
	}

	header = truncate(fmt.Sprintf("CH%d - %s", ch+1, capitalize(es.AppName)), leftW+rightW)
	if hasMPRISTrack {
		if es.Artist != "" {
			nameLine = truncate(es.Artist, leftW+rightW)
			subLine = truncate(es.Track, leftW+rightW)
		} else {
			nameLine = truncate(es.Track, leftW+rightW)
		}
		if focusedNav && m.navSetting == navStream {
			nameLine = truncate("⇌ "+nameLine, leftW+rightW)
		}
		return header, nameLine, subLine
	}

	content := subtitle
	if content == "" {
		content = es.Name
	}
	if focusedNav && m.navSetting == navStream {
		content = "⇌ " + content
	}
	nameLine, subLine = wrapTwo(content, leftW+rightW)
	return header, nameLine, subLine
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
