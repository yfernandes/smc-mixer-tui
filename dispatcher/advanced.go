package dispatcher

import "github.com/yfernandes/smc-mixer-tui/midi"

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
	rec := d.channels[ch].Rec
	leds := d.leds
	d.mu.Unlock()

	if leds != nil {
		leds.SetButtonLED(ch, midi.ButtonRec, pinned || rec)
	}
}
