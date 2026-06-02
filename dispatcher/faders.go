package dispatcher

import (
	"context"
	"log"
	"time"

	"github.com/yfernandes/smc-mixer-tui/midi"
)

// UpdateActualVolume records the PipeWire-reported volume for a channel.
// It only updates display state; synced channels are never desynced by polling
// because the fader is the source of truth.
func (d *Dispatcher) UpdateActualVolume(ch int, vol float64) {
	d.mu.Lock()
	d.channels[ch].ActualVolume = vol
	bound := d.channels[ch].StreamID != nil
	synced := d.channels[ch].Synced
	leds := d.leds
	d.mu.Unlock()

	if leds != nil {
		leds.SetFaderLED(ch, bound && synced)
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

	if !update.synced || !update.bound {
		return
	}

	if d.volDebounce == 0 {
		if err := d.pw.SetVolume(ctx, update.streamID, faderPos); err != nil {
			log.Printf("fader ch%d: SetVolume(%d, %.3f): %v", m.Channel, update.streamID, faderPos, err)
		}
		return
	}
	// Latest-wins: replace any pending value so only the most recent position is sent.
	select {
	case <-d.volWorkers[m.Channel]:
	default:
	}
	d.volWorkers[m.Channel] <- faderPos
}

// runVolWorker debounces PipeWire volume writes for one channel.
// It fires only after volDebounce of silence, sending the latest fader position.
func (d *Dispatcher) runVolWorker(ctx context.Context, ch int) {
	var pending float64
	hasPending := false

	timer := time.NewTimer(d.volDebounce)
	if !timer.Stop() {
		<-timer.C
	}

	for {
		select {
		case <-ctx.Done():
			return
		case vol := <-d.volWorkers[ch]:
			pending = vol
			hasPending = true
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(d.volDebounce)
		case <-timer.C:
			if !hasPending {
				continue
			}
			d.mu.RLock()
			id, bound := d.channels[ch].boundID()
			d.mu.RUnlock()
			if bound {
				if err := d.pw.SetVolume(ctx, id, pending); err != nil {
					log.Printf("vol worker ch%d: %v", ch, err)
				}
			}
			hasPending = false
		}
	}
}
