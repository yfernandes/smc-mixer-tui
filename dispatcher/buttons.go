package dispatcher

import (
	"context"
	"log"
	"time"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/midi"
)

const stopButtonDebounce = 250 * time.Millisecond
const recLongPressThreshold = 500 * time.Millisecond

func (d *Dispatcher) onButton(ctx context.Context, m midi.ButtonMsg) {
	if m.Kind == midi.ButtonRec {
		d.onRecButton(ctx, m)
		return
	}
	if !m.Pressed {
		return
	}

	d.mu.Lock()
	if d.shouldIgnoreButtonLocked(m) {
		d.mu.Unlock()
		return
	}
	effects := d.applyButtonState(ctx, m)
	leds := d.leds
	mpris := d.mpris
	d.mu.Unlock()

	d.applyButtonEffects(ctx, m, effects, leds, mpris)
}

func (d *Dispatcher) onRecButton(ctx context.Context, m midi.ButtonMsg) {
	ch := m.Channel
	if m.Pressed {
		d.mu.Lock()
		d.rPressedAt[ch] = time.Now()
		d.mu.Unlock()
		return
	}

	// Release: determine short vs long press.
	d.mu.Lock()
	pressedAt := d.rPressedAt[ch]
	d.rPressedAt[ch] = time.Time{}
	activePage := d.activePage
	streamID := d.channels[ch].StreamID
	pinned := d.channels[ch].Pinned
	pinCB := d.pinCallback
	d.mu.Unlock()

	if pressedAt.IsZero() {
		return
	}

	if time.Since(pressedAt) >= recLongPressThreshold {
		if streamID == nil || pinCB == nil {
			return
		}
		if activePage == "main" {
			// On main page, long press unpins if pinned; no-op otherwise.
			if pinned {
				pinCB(ch)
			}
			return
		}
		pinCB(ch)
		return
	}

	// Short press: existing R button logic (advanced mode toggle or Rec toggle).
	pressMsg := midi.ButtonMsg{Channel: ch, Kind: midi.ButtonRec, Pressed: true}
	d.mu.Lock()
	effects := d.applyButtonState(ctx, pressMsg)
	leds := d.leds
	mpris := d.mpris
	d.mu.Unlock()
	d.applyButtonEffects(ctx, pressMsg, effects, leds, mpris)
}

func (d *Dispatcher) shouldIgnoreButtonLocked(m midi.ButtonMsg) bool {
	if m.Kind != midi.ButtonStop {
		return false
	}
	now := time.Now()
	if !d.lastStopAt[m.Channel].IsZero() && now.Sub(d.lastStopAt[m.Channel]) < stopButtonDebounce {
		return true
	}
	d.lastStopAt[m.Channel] = now
	return false
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
	// advanced mode fields
	advancedToggled   bool
	advancedNowOn     bool
	advancedOldCancel context.CancelFunc
	advancedBCtx      context.Context // non-nil when advancedNowOn; goroutine must use this
	advancedGen       uint32          // blinkGen value captured at activation
	chRec             bool            // Rec state to restore LED after deactivation
	chPinned          bool            // Pinned state to restore LED after deactivation
	isAdvancedAction  bool            // button was consumed in advanced mode (not a toggle)
	advancedAction    string          // action name to log
}

type buttonLED struct {
	ch     int
	kind   midi.ButtonKind
	active bool
}

// applyButtonState mutates channel state and captures the side effects that must
// run after the dispatcher lock is released. ctx is needed to create the blink
// goroutine context while the lock is still held (fix for the TOCTOU race in fix 3).
func (d *Dispatcher) applyButtonState(ctx context.Context, m midi.ButtonMsg) buttonEffects {
	ch := &d.channels[m.Channel]
	effects := buttonEffects{}

	// Advanced mode: reroute fader/knob controls to stubs; R exits advanced mode.
	if ch.Advanced && ch.advancedSpec != nil {
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
		effects.chID, effects.chBound = ch.boundID()
		effects.chMuteEffective = ch.effectiveMute()
		effects.chMPRIS = ch.MPRISName
		return effects
	}

	// Normal mode
	switch m.Kind {
	case midi.ButtonMute:
		effects.ledState = ch.toggleButton(m.Kind)
	case midi.ButtonSolo:
		effects.ledState = ch.toggleButton(m.Kind)
	case midi.ButtonRec:
		if ch.StreamID != nil && ch.advancedSpec != nil {
			// Activate advanced mode instead of toggling Rec.
			// Create and store the blink context while the lock is held to close
			// the TOCTOU window where OnGlobal could clear Advanced before we
			// store the cancel func.
			bCtx, cancel := context.WithCancel(ctx)
			ch.Advanced = true
			d.blinkGen[m.Channel]++
			effects.advancedToggled = true
			effects.advancedNowOn = true
			effects.advancedOldCancel = d.advancedCancels[m.Channel]
			d.advancedCancels[m.Channel] = cancel
			effects.advancedBCtx = bCtx
			effects.advancedGen = d.blinkGen[m.Channel]
		} else {
			effects.ledState = ch.toggleButton(m.Kind)
		}
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
	// Advanced mode toggle (R button activates or deactivates).
	if effects.advancedToggled {
		if effects.advancedOldCancel != nil {
			effects.advancedOldCancel()
		}
		if effects.advancedNowOn {
			// Cancel was stored in advancedCancels while under the lock; just start goroutine.
			go d.runAdvancedBlink(effects.advancedBCtx, m.Channel, effects.advancedGen)
		} else {
			// Deactivated: restore R LED. The blink goroutine also restores via restoreRLED
			// on ctx.Done(); this write is a fast-path to avoid the LED staying on briefly.
			if leds != nil {
				leds.SetButtonLED(m.Channel, midi.ButtonRec, effects.chRec || effects.chPinned)
			}
		}
		return
	}

	// Advanced mode action (Mute/Solo/Stop pressed while in advanced mode).
	if effects.isAdvancedAction {
		kindName := buttonKindName(m.Kind)
		log.Printf("advanced ch%d %s: action=%q", m.Channel, kindName, effects.advancedAction)
		return
	}

	// Normal mode: default LED update.
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
