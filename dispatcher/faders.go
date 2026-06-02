package dispatcher

import (
	"context"
	"log"
	"math"

	"github.com/yfernandes/smc-mixer-tui/midi"
)

// syncFaderCap is the maximum position we park the fader at when desyncing.
const syncFaderCap = 0.25

// UpdateActualVolume records the PipeWire-reported volume for a channel.
// If the volume differs significantly from the last value we set, the channel
// is desynced so the user must pick up the fader again. On desync the fader
// is moved to min(actualVol, syncFaderCap) so the user has a low reference
// point to pick up from.
func (d *Dispatcher) UpdateActualVolume(ch int, vol float64) {
	d.mu.Lock()
	c := &d.channels[ch]
	c.ActualVolume = vol
	wasSync := c.Synced
	if c.Synced && math.Abs(vol-c.LastSetVol) > PickupThreshold {
		c.Synced = false
	}
	justDesynced := wasSync && !c.Synced
	bound, synced, leds := c.StreamID != nil, c.Synced, d.leds
	d.mu.Unlock()

	if leds == nil {
		return
	}
	leds.SetFaderLED(ch, bound && synced)
	if justDesynced {
		target := vol
		if target > syncFaderCap {
			target = syncFaderCap
		}
		leds.SetFaderPosition(ch, target)
	}
}

func (d *Dispatcher) onFader(ctx context.Context, m midi.FaderMsg) {
	faderPos := float64(m.Value) / 127.0

	d.mu.Lock()
	c := &d.channels[m.Channel]
	c.FaderPos = faderPos
	if !c.Synced && math.Abs(faderPos-c.ActualVolume) < PickupThreshold {
		c.Synced = true
	}
	synced, id, leds := c.Synced, c.StreamID, d.leds
	if synced {
		c.LastSetVol = faderPos
	}
	d.mu.Unlock()

	if leds != nil {
		leds.SetFaderLED(m.Channel, id != nil && synced)
	}

	if !synced {
		return // fader has not yet picked up the actual volume
	}
	if id == nil {
		return
	}
	if err := d.pw.SetVolume(ctx, *id, faderPos); err != nil {
		log.Printf("fader ch%d: SetVolume(%d, %.3f): %v", m.Channel, *id, faderPos, err)
	}
}
