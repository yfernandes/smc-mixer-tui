package dispatcher

import (
	"context"
	"log"
	"math"
	"time"

	"github.com/yfernandes/smc-mixer-tui/midi"
)

func (d *Dispatcher) onKnob(ctx context.Context, m midi.KnobMsg) {
	d.mu.Lock()
	adv := d.channels[m.Channel].Advanced
	advSpec := d.channels[m.Channel].advancedSpec
	if adv && advSpec != nil {
		delta := m.Delta
		d.mu.Unlock()
		log.Printf("advanced knob ch%d: effect=%q delta=%+d", m.Channel, advSpec.KnobEffect, delta)
		return
	}
	k := clampKnob(d.channels[m.Channel].Knob + m.Delta)
	d.channels[m.Channel].Knob = k
	ctrl := d.channels[m.Channel].crossfader
	knobID := d.channels[m.Channel].KnobStreamID
	d.mu.Unlock()

	if ctrl != nil {
		volA, volB := crossfadeGains(k)
		if d.volThrottle == 0 {
			if err := ctrl.SetGains(ctx, volA, volB); err != nil {
				log.Printf("knob ch%d crossfade: %v", m.Channel, err)
			}
			return
		}
		// Latest-wins: replace any pending gains.
		select {
		case <-d.crossWorkers[m.Channel]:
		default:
		}
		d.crossWorkers[m.Channel] <- crossGains{volA, volB}
		return
	}

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

// crossfadeGains implements plateau crossfade:
// center = both sinks at full; edges fade one side to zero.
func crossfadeGains(knob int) (float64, float64) {
	ratio := float64(knob) / 127.0
	return math.Min(1.0, 2.0*(1.0-ratio)), math.Min(1.0, 2.0*ratio)
}

// SetCrossfader configures the crossfader controller for channel ch.
// Pass nil to disable. nameA/nameB are display-only labels for the UI.
func (d *Dispatcher) SetCrossfader(ch int, ctrl CrossfaderController, nameA, nameB string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.channels[ch].crossfader = ctrl
	d.channels[ch].CrossSinkAName = nameA
	d.channels[ch].CrossSinkBName = nameB
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

// runCrossWorker throttles PipeWire crossfader gain writes for one channel.
func (d *Dispatcher) runCrossWorker(ctx context.Context, ch int) {
	ticker := time.NewTicker(d.volThrottle)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			select {
			case gains := <-d.crossWorkers[ch]:
				d.mu.RLock()
				ctrl := d.channels[ch].crossfader
				d.mu.RUnlock()
				if ctrl != nil {
					if err := ctrl.SetGains(ctx, gains[0], gains[1]); err != nil {
						log.Printf("cross worker ch%d: %v", ch, err)
					}
				}
			default:
			}
		}
	}
}
