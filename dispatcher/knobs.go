package dispatcher

import (
	"context"
	"log"
	"math"

	"github.com/yfernandes/smc-mixer-tui/midi"
)

func (d *Dispatcher) onKnob(ctx context.Context, m midi.KnobMsg) {
	d.mu.Lock()
	k := clampKnob(d.channels[m.Channel].Knob + m.Delta)
	d.channels[m.Channel].Knob = k
	ctrl := d.channels[m.Channel].crossfader
	d.mu.Unlock()

	if ctrl == nil {
		return
	}
	volA, volB := crossfadeGains(k)
	if err := ctrl.SetGains(ctx, volA, volB); err != nil {
		log.Printf("knob ch%d crossfade: %v", m.Channel, err)
	}
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
