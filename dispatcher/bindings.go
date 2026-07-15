package dispatcher

import (
	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/midi"
)

// Bind assigns a PipeWire stream to a channel strip.
// If the stream is already bound to a different channel, that channel is
// unbound first — a stream may only be controlled by one channel at a time.
// The new channel starts unsynced: the fader must be brought to zero before
// it takes control of PipeWire, preventing accidental volume blasts on rebind.
func (d *Dispatcher) Bind(ch int, id uint32, name string, kind audio.NodeKind, mprisName string) {
	d.mu.Lock()
	// Release any other channel already holding this stream.
	var evicted []int
	for i := range d.channels {
		if i != ch && d.channels[i].boundTo(id) {
			d.channels[i].clearBinding()
			evicted = append(evicted, i)
		}
	}
	d.channels[ch].ManuallyUnbound = false
	d.channels[ch].bind(streamBinding{
		id:        id,
		name:      name,
		kind:      kind,
		mprisName: mprisName,
	})
	leds := d.leds
	rLED := d.channels[ch].Rec || d.channels[ch].Pinned
	d.mu.Unlock()

	if leds != nil {
		for _, i := range evicted {
			leds.SetButtonLED(i, midi.ButtonStop, false)
		}
		leds.SetButtonLED(ch, midi.ButtonStop, false)
		leds.SetButtonLED(ch, midi.ButtonRec, rLED)
	}
}

// UserBind assigns a PipeWire stream to a channel strip in response to an explicit
// user action. Behaves like Bind but sets UserBound=true so planBindings will not
// override this slot with a config-driven stream while the stream is live.
// pid is the OS process ID of the stream; when non-zero it is stored as BoundPID so
// that if the stream dies a new stream from the same process is reattached automatically.
// mediaName is the PipeWire media.name (e.g. tab title); stored as BoundMediaName so
// PID-based reconnect can prefer the same tab when multiple tabs share a PID.
func (d *Dispatcher) UserBind(ch int, id uint32, name string, kind audio.NodeKind, mprisName string, pid uint32, mediaName string) {
	d.mu.Lock()
	var evicted []int
	for i := range d.channels {
		if i != ch && d.channels[i].boundTo(id) {
			d.channels[i].clearBinding()
			evicted = append(evicted, i)
		}
	}
	d.channels[ch].ManuallyUnbound = false
	d.channels[ch].bind(streamBinding{
		id:        id,
		name:      name,
		kind:      kind,
		mprisName: mprisName,
	})
	d.channels[ch].UserBound = true
	d.channels[ch].BoundPID = pid
	d.channels[ch].BoundMediaName = mediaName
	leds := d.leds
	rLED := d.channels[ch].Rec || d.channels[ch].Pinned
	d.mu.Unlock()

	if leds != nil {
		for _, i := range evicted {
			leds.SetButtonLED(i, midi.ButtonStop, false)
		}
		leds.SetButtonLED(ch, midi.ButtonStop, false)
		leds.SetButtonLED(ch, midi.ButtonRec, rLED)
	}
}

// UpdateBindingMetadata refreshes non-control metadata for an existing binding.
// It is intentionally narrower than Bind: it must not reset fader pickup state,
// volume tracking, or crossfader routing.
func (d *Dispatcher) UpdateBindingMetadata(ch int, id uint32, name, mprisName string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.channels[ch].boundTo(id) {
		return
	}
	if name != "" {
		d.channels[ch].Name = name
	}
	d.channels[ch].MPRISName = mprisName
}

// LoseBinding clears the stream binding from a channel strip without suppressing
// config-driven auto-rebind. Use when PipeWire reports the bound stream is gone.
func (d *Dispatcher) LoseBinding(ch int) {
	d.mu.Lock()
	d.channels[ch].clearBinding()
	d.channels[ch].Advanced = false
	oldCancel := d.advancedCancels[ch]
	d.advancedCancels[ch] = nil
	d.blinkGen[ch]++
	leds := d.leds
	rLED := d.channels[ch].Rec // Pinned excluded: stream is offline, LED turns off until rebind
	d.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
	}
	if leds != nil {
		leds.SetButtonLED(ch, midi.ButtonStop, false)
		leds.SetButtonLED(ch, midi.ButtonRec, rLED)
	}
}

// ResetStrip clears the stream binding AND all button states (Mute, Solo, Stop, Rec)
// for a channel, then syncs all LEDs. Use on page switch, where the strip is about
// to control a different device and stale button state would be misleading.
func (d *Dispatcher) ResetStrip(ch int) {
	d.mu.Lock()
	d.channels[ch].clearBinding()
	d.channels[ch].BoundPID = 0
	d.channels[ch].Mute = false
	d.channels[ch].SoloMuted = false
	d.channels[ch].Solo = false
	d.channels[ch].Stop = false
	d.channels[ch].Advanced = false
	oldCancel := d.advancedCancels[ch]
	d.advancedCancels[ch] = nil
	d.blinkGen[ch]++
	leds := d.leds
	rLED := d.channels[ch].Rec || d.channels[ch].Pinned
	d.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
	}
	// Discard any pending throttled volume write queued before the page switch.
	// The worker checks bound before calling SetVolume, but draining here prevents
	// a stale value from being applied to the new stream after rebind.
	select {
	case <-d.volWorkers[ch]:
	default:
	}
	if leds != nil {
		leds.SetButtonLED(ch, midi.ButtonMute, false)
		leds.SetButtonLED(ch, midi.ButtonSolo, false)
		leds.SetButtonLED(ch, midi.ButtonStop, false)
		leds.SetButtonLED(ch, midi.ButtonRec, rLED)
	}
}

// SetChannelSyncMode configures fader sync behaviour for channel ch.
// mode selects zero (drive-to-zero) or soft_pickup (cross-actual-volume) detection.
// tol is the soft pickup tolerance in 0.0–1.0 scale; 0 uses the default PickupThreshold.
// Call after Bind to apply per-device config; SyncMode persists across Bind calls until
// explicitly changed.
func (d *Dispatcher) SetChannelSyncMode(ch int, mode SyncMode, tol float64) {
	d.mu.Lock()
	d.channels[ch].SyncMode = mode
	d.channels[ch].pickupTol = tol
	d.mu.Unlock()
}

// BindKnob sets the PipeWire node that this channel's knob controls for gain writes.
// Used on the main page where knob and fader target independent devices.
// Returns true if the binding was new or changed.
func (d *Dispatcher) BindKnob(ch int, id uint32) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.channels[ch].KnobStreamID != nil && *d.channels[ch].KnobStreamID == id {
		return false
	}
	idCopy := id
	d.channels[ch].KnobStreamID = &idCopy
	return true
}

// SetKnob sets the accumulated knob position for channel ch.
// Use to seed the position from an externally-known volume (e.g. at startup).
func (d *Dispatcher) SetKnob(ch int, val int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.channels[ch].Knob = clampKnob(val)
}

// LoseKnob clears the knob's independent device binding.
func (d *Dispatcher) LoseKnob(ch int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.channels[ch].KnobStreamID = nil
}

// Unbind removes the stream binding from a channel strip and suppresses
// config-driven auto-rebind for the rest of the session.
func (d *Dispatcher) Unbind(ch int) {
	d.mu.Lock()
	d.channels[ch].clearBinding()
	d.channels[ch].BoundPID = 0
	d.channels[ch].ManuallyUnbound = true
	d.channels[ch].Advanced = false
	oldCancel := d.advancedCancels[ch]
	d.advancedCancels[ch] = nil
	d.blinkGen[ch]++
	leds := d.leds
	rLED := d.channels[ch].Rec || d.channels[ch].Pinned
	d.mu.Unlock()

	if oldCancel != nil {
		oldCancel()
	}
	if leds != nil {
		leds.SetButtonLED(ch, midi.ButtonStop, false)
		leds.SetButtonLED(ch, midi.ButtonRec, rLED)
	}
}
