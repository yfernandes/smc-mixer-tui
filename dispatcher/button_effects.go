package dispatcher

import (
	"context"
	"log"

	"github.com/yfernandes/smc-mixer-tui/midi"
)

func (d *Dispatcher) applyButtonEffects(ctx context.Context, m midi.ButtonMsg, effects buttonEffects, leds LEDWriter, mpris MPRISCaller) {
	d.applyNormalButtonEffects(ctx, m, effects, leds, mpris)
}

func (d *Dispatcher) applyNormalButtonEffects(ctx context.Context, m midi.ButtonMsg, effects buttonEffects, leds LEDWriter, mpris MPRISCaller) {
	if leds != nil {
		leds.SetButtonLED(m.Channel, m.Kind, effects.ledState)
	}

	switch m.Kind {
	case midi.ButtonMute:
		d.applyMuteEffects(ctx, m.Channel, effects)
	case midi.ButtonSolo:
		d.applySoloEffects(ctx, effects, leds)
	case midi.ButtonStop:
		d.applyStopEffects(ctx, m.Channel, effects, mpris)
	}
}

func (d *Dispatcher) applyMuteEffects(ctx context.Context, ch int, effects buttonEffects) {
	if !effects.chBound {
		return
	}
	if err := d.pw.SetMute(ctx, effects.chID, effects.chMuteEffective); err != nil {
		log.Printf("button ch%d mute: SetMute(%d, %v): %v", ch, effects.chID, effects.chMuteEffective, err)
	}
}

func (d *Dispatcher) applySoloEffects(ctx context.Context, effects buttonEffects, leds LEDWriter) {
	for _, u := range effects.soloUpdates {
		if !u.bound {
			continue
		}
		if err := d.pw.SetMute(ctx, u.id, u.mute); err != nil {
			log.Printf("solo ch%d: SetMute(%d, %v): %v", u.ch, u.id, u.mute, err)
		}
	}
	if leds == nil {
		return
	}
	for _, led := range effects.soloLEDs {
		leds.SetButtonLED(led.ch, led.kind, led.active)
	}
}

func (d *Dispatcher) applyStopEffects(ctx context.Context, ch int, effects buttonEffects, mpris MPRISCaller) {
	if effects.chMPRIS == "" || mpris == nil {
		return
	}
	if err := mpris.PlayPause(ctx, effects.chMPRIS); err != nil {
		log.Printf("button ch%d stop: PlayPause(%s): %v", ch, effects.chMPRIS, err)
	}
}

func buttonKindName(k midi.ButtonKind) string {
	switch k {
	case midi.ButtonMute:
		return "mute"
	case midi.ButtonSolo:
		return "solo"
	case midi.ButtonStop:
		return "stop"
	default:
		return "rec"
	}
}
