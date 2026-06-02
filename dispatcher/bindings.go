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
		if i != ch && d.channels[i].StreamID != nil && *d.channels[i].StreamID == id {
			d.channels[i].StreamID = nil
			d.channels[i].Name = ""
			d.channels[i].Synced = false
			evicted = append(evicted, i)
		}
	}
	d.channels[ch].StreamID = &id
	d.channels[ch].Name = name
	d.channels[ch].Kind = kind
	d.channels[ch].MPRISName = mprisName
	d.channels[ch].Synced = false
	d.channels[ch].ActualVolume = 0
	d.channels[ch].LastSetVol = -1
	leds := d.leds
	d.mu.Unlock()

	if leds != nil {
		for _, i := range evicted {
			leds.SetFaderLED(i, false)
		}
		leds.SetFaderLED(ch, false) // off until fader picks up
	}
}

// Unbind removes the stream binding from a channel strip.
func (d *Dispatcher) Unbind(ch int) {
	d.mu.Lock()
	d.channels[ch].StreamID = nil
	d.channels[ch].Name = ""
	d.channels[ch].Synced = false
	leds := d.leds
	d.mu.Unlock()

	if leds != nil {
		leds.SetFaderLED(ch, false)
	}
}
