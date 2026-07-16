package dispatcher

import (
	"context"
	"log"
	"time"

	"github.com/yfernandes/smc-mixer-tui/midi"
)

func (d *Dispatcher) onKnob(ctx context.Context, m midi.KnobMsg) {
	d.mu.Lock()
	k := clampKnob(d.channels[m.Channel].Knob + m.Delta)
	d.channels[m.Channel].Knob = k
	knobID := d.channels[m.Channel].KnobStreamID
	d.mu.Unlock()

	if knobID == nil {
		return
	}
	vol := float64(k) / 127.0
	if d.volThrottle == 0 {
		if err := d.pw.SetVolume(ctx, *knobID, vol); err != nil {
			log.Printf("knob ch%d gain: %v", m.Channel, err)
		}
		return
	}
	// Latest-wins: replace any pending volume.
	select {
	case <-d.knobVolWorkers[m.Channel]:
	default:
	}
	d.knobVolWorkers[m.Channel] <- vol
}

func clampKnob(k int) int {
	if k < 0 {
		return 0
	}
	if k > 127 {
		return 127
	}
	return k
}

// runKnobVolWorker throttles PipeWire volume writes for knob-bound devices (gain type).
func (d *Dispatcher) runKnobVolWorker(ctx context.Context, ch int) {
	ticker := time.NewTicker(d.volThrottle)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			select {
			case vol := <-d.knobVolWorkers[ch]:
				d.mu.RLock()
				knobID := d.channels[ch].KnobStreamID
				d.mu.RUnlock()
				if knobID != nil {
					if err := d.pw.SetVolume(ctx, *knobID, vol); err != nil {
						log.Printf("knob vol worker ch%d: %v", ch, err)
					}
				}
			default:
			}
		}
	}
}
