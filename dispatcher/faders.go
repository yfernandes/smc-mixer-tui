package dispatcher

import (
	"context"
	"log"
	"time"

	"github.com/yfernandes/smc-mixer-tui/midi"
)

// UpdateActualVolume records the PipeWire-reported volume for a channel.
// It only updates display state; synced channels are never desynced by polling.
// For unsynced channels in soft pickup mode, it resets pickupSide so the next
// fader message recomputes direction relative to the new target.
func (d *Dispatcher) UpdateActualVolume(ch int, vol float64) {
	d.mu.Lock()
	prev := d.channels[ch].ActualVolume
	d.channels[ch].ActualVolume = vol
	if !d.channels[ch].Synced && vol != prev {
		// Target changed while unsynced: stale pickupSide would track the wrong side.
		// Reset to 0 so moveSoftPickup re-evaluates on the next fader message.
		d.channels[ch].pickupSide = 0
	}
	d.mu.Unlock()
}

func (d *Dispatcher) onFader(ctx context.Context, m midi.FaderMsg) {
	faderPos := float64(m.Value) / 16383.0

	d.mu.Lock()
	adv := d.channels[m.Channel].Advanced
	advSpec := d.channels[m.Channel].advancedSpec
	if adv && advSpec != nil {
		d.mu.Unlock()
		log.Printf("advanced fader ch%d: effect=%q value=%d", m.Channel, advSpec.FaderEffect, m.Value)
		return
	}
	update := d.channels[m.Channel].moveFader(faderPos)
	d.mu.Unlock()

	if !update.synced || !update.bound {
		return
	}

	if d.volThrottle == 0 {
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

// runVolWorker throttles PipeWire volume writes for one channel.
// It fires at most once per volThrottle interval, always with the latest value.
// The goroutine blocks during the wpctl call, so at most one call is in-flight at a time.
func (d *Dispatcher) runVolWorker(ctx context.Context, ch int) {
	ticker := time.NewTicker(d.volThrottle)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			select {
			case vol := <-d.volWorkers[ch]:
				d.mu.RLock()
				id, bound := d.channels[ch].boundID()
				d.mu.RUnlock()
				if bound {
					if err := d.pw.SetVolume(ctx, id, vol); err != nil {
						log.Printf("vol worker ch%d: %v", ch, err)
					}
				}
			default:
			}
		}
	}
}
