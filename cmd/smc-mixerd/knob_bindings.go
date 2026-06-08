package main

import (
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

// applyKnobBindings binds or clears the independent knob device for each channel.
// Only the main page has independent knob slots; all other pages clear knob bindings.
func applyKnobBindings(cfg *config.Config, disp *dispatcher.Dispatcher, activePage string, ss []streams.EnrichedStream) {
	for ch := range 8 {
		if s := knobBindingCandidate(cfg, activePage, ch, ss); s != nil {
			disp.BindKnob(ch, s.ID)
		} else {
			disp.LoseKnob(ch)
		}
	}
}

func knobBindingCandidate(cfg *config.Config, activePage string, ch int, ss []streams.EnrichedStream) *streams.EnrichedStream {
	if activePage != "main" {
		return nil
	}
	knob, ok := cfg.KnobFor(ch)
	if !ok {
		return nil
	}
	dev := cfg.KnobDeviceFor(ch)
	if dev == nil {
		return nil
	}
	// Output devices placed in a knob slot default to KnobNone but the user's
	// intent is volume control. KnobSend (crossfade) is handled separately.
	isGain := knob.Type == config.KnobGain || (dev.IsOutput() && knob.Type == config.KnobNone)
	if !isGain {
		return nil
	}
	return bindingCandidate(newStreamMatcher(dev), ss)
}
