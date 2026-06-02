package dispatcher

import (
	"context"
	"time"

	"github.com/yfernandes/smc-mixer-tui/midi"
)

// New creates a Dispatcher backed by pw. Knobs start at center (64).
// By default PipeWire writes are synchronous (volDebounce == 0); call
// SetVolDebounce before Run to enable debounced async writes.
func New(pw PipeWire) *Dispatcher {
	d := &Dispatcher{pw: pw}
	for i := range d.channels {
		d.channels[i] = newChannel()
	}
	for i := range d.volWorkers {
		d.volWorkers[i] = make(chan float64, 1)
		d.crossWorkers[i] = make(chan crossGains, 1)
	}
	return d
}

// SetVolDebounce sets the debounce delay for PipeWire volume writes.
// Must be called before Run. Production code uses 200ms; tests leave it at 0
// (synchronous) to avoid goroutine timing issues.
func (d *Dispatcher) SetVolDebounce(delay time.Duration) {
	d.volDebounce = delay
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
// If volDebounce > 0, per-channel PipeWire worker goroutines are started and
// cancelled automatically when Run returns.
func (d *Dispatcher) Run(ctx context.Context, msgs <-chan midi.Msg) {
	if d.volDebounce > 0 {
		workerCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		for i := range d.volWorkers {
			go d.runVolWorker(workerCtx, i)
			go d.runCrossWorker(workerCtx, i)
		}
	}
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
