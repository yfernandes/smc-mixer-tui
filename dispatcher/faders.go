package dispatcher

import (
	"context"
	"log"

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
	update := d.channels[ch].updateActualVolume(vol)
	leds := d.leds
	d.mu.Unlock()

	if leds == nil {
		return
	}
	leds.SetFaderLED(ch, update.bound && update.synced)
	if update.justDesynced {
		leds.SetFaderPosition(ch, update.desyncFaderTarget)
	}
}

func (d *Dispatcher) onFader(ctx context.Context, m midi.FaderMsg) {
	faderPos := float64(m.Value) / 127.0

	d.mu.Lock()
	update := d.channels[m.Channel].moveFader(faderPos)
	leds := d.leds
	d.mu.Unlock()

	if leds != nil {
		leds.SetFaderLED(m.Channel, update.bound && update.synced)
	}

	if !update.synced {
		return // fader has not yet picked up the actual volume
	}
	if !update.bound {
		return
	}
	if err := d.pw.SetVolume(ctx, update.streamID, faderPos); err != nil {
		log.Printf("fader ch%d: SetVolume(%d, %.3f): %v", m.Channel, update.streamID, faderPos, err)
	}
}
