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
	d.mu.Unlock()

	if leds != nil {
		for _, i := range evicted {
			leds.SetFaderLED(i, false)
			leds.SetButtonLED(i, midi.ButtonStop, false)
		}
		leds.SetFaderLED(ch, false) // off until fader reaches zero
		leds.SetButtonLED(ch, midi.ButtonStop, false)
	}
}

// UserBind assigns a PipeWire stream to a channel strip in response to an explicit
// user action. Behaves like Bind but sets UserBound=true so planBindings will not
// override this slot with a config-driven stream while the stream is live.
func (d *Dispatcher) UserBind(ch int, id uint32, name string, kind audio.NodeKind, mprisName string) {
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
	leds := d.leds
	d.mu.Unlock()

	if leds != nil {
		for _, i := range evicted {
			leds.SetFaderLED(i, false)
			leds.SetButtonLED(i, midi.ButtonStop, false)
		}
		leds.SetFaderLED(ch, false)
		leds.SetButtonLED(ch, midi.ButtonStop, false)
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
	d.channels[ch].advancedSpec = nil
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
		leds.SetFaderLED(ch, false)
		leds.SetButtonLED(ch, midi.ButtonStop, false)
		leds.SetButtonLED(ch, midi.ButtonRec, rLED)
	}
}

// BindKnob sets the PipeWire node that this channel's knob controls for gain writes.
// Used on the main page where knob and fader target independent devices.
func (d *Dispatcher) BindKnob(ch int, id uint32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	idCopy := id
	d.channels[ch].KnobStreamID = &idCopy
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
	d.channels[ch].ManuallyUnbound = true
	d.channels[ch].Advanced = false
	d.channels[ch].advancedSpec = nil
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
		leds.SetFaderLED(ch, false)
		leds.SetButtonLED(ch, midi.ButtonStop, false)
		leds.SetButtonLED(ch, midi.ButtonRec, rLED)
	}
}
