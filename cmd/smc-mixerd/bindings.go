package main

import (
	"regexp"
	"strings"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

func applyBindings(cfg *config.Config, disp *dispatcher.Dispatcher, ss []streams.EnrichedStream, pinnedKeys map[int]string) {
	clearStaleBindings(disp, ss)
	activePage := disp.ActivePage()
	// Sync pinned flags before planning so planBindings can skip already-live pinned slots.
	syncPinnedFlags(cfg, disp, activePage, pinnedKeys)
	for _, action := range planBindings(cfg, activePage, disp.Snapshot(), ss) {
		switch {
		case action.lose:
			disp.LoseBinding(action.ch)
		case action.syncSpec:
			// Stream already matched; only refresh config-derived metadata.
			dev := cfg.ChannelForPage(activePage, action.ch)
			disp.SetAdvancedSpec(action.ch, advancedSpecFrom(dev))
		default:
			disp.Bind(action.ch, action.id, action.name, action.kind, action.mprisName)
			dev := cfg.ChannelForPage(activePage, action.ch)
			disp.SetAdvancedSpec(action.ch, advancedSpecFrom(dev))
		}
	}
	applyKnobBindings(cfg, disp, activePage, ss)
	refreshBindingMetadata(disp, ss)
}

// syncPinnedFlags updates Channel.Pinned for all channels based on current page and pinnedKeys.
// On main page: a slot is pinned if it appears in pinnedKeys.
// On other pages: a slot is pinned if its device key matches the pinned key for that slot.
func syncPinnedFlags(cfg *config.Config, disp *dispatcher.Dispatcher, activePage string, pinnedKeys map[int]string) {
	for ch := range 8 {
		pinnedKey, hasPinned := pinnedKeys[ch]
		var isPinned bool
		if hasPinned {
			if activePage == "main" {
				isPinned = true
			} else {
				isPinned = cfg.DeviceKeyForPage(activePage, ch) == pinnedKey
			}
		}
		disp.SetPinned(ch, isPinned)
	}
}

func advancedSpecFrom(dev *config.DeviceConfig) *dispatcher.AdvancedSpec {
	if dev == nil || dev.Advanced == nil {
		return nil
	}
	spec := &dispatcher.AdvancedSpec{}
	if dev.Advanced.Fader != nil {
		spec.FaderEffect = dev.Advanced.Fader.Effect
	}
	if dev.Advanced.Knob != nil {
		spec.KnobEffect = dev.Advanced.Knob.Effect
	}
	if dev.Advanced.MuteButton != nil {
		spec.MuteButtonAction = dev.Advanced.MuteButton.Action
	}
	if dev.Advanced.SoloButton != nil {
		spec.SoloButtonAction = dev.Advanced.SoloButton.Action
	}
	if dev.Advanced.StopButton != nil {
		spec.StopButtonAction = dev.Advanced.StopButton.Action
	}
	return spec
}

func clearStaleBindings(disp *dispatcher.Dispatcher, ss []streams.EnrichedStream) {
	snap := disp.Snapshot()
	for ch, c := range snap {
		if c.StreamID != nil && !c.ManuallyUnbound && !streamLive(*c.StreamID, ss) {
			disp.LoseBinding(ch)
		}
	}
}

type bindingAction struct {
	ch        int
	id        uint32
	name      string
	kind      audio.NodeKind
	mprisName string
	lose      bool // if true: lose binding only, all other fields ignored
	syncSpec  bool // if true: sync advancedSpec for an unchanged live binding
}

func planBindings(cfg *config.Config, activePage string, snap [8]dispatcher.Channel, ss []streams.EnrichedStream) []bindingAction {
	var actions []bindingAction
	for ch := range 8 {
		// Pinned main-page slots with a live binding are immune to reassignment.
		if activePage == "main" && snap[ch].Pinned && channelBindingLive(snap[ch], ss) {
			continue
		}
		if snap[ch].ManuallyUnbound {
			continue
		}

		dev := cfg.ChannelForPage(activePage, ch)
		if dev == nil {
			// No device configured for this slot on the current page: lose any live binding
			// so it doesn't block other logic or remain stale across page switches.
			if channelBindingLive(snap[ch], ss) {
				actions = append(actions, bindingAction{ch: ch, lose: true})
			}
			continue
		}

		// Live binding already matches the configured device — no rebind, but refresh
		// config-derived state (e.g. advancedSpec) in case the page switch changed the
		// device behind the same stream.
		if channelBindingMatchesConfig(snap[ch], dev, ss) {
			actions = append(actions, bindingAction{ch: ch, syncSpec: true})
			continue
		}

		matcher := newStreamMatcher(dev)
		if s := bindingCandidate(matcher, ss); s != nil {
			actions = append(actions, bindingAction{
				ch:        ch,
				id:        s.ID,
				name:      s.Name,
				kind:      s.Kind,
				mprisName: mprisName(*s),
			})
		} else if channelBindingLive(snap[ch], ss) {
			// Configured device has no matching stream, but a stale (non-matching) binding
			// is live. Lose it so the slot is ready to rebind when the stream appears.
			actions = append(actions, bindingAction{ch: ch, lose: true})
		}
	}
	return actions
}

// channelBindingMatchesConfig reports whether ch's live binding is for a stream
// that matches the device config. Used to avoid redundant rebinds on page switch.
func channelBindingMatchesConfig(ch dispatcher.Channel, dev *config.DeviceConfig, ss []streams.EnrichedStream) bool {
	if ch.StreamID == nil {
		return false
	}
	for _, s := range ss {
		if s.ID == *ch.StreamID {
			return newStreamMatcher(dev).matches(s)
		}
	}
	return false
}

func channelBindingLive(ch dispatcher.Channel, ss []streams.EnrichedStream) bool {
	return ch.StreamID != nil && streamLive(*ch.StreamID, ss)
}

func bindingCandidate(matcher streamMatcher, ss []streams.EnrichedStream) *streams.EnrichedStream {
	for i := range ss {
		if matcher.matches(ss[i]) {
			return &ss[i]
		}
	}
	return nil
}

func mprisName(s streams.EnrichedStream) string {
	return s.MPRISPlayer
}

type streamMatcher struct {
	wantKind    audio.NodeKind
	hasWantKind bool
	strategies  []func(streams.EnrichedStream) bool
}

// newStreamMatcher builds a matcher for a device.
// Strategies are mutually exclusive and applied in priority order: regex > match-title > substring.
func newStreamMatcher(dev *config.DeviceConfig) streamMatcher {
	m := streamMatcher{}
	if wantKind, ok := dev.AudioKind(); ok {
		m.wantKind = wantKind
		m.hasWantKind = true
	}
	switch {
	case dev.MatchRegex != "":
		if re, err := regexp.Compile("(?i)" + dev.MatchRegex); err == nil {
			m.strategies = append(m.strategies, func(s streams.EnrichedStream) bool {
				return re.MatchString(s.Name) || re.MatchString(s.BindKey)
			})
		} else {
			m.strategies = append(m.strategies, func(streams.EnrichedStream) bool { return false })
		}
	case dev.MatchTitle != "":
		title := strings.ToLower(dev.MatchTitle)
		m.strategies = append(m.strategies, func(s streams.EnrichedStream) bool {
			return strings.Contains(strings.ToLower(s.WinTitle), title)
		})
	default:
		if dev.Match != "" {
			lower := strings.ToLower(dev.Match)
			m.strategies = append(m.strategies, func(s streams.EnrichedStream) bool {
				return strings.Contains(strings.ToLower(s.Name), lower) ||
					strings.Contains(strings.ToLower(s.BindKey), lower)
			})
		}
	}
	return m
}

func (m streamMatcher) matches(s streams.EnrichedStream) bool {
	if m.hasWantKind && s.Kind != m.wantKind {
		return false
	}
	for _, fn := range m.strategies {
		if fn(s) {
			return true
		}
	}
	return false
}

// applyKnobBindings binds or clears the independent knob device for each channel.
// Only the main page has independent knob slots; all other pages clear knob bindings.
func applyKnobBindings(cfg *config.Config, disp *dispatcher.Dispatcher, activePage string, ss []streams.EnrichedStream) {
	for ch := range 8 {
		if activePage != "main" {
			disp.LoseKnob(ch)
			continue
		}
		knob, ok := cfg.KnobFor(ch)
		if !ok || knob.Type != config.KnobGain {
			disp.LoseKnob(ch)
			continue
		}
		dev := cfg.KnobDeviceFor(ch)
		if dev == nil {
			disp.LoseKnob(ch)
			continue
		}
		matcher := newStreamMatcher(dev)
		if s := bindingCandidate(matcher, ss); s != nil {
			disp.BindKnob(ch, s.ID)
		} else {
			disp.LoseKnob(ch)
		}
	}
}

func configLabels(cfg *config.Config) [8]string {
	var labels [8]string
	for ch := range 8 {
		if dev := cfg.ChannelFor(ch); dev != nil {
			labels[ch] = dev.Label
		}
	}
	return labels
}

func streamLive(id uint32, ss []streams.EnrichedStream) bool {
	for _, s := range ss {
		if s.ID == id {
			return true
		}
	}
	return false
}

func refreshBindingMetadata(disp *dispatcher.Dispatcher, ss []streams.EnrichedStream) {
	snap := disp.Snapshot()
	byID := make(map[uint32]streams.EnrichedStream, len(ss))
	for _, s := range ss {
		byID[s.ID] = s
	}
	for ch, c := range snap {
		if c.StreamID == nil {
			continue
		}
		s, ok := byID[*c.StreamID]
		if !ok {
			continue
		}
		disp.UpdateBindingMetadata(ch, s.ID, s.Name, mprisName(s))
	}
}
