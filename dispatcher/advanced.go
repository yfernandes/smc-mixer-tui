package dispatcher

import (
	"context"
	"time"

	"github.com/yfernandes/smc-mixer-tui/midi"
)

// AdvancedSpec holds the effect and action names for a channel's advanced mode.
// Set by the daemon when a device with an [advanced] block is bound.
type AdvancedSpec struct {
	FaderEffect      string
	KnobEffect       string
	MuteButtonAction string
	SoloButtonAction string
	StopButtonAction string
}

// SetAdvancedSpec sets (or clears, if nil) the advanced mode spec for channel ch.
// Called by the daemon after binding a channel to a device.
func (d *Dispatcher) SetAdvancedSpec(ch int, spec *AdvancedSpec) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.channels[ch].advancedSpec = spec
}

// SetPinCallback sets the callback invoked on a long press of the R button.
// The callback receives the channel index and runs outside the dispatcher lock.
func (d *Dispatcher) SetPinCallback(cb func(ch int)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pinCallback = cb
}

// SetPinned sets the pinned state for channel ch and updates the R LED accordingly.
// Advanced mode blink takes visual priority; the LED is not updated while advanced is active.
func (d *Dispatcher) SetPinned(ch int, pinned bool) {
	d.mu.Lock()
	d.channels[ch].Pinned = pinned
	advanced := d.channels[ch].Advanced
	rec := d.channels[ch].Rec
	leds := d.leds
	d.mu.Unlock()

	if leds != nil && !advanced {
		leds.SetButtonLED(ch, midi.ButtonRec, pinned || rec)
	}
}

// runAdvancedBlink blinks the R button LED for channel ch until ctx is cancelled.
// gen must match the blinkGen value captured at activation; if it diverges the
// goroutine exits silently, preventing a stale goroutine from clobbering LED state
// after a new blink cycle has already started for the same channel.
func (d *Dispatcher) runAdvancedBlink(ctx context.Context, ch int, gen uint32) {
	for {
		d.mu.RLock()
		leds := d.leds
		stale := d.blinkGen[ch] != gen
		d.mu.RUnlock()
		if stale {
			return
		}
		if leds != nil {
			leds.SetButtonLED(ch, midi.ButtonRec, true)
		}
		select {
		case <-ctx.Done():
			d.mu.RLock()
			stale = d.blinkGen[ch] != gen
			d.mu.RUnlock()
			if !stale {
				d.restoreRLED(ch)
			}
			return
		case <-time.After(400 * time.Millisecond):
		}

		d.mu.RLock()
		leds = d.leds
		stale = d.blinkGen[ch] != gen
		d.mu.RUnlock()
		if stale {
			return
		}
		if leds != nil {
			leds.SetButtonLED(ch, midi.ButtonRec, false)
		}
		select {
		case <-ctx.Done():
			d.mu.RLock()
			stale = d.blinkGen[ch] != gen
			d.mu.RUnlock()
			if !stale {
				d.restoreRLED(ch)
			}
			return
		case <-time.After(400 * time.Millisecond):
		}
	}
}

// restoreRLED sets the R LED for ch to its correct non-advanced-mode state (Rec || Pinned).
func (d *Dispatcher) restoreRLED(ch int) {
	d.mu.RLock()
	leds := d.leds
	desired := d.channels[ch].Rec || d.channels[ch].Pinned
	d.mu.RUnlock()
	if leds != nil {
		leds.SetButtonLED(ch, midi.ButtonRec, desired)
	}
}
