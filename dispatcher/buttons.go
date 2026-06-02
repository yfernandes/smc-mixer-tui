package dispatcher

import (
	"context"
	"log"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/midi"
)

func (d *Dispatcher) onButton(ctx context.Context, m midi.ButtonMsg) {
	if !m.Pressed {
		return
	}

	d.mu.Lock()
	effects := d.applyButtonState(m)
	leds := d.leds
	mpris := d.mpris
	d.mu.Unlock()

	d.applyButtonEffects(ctx, m, effects, leds, mpris)
}

type muteUpdate struct {
	ch    int
	id    uint32
	bound bool
	mute  bool
}

type buttonEffects struct {
	ledState        bool
	chBound         bool
	chID            uint32
	chMuteEffective bool
	chMPRIS         string
	soloUpdates     []muteUpdate
	soloLEDs        []buttonLED
}

type buttonLED struct {
	ch     int
	kind   midi.ButtonKind
	active bool
}

// applyButtonState mutates channel state and captures the side effects that must
// run after the dispatcher lock is released.
func (d *Dispatcher) applyButtonState(m midi.ButtonMsg) buttonEffects {
	ch := &d.channels[m.Channel]
	effects := buttonEffects{}

	switch m.Kind {
	case midi.ButtonMute:
		effects.ledState = ch.toggleButton(m.Kind)
	case midi.ButtonSolo:
		effects.ledState = ch.toggleButton(m.Kind)
	case midi.ButtonRec:
		effects.ledState = ch.toggleButton(m.Kind)
	case midi.ButtonStop:
		effects.ledState = ch.toggleButton(m.Kind)
	}

	effects.chID, effects.chBound = ch.boundID()
	effects.chMuteEffective = ch.effectiveMute()
	effects.chMPRIS = ch.MPRISName

	if m.Kind == midi.ButtonSolo {
		effects.soloUpdates, effects.soloLEDs = d.recomputeSoloGroup(ch.Kind)
	}

	return effects
}

func (d *Dispatcher) recomputeSoloGroup(kind audio.NodeKind) ([]muteUpdate, []buttonLED) {
	anySoloed := false
	for i := range d.channels {
		if d.channels[i].Kind == kind && d.channels[i].Solo {
			anySoloed = true
			break
		}
	}

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

func (d *Dispatcher) applyButtonEffects(ctx context.Context, m midi.ButtonMsg, effects buttonEffects, leds LEDWriter, mpris MPRISCaller) {
	if leds != nil {
		leds.SetButtonLED(m.Channel, m.Kind, effects.ledState)
	}

	switch m.Kind {
	case midi.ButtonMute:
		if effects.chBound {
			if err := d.pw.SetMute(ctx, effects.chID, effects.chMuteEffective); err != nil {
				log.Printf("button ch%d mute: SetMute(%d, %v): %v", m.Channel, effects.chID, effects.chMuteEffective, err)
			}
		}
	case midi.ButtonSolo:
		for _, u := range effects.soloUpdates {
			if !u.bound {
				continue
			}
			if err := d.pw.SetMute(ctx, u.id, u.mute); err != nil {
				log.Printf("solo ch%d: SetMute(%d, %v): %v", u.ch, u.id, u.mute, err)
			}
		}
		if leds != nil {
			for _, led := range effects.soloLEDs {
				leds.SetButtonLED(led.ch, led.kind, led.active)
			}
		}
	case midi.ButtonStop:
		if effects.chMPRIS != "" && mpris != nil {
			if err := mpris.PlayPause(ctx, effects.chMPRIS); err != nil {
				log.Printf("button ch%d stop: PlayPause(%s): %v", m.Channel, effects.chMPRIS, err)
			}
		}
	}
}
