package dispatcher

import (
	"context"

	"github.com/yfernandes/smc-mixer-tui/midi"
)

// applyButtonState mutates channel state and captures effects for lock-free I/O.
func (d *Dispatcher) applyButtonState(ctx context.Context, m midi.ButtonMsg) buttonEffects {
	return d.applyNormalButtonState(ctx, m)
}

func (d *Dispatcher) applyNormalButtonState(_ context.Context, m midi.ButtonMsg) buttonEffects {
	ch := &d.channels[m.Channel]
	effects := buttonEffects{}

	switch m.Kind {
	case midi.ButtonMute, midi.ButtonSolo, midi.ButtonStop:
		effects.ledState = ch.toggleButton(m.Kind)
	case midi.ButtonRec:
		effects.ledState = ch.toggleButton(m.Kind)
	}

	effects = captureButtonChannelEffects(ch, effects)
	if m.Kind == midi.ButtonSolo {
		effects.soloUpdates, effects.soloLEDs = d.recomputeSoloGroup(ch.Kind)
	}
	return effects
}

func captureButtonChannelEffects(ch *Channel, effects buttonEffects) buttonEffects {
	effects.chID, effects.chBound = ch.boundID()
	effects.chMuteEffective = ch.effectiveMute()
	effects.chMPRIS = ch.MPRISName
	return effects
}
