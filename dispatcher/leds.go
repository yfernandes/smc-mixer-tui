package dispatcher

import "github.com/yfernandes/smc-mixer-tui/midi"

// SetLEDWriter sets (or clears, if nil) the LED output device.
func (d *Dispatcher) SetLEDWriter(w LEDWriter) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.leds = w
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
		leds.SetButtonLED(ch, midi.ButtonRec, c.Rec)
		leds.SetButtonLED(ch, midi.ButtonSolo, c.Solo)
		leds.SetButtonLED(ch, midi.ButtonMute, c.Mute || c.SoloMuted)
		leds.SetButtonLED(ch, midi.ButtonStop, c.Stop)
		// Fader LED blinks when bound AND synced; off when unbound or awaiting pickup.
		leds.SetFaderLED(ch, c.StreamID != nil && c.Synced)
	}
	for i, action := range globalLEDActions {
		leds.SetGlobalLED(action, globals[i])
	}
}

// OnGlobal toggles the LED for a transport button press.
func (d *Dispatcher) OnGlobal(m midi.GlobalMsg) {
	if !m.Pressed {
		return
	}
	var idx int
	found := false
	for i, a := range globalLEDActions {
		if a == m.Action {
			idx, found = i, true
			break
		}
	}
	if !found {
		return
	}

	d.mu.Lock()
	d.globalLEDs[idx] = !d.globalLEDs[idx]
	state := d.globalLEDs[idx]
	leds := d.leds
	d.mu.Unlock()

	if leds != nil {
		leds.SetGlobalLED(m.Action, state)
	}
}

// UpdatePlaybackStatus syncs the Stop button LED to the actual MPRIS playback
// state. playing=true means the stream is actively playing (LED on).
func (d *Dispatcher) UpdatePlaybackStatus(ch int, playing bool) {
	d.mu.Lock()
	d.channels[ch].Stop = playing
	leds := d.leds
	d.mu.Unlock()
	if leds != nil {
		leds.SetButtonLED(ch, midi.ButtonStop, playing)
	}
}
