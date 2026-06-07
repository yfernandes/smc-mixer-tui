package dispatcher

import (
	"context"

	"github.com/yfernandes/smc-mixer-tui/midi"
)

// applyButtonState mutates channel state and captures the side effects that must
// run after the dispatcher lock is released. ctx is needed to create the blink
// goroutine context while the lock is still held.
func (d *Dispatcher) applyButtonState(ctx context.Context, m midi.ButtonMsg) buttonEffects {
	ch := &d.channels[m.Channel]
	if ch.Advanced && ch.advancedSpec != nil {
		return d.applyAdvancedButtonState(m)
	}
	return d.applyNormalButtonState(ctx, m)
}

func (d *Dispatcher) applyAdvancedButtonState(m midi.ButtonMsg) buttonEffects {
	ch := &d.channels[m.Channel]
	effects := buttonEffects{}
	switch m.Kind {
	case midi.ButtonRec:
		ch.Advanced = false
		effects.advancedToggled = true
		effects.advancedNowOn = false
		effects.advancedOldCancel = d.advancedCancels[m.Channel]
		d.advancedCancels[m.Channel] = nil
		d.blinkGen[m.Channel]++
		effects.chRec = ch.Rec
		effects.chPinned = ch.Pinned
	case midi.ButtonMute:
		effects.isAdvancedAction = true
		effects.advancedAction = ch.advancedSpec.MuteButtonAction
	case midi.ButtonSolo:
		effects.isAdvancedAction = true
		effects.advancedAction = ch.advancedSpec.SoloButtonAction
	case midi.ButtonStop:
		effects.isAdvancedAction = true
		effects.advancedAction = ch.advancedSpec.StopButtonAction
	}
	return captureButtonChannelEffects(ch, effects)
}

func (d *Dispatcher) applyNormalButtonState(ctx context.Context, m midi.ButtonMsg) buttonEffects {
	ch := &d.channels[m.Channel]
	effects := buttonEffects{}

	switch m.Kind {
	case midi.ButtonMute, midi.ButtonSolo, midi.ButtonStop:
		effects.ledState = ch.toggleButton(m.Kind)
	case midi.ButtonRec:
		if ch.StreamID != nil && ch.advancedSpec != nil {
			effects = d.activateAdvancedMode(ctx, m.Channel)
		} else {
			effects.ledState = ch.toggleButton(m.Kind)
		}
	}

	effects = captureButtonChannelEffects(ch, effects)
	if m.Kind == midi.ButtonSolo {
		effects.soloUpdates, effects.soloLEDs = d.recomputeSoloGroup(ch.Kind)
	}
	return effects
}

func (d *Dispatcher) activateAdvancedMode(ctx context.Context, ch int) buttonEffects {
	bCtx, cancel := context.WithCancel(ctx)
	d.channels[ch].Advanced = true
	d.blinkGen[ch]++
	effects := buttonEffects{
		advancedToggled:   true,
		advancedNowOn:     true,
		advancedOldCancel: d.advancedCancels[ch],
		advancedBCtx:      bCtx,
		advancedGen:       d.blinkGen[ch],
	}
	d.advancedCancels[ch] = cancel
	return effects
}

func captureButtonChannelEffects(ch *Channel, effects buttonEffects) buttonEffects {
	effects.chID, effects.chBound = ch.boundID()
	effects.chMuteEffective = ch.effectiveMute()
	effects.chMPRIS = ch.MPRISName
	return effects
}
