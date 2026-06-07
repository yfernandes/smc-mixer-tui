package dispatcher

import (
	"context"
	"time"

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
