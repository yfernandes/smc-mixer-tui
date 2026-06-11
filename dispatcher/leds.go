package dispatcher

import (
	"context"

	"github.com/yfernandes/smc-mixer-tui/midi"
)

// SetLEDWriter sets (or clears, if nil) the LED output device.
// When cleared (w == nil), FaderPosKnown is reset on all channels: the hardware
// position may change while disconnected and cannot be known until the next CC.
func (d *Dispatcher) SetLEDWriter(w LEDWriter) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.leds = w
	if w == nil {
		for i := range d.channels {
			d.channels[i].FaderPosKnown = false
		}
	}
}

// SyncLEDs pushes the full current LED state to the device. Call after connecting
// a new writer so the hardware reflects the in-memory state immediately.
func (d *Dispatcher) SyncLEDs() {
	d.mu.RLock()
	leds := d.leds
	chs := d.channels
	globals := d.globalLEDs
	d.mu.RUnlock()

	if leds == nil {
		return
	}
	for ch, c := range chs {
		// R LED: on when Rec is toggled, channel is pinned, or advanced mode is active (blink goroutine will take over).
		leds.SetButtonLED(ch, midi.ButtonRec, c.Rec || c.Pinned || c.Advanced)
		leds.SetButtonLED(ch, midi.ButtonSolo, c.Solo)
		leds.SetButtonLED(ch, midi.ButtonMute, c.Mute || c.SoloMuted)
		leds.SetButtonLED(ch, midi.ButtonStop, c.Stop)
	}
	for i, action := range globalLEDActions {
		leds.SetGlobalLED(action, globals[i])
	}
}

// OnGlobal handles a transport button press, switching the active page and
// updating the page button LEDs so exactly one is lit at any time.
func (d *Dispatcher) OnGlobal(m midi.GlobalMsg) {
	if !m.Pressed {
		return
	}
	page, ok := actionPage(m.Action)
	if !ok {
		return
	}

	d.mu.Lock()
	if d.activePage == page {
		d.activePage = "main"
	} else {
		d.activePage = page
	}
	newPage := d.activePage
	for i, a := range globalLEDActions {
		p, _ := actionPage(a)
		d.globalLEDs[i] = p == newPage && newPage != "main"
	}
	newLEDs := d.globalLEDs
	leds := d.leds
	// Collect cancels for advanced mode channels; clear their state.
	var advCancels [8]context.CancelFunc
	var advRLED [8]bool // desired R LED state (Rec || Pinned) after exiting advanced mode
	for ch := range d.channels {
		if d.channels[ch].Advanced {
			d.channels[ch].Advanced = false
			advCancels[ch] = d.advancedCancels[ch]
			d.advancedCancels[ch] = nil
			d.blinkGen[ch]++
			advRLED[ch] = d.channels[ch].Rec || d.channels[ch].Pinned
		}
	}
	cb := d.pageChangeCallback
	d.mu.Unlock()

	if leds != nil {
		for i, a := range globalLEDActions {
			leds.SetGlobalLED(a, newLEDs[i])
		}
	}
	// Stop blink goroutines; the goroutine itself restores the R LED on ctx.Done().
	for ch, cancel := range advCancels {
		if cancel != nil {
			cancel()
			// Restore R LED for channels that were in advanced mode (goroutine may
			// not have exited yet, but this write is correct and the goroutine's
			// final write will converge to the same value via restoreRLED).
			if leds != nil {
				leds.SetButtonLED(ch, midi.ButtonRec, advRLED[ch])
			}
		}
	}
	if cb != nil {
		cb()
	}
}

// ActivePage returns the name of the currently active page ("main" if none).
func (d *Dispatcher) ActivePage() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.activePage
}

// actionPage maps a GlobalAction to its page name.
func actionPage(a midi.GlobalAction) (string, bool) {
	switch a {
	case midi.ActionPlay:
		return "applications", true
	case midi.ActionRecord:
		return "inputs", true
	case midi.ActionPause:
		return "outputs", true
	case midi.ActionPrevious:
		return "system", true
	case midi.ActionNext:
		return "custom", true
	}
	return "", false
}

// UpdatePlaybackStatus syncs the Stop button LED to the actual MPRIS playback
// state. playing=true means the stream is actively playing (LED on).
// No-op if the state hasn't changed, to avoid redundant hardware writes.
func (d *Dispatcher) UpdatePlaybackStatus(ch int, playing bool) {
	d.mu.Lock()
	if d.channels[ch].Stop == playing {
		d.mu.Unlock()
		return
	}
	d.channels[ch].Stop = playing
	leds := d.leds
	d.mu.Unlock()
	if leds != nil {
		leds.SetButtonLED(ch, midi.ButtonStop, playing)
	}
}
