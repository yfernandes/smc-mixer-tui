package dispatcher

import (
	"context"
	"sync"

	"github.com/yago/smc-mixer/midi"
)

// PipeWire is the subset of pipewire.Client used by the dispatcher.
type PipeWire interface {
	SetVolume(ctx context.Context, id uint32, vol float64) error
	SetMute(ctx context.Context, id uint32, muted bool) error
}

// LEDWriter sends LED feedback to the MIDI device.
type LEDWriter interface {
	SetButtonLED(ch int, kind midi.ButtonKind, on bool)
	SetFaderLED(ch int, blink bool)
	SetGlobalLED(action midi.GlobalAction, on bool)
}

// Channel holds the runtime state for one mixer channel strip.
type Channel struct {
	StreamID *uint32 // nil if unbound
	Name     string  // display name; "" if unbound
	Volume   float64 // 0.0–1.0, last fader position
	Knob     int     // 0–127, accumulated relative position; starts at 64
	Mute     bool
	Solo     bool
	Rec      bool
	Stop     bool
}

// globalLEDActions lists the transport actions that have LEDs, in index order.
var globalLEDActions = [5]midi.GlobalAction{
	midi.ActionPlay,
	midi.ActionPause,
	midi.ActionRecord,
	midi.ActionPrevious,
	midi.ActionNext,
}

// Dispatcher maps MIDI events to PipeWire actions and maintains channel state.
type Dispatcher struct {
	pw         PipeWire
	leds       LEDWriter // nil when no device is connected
	channels   [8]Channel
	globalLEDs [5]bool // toggle state for transport buttons, indexed by globalLEDActions
	mu         sync.RWMutex
}

// New creates a Dispatcher backed by pw. Knobs start at center (64).
func New(pw PipeWire) *Dispatcher {
	d := &Dispatcher{pw: pw}
	for i := range d.channels {
		d.channels[i].Knob = 64
	}
	return d
}

// SetLEDWriter sets (or clears, if nil) the LED output device.
func (d *Dispatcher) SetLEDWriter(w LEDWriter) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.leds = w
}

// SyncLEDs pushes the full current LED state to the device. Call after connecting
// a new writer so the hardware reflects the in-memory state immediately.
func (d *Dispatcher) SyncLEDs() {
	d.mu.RLock()
	leds := d.leds
	chs := d.channels
	globals := d.globalLEDs
	d.mu.RUnlock()

	if leds == nil {
		return
	}
	for ch, c := range chs {
		leds.SetButtonLED(ch, midi.ButtonRec, c.Rec)
		leds.SetButtonLED(ch, midi.ButtonSolo, c.Solo)
		leds.SetButtonLED(ch, midi.ButtonMute, c.Mute)
		leds.SetButtonLED(ch, midi.ButtonStop, c.Stop)
		leds.SetFaderLED(ch, c.StreamID != nil)
	}
	for i, action := range globalLEDActions {
		leds.SetGlobalLED(action, globals[i])
	}
}

// OnGlobal toggles the LED for a transport button press.
func (d *Dispatcher) OnGlobal(m midi.GlobalMsg) {
	if !m.Pressed {
		return
	}
	var idx int
	found := false
	for i, a := range globalLEDActions {
		if a == m.Action {
			idx, found = i, true
			break
		}
	}
	if !found {
		return
	}

	d.mu.Lock()
	d.globalLEDs[idx] = !d.globalLEDs[idx]
	state := d.globalLEDs[idx]
	leds := d.leds
	d.mu.Unlock()

	if leds != nil {
		leds.SetGlobalLED(m.Action, state)
	}
}

// Bind assigns a PipeWire stream to a channel strip.
func (d *Dispatcher) Bind(ch int, id uint32, name string) {
	d.mu.Lock()
	d.channels[ch].StreamID = &id
	d.channels[ch].Name = name
	leds := d.leds
	d.mu.Unlock()

	if leds != nil {
		leds.SetFaderLED(ch, true)
	}
}

// Unbind removes the stream binding from a channel strip.
func (d *Dispatcher) Unbind(ch int) {
	d.mu.Lock()
	d.channels[ch].StreamID = nil
	d.channels[ch].Name = ""
	leds := d.leds
	d.mu.Unlock()

	if leds != nil {
		leds.SetFaderLED(ch, false)
	}
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
		d.onKnob(m)
	case midi.ButtonMsg:
		d.onButton(ctx, m)
	}
	// midi.GlobalMsg — transport, not handled here
}

func (d *Dispatcher) onFader(ctx context.Context, m midi.FaderMsg) {
	vol := float64(m.Value) / 127.0

	d.mu.Lock()
	d.channels[m.Channel].Volume = vol
	id := d.channels[m.Channel].StreamID // *uint32; each Bind allocates a new slot
	d.mu.Unlock()

	if id != nil {
		_ = d.pw.SetVolume(ctx, *id, vol)
	}
}

func (d *Dispatcher) onKnob(m midi.KnobMsg) {
	d.mu.Lock()
	defer d.mu.Unlock()
	k := d.channels[m.Channel].Knob + m.Delta
	if k < 0 {
		k = 0
	} else if k > 127 {
		k = 127
	}
	d.channels[m.Channel].Knob = k
}

func (d *Dispatcher) onButton(ctx context.Context, m midi.ButtonMsg) {
	if !m.Pressed {
		return
	}
	d.mu.Lock()
	ch := &d.channels[m.Channel]
	switch m.Kind {
	case midi.ButtonMute:
		ch.Mute = !ch.Mute
	case midi.ButtonSolo:
		ch.Solo = !ch.Solo
	case midi.ButtonRec:
		ch.Rec = !ch.Rec
	case midi.ButtonStop:
		ch.Stop = !ch.Stop
	}
	muted, id, leds := ch.Mute, ch.StreamID, d.leds
	var ledState bool
	switch m.Kind {
	case midi.ButtonMute:
		ledState = ch.Mute
	case midi.ButtonSolo:
		ledState = ch.Solo
	case midi.ButtonRec:
		ledState = ch.Rec
	case midi.ButtonStop:
		ledState = ch.Stop
	}
	d.mu.Unlock()

	if leds != nil {
		leds.SetButtonLED(m.Channel, m.Kind, ledState)
	}
	if m.Kind == midi.ButtonMute && id != nil {
		_ = d.pw.SetMute(ctx, *id, muted)
	}
}
