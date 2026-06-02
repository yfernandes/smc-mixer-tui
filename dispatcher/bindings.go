package dispatcher

import "github.com/yfernandes/smc-mixer-tui/audio"

// Bind assigns a PipeWire stream to a channel strip.
// If the stream is already bound to a different channel, that channel is
// unbound first — a stream may only be controlled by one channel at a time.
// The new channel starts unsynced: the fader must reach the actual volume
// before it takes control of PipeWire.
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
		}
		leds.SetFaderLED(ch, false) // off until fader picks up
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

// Unbind removes the stream binding from a channel strip.
func (d *Dispatcher) Unbind(ch int) {
	d.mu.Lock()
	d.channels[ch].clearBinding()
	leds := d.leds
	d.mu.Unlock()

	if leds != nil {
		leds.SetFaderLED(ch, false)
	}
}
