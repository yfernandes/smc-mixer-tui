package dispatcher

import (
	"context"

	"github.com/yfernandes/smc-mixer-tui/midi"
)

// New creates a Dispatcher backed by pw. Knobs start at center (64).
func New(pw PipeWire) *Dispatcher {
	d := &Dispatcher{pw: pw}
	for i := range d.channels {
		d.channels[i] = newChannel()
	}
	return d
}

// SetMPRISCaller sets (or clears, if nil) the MPRIS playback controller.
func (d *Dispatcher) SetMPRISCaller(m MPRISCaller) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mpris = m
}

// Snapshot returns a copy of all channel states, safe for the TUI to read.
func (d *Dispatcher) Snapshot() [8]Channel {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.channels
}

// Run reads MIDI events from msgs until ctx is cancelled or msgs is closed.
func (d *Dispatcher) Run(ctx context.Context, msgs <-chan midi.Msg) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			d.dispatch(ctx, msg)
		}
	}
}

func (d *Dispatcher) dispatch(ctx context.Context, msg midi.Msg) {
	switch m := msg.(type) {
	case midi.FaderMsg:
		d.onFader(ctx, m)
	case midi.KnobMsg:
		d.onKnob(ctx, m)
	case midi.ButtonMsg:
		d.onButton(ctx, m)
	}
	// midi.GlobalMsg — transport, not handled here
}
