package main

import (
	"context"
	"math"

	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

// knobVolumeGetter reads the current PipeWire volume for a node. May be nil to skip seeding.
type knobVolumeGetter func(ctx context.Context, id uint32) (float64, bool, error)

// applyKnobBindings binds or clears the independent knob device for each channel.
// Only the main page has independent knob slots; all other pages clear knob bindings.
// When getVol is non-nil and a knob binding is new or changed, the knob position is
// seeded from the actual PipeWire volume so the display matches reality at startup.
func applyKnobBindings(ctx context.Context, cfg *config.Config, disp *dispatcher.Dispatcher, activePage string, ss []streams.EnrichedStream, getVol knobVolumeGetter) {
	for ch := range 8 {
		if s := knobBindingCandidate(cfg, activePage, ch, ss); s != nil {
			if disp.BindKnob(ch, s.ID) && getVol != nil {
				if vol, _, err := getVol(ctx, s.ID); err == nil {
					disp.SetKnob(ch, int(math.Round(vol*127)))
				}
			}
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
