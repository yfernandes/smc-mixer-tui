package dispatcher

import (
	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/midi"
)

func (d *Dispatcher) recomputeSoloGroup(kind audio.NodeKind) ([]muteUpdate, []buttonLED) {
	anySoloed := d.anySoloedInKind(kind)

	var muteUpdates []muteUpdate
	var leds []buttonLED
	for i := range d.channels {
		if d.channels[i].StreamID == nil || d.channels[i].Kind != kind {
			continue
		}
		c := &d.channels[i]
		c.SoloMuted = anySoloed && !c.Solo
		id, _ := c.boundID()
		muteUpdates = append(muteUpdates, muteUpdate{
			ch:    i,
			id:    id,
			bound: true,
			mute:  c.effectiveMute(),
		})
		leds = append(leds,
			buttonLED{ch: i, kind: midi.ButtonMute, active: c.effectiveMute()},
			buttonLED{ch: i, kind: midi.ButtonSolo, active: c.Solo},
		)
	}
	return muteUpdates, leds
}

func (d *Dispatcher) anySoloedInKind(kind audio.NodeKind) bool {
	for i := range d.channels {
		if d.channels[i].Kind == kind && d.channels[i].Solo {
			return true
		}
	}
	return false
}
