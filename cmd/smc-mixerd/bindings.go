package main

import (
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

func configLabels(cfg *config.Config) [8]string {
	var labels [8]string
	for ch := range 8 {
		if dev := cfg.ChannelFor(ch); dev != nil {
			labels[ch] = dev.Label
		}
	}
	return labels
}
