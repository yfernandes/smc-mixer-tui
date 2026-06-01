package dispatcher

import (
	"context"
	"log"
	"math"
	"sync"

	"github.com/yfernandes/smc-mixer-tui/midi"
)

// pickupThreshold is the fader-to-actual-volume tolerance for sync detection
// and external-change detection (≈2 MIDI steps out of 127).
const pickupThreshold = 2.0 / 127.0

// NodeKind classifies an audio stream's functional role.
type NodeKind uint8

const (
	KindSource NodeKind = iota // app playing audio
	KindMic                    // microphone / capture device
	KindSink                   // output device / speakers
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
	SetFaderPosition(ch int, vol float64)
	SetGlobalLED(action midi.GlobalAction, on bool)
}

// MPRISCaller toggles playback for a named MPRIS media player.
type MPRISCaller interface {
	PlayPause(ctx context.Context, playerName string) error
}

// Channel holds the runtime state for one mixer channel strip.
type Channel struct {
	StreamID     *uint32  // nil if unbound
	Name         string   // display name; "" if unbound
	Kind         NodeKind // functional role; set on Bind
	MPRISName    string   // MPRIS player name suffix; "" if not an MPRIS source
	ActualVolume float64  // volume reported by PipeWire — the source of truth
	FaderPos     float64  // physical hardware fader position, 0.0–1.0
	LastSetVol   float64  // last volume we sent to PipeWire; -1 = never
	Synced       bool     // fader has picked up ActualVolume; controls PipeWire when true
	Knob         int      // 0–127, accumulated relative position; starts at 64
	Mute         bool     // user-set mute
	SoloMuted    bool     // muted as a side-effect of another same-kind channel being soloed
	Solo         bool
	Rec          bool
	Stop         bool
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
	leds       LEDWriter   // nil when no device is connected
	mpris      MPRISCaller // nil when MPRIS unavailable
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

// SetMPRISCaller sets (or clears, if nil) the MPRIS playback controller.
func (d *Dispatcher) SetMPRISCaller(m MPRISCaller) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mpris = m
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
		leds.SetButtonLED(ch, midi.ButtonMute, c.Mute || c.SoloMuted)
		leds.SetButtonLED(ch, midi.ButtonStop, c.Stop)
		// Fader LED blinks when bound AND synced; off when unbound or awaiting pickup.
		leds.SetFaderLED(ch, c.StreamID != nil && c.Synced)
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
// If the stream is already bound to a different channel, that channel is
// unbound first — a stream may only be controlled by one channel at a time.
// The new channel starts unsynced: the fader must reach the actual volume
// before it takes control of PipeWire.
func (d *Dispatcher) Bind(ch int, id uint32, name string, kind NodeKind, mprisName string) {
	d.mu.Lock()
	// Release any other channel already holding this stream.
	var evicted []int
	for i := range d.channels {
		if i != ch && d.channels[i].StreamID != nil && *d.channels[i].StreamID == id {
			d.channels[i].StreamID = nil
			d.channels[i].Name = ""
			d.channels[i].Synced = false
			evicted = append(evicted, i)
		}
	}
	d.channels[ch].StreamID = &id
	d.channels[ch].Name = name
	d.channels[ch].Kind = kind
	d.channels[ch].MPRISName = mprisName
	d.channels[ch].Synced = false
	d.channels[ch].ActualVolume = 0
	d.channels[ch].LastSetVol = -1
	leds := d.leds
	d.mu.Unlock()

	if leds != nil {
		for _, i := range evicted {
			leds.SetFaderLED(i, false)
		}
		leds.SetFaderLED(ch, false) // off until fader picks up
	}
}

// Unbind removes the stream binding from a channel strip.
func (d *Dispatcher) Unbind(ch int) {
	d.mu.Lock()
	d.channels[ch].StreamID = nil
	d.channels[ch].Name = ""
	d.channels[ch].Synced = false
	leds := d.leds
	d.mu.Unlock()

	if leds != nil {
		leds.SetFaderLED(ch, false)
	}
}

// UpdatePlaybackStatus syncs the Stop button LED to the actual MPRIS playback
// state. playing=true means the stream is actively playing (LED on).
func (d *Dispatcher) UpdatePlaybackStatus(ch int, playing bool) {
	d.mu.Lock()
	d.channels[ch].Stop = playing
	leds := d.leds
	d.mu.Unlock()
	if leds != nil {
		leds.SetButtonLED(ch, midi.ButtonStop, playing)
	}
}

// syncFaderCap is the maximum position we park the fader at when desyncing.
const syncFaderCap = 0.25

// UpdateActualVolume records the PipeWire-reported volume for a channel.
// If the volume differs significantly from the last value we set, the channel
// is desynced so the user must pick up the fader again. On desync the fader
// is moved to min(actualVol, syncFaderCap) so the user has a low reference
// point to pick up from.
func (d *Dispatcher) UpdateActualVolume(ch int, vol float64) {
	d.mu.Lock()
	c := &d.channels[ch]
	c.ActualVolume = vol
	wasSync := c.Synced
	if c.Synced && math.Abs(vol-c.LastSetVol) > pickupThreshold {
		c.Synced = false
	}
	justDesynced := wasSync && !c.Synced
	bound, synced, leds := c.StreamID != nil, c.Synced, d.leds
	d.mu.Unlock()

	if leds == nil {
		return
	}
	leds.SetFaderLED(ch, bound && synced)
	if justDesynced {
		target := vol
		if target > syncFaderCap {
			target = syncFaderCap
		}
		leds.SetFaderPosition(ch, target)
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
	faderPos := float64(m.Value) / 127.0

	d.mu.Lock()
	c := &d.channels[m.Channel]
	c.FaderPos = faderPos
	if !c.Synced && math.Abs(faderPos-c.ActualVolume) < pickupThreshold {
		c.Synced = true
	}
	synced, id, leds := c.Synced, c.StreamID, d.leds
	if synced {
		c.LastSetVol = faderPos
	}
	d.mu.Unlock()

	if leds != nil {
		leds.SetFaderLED(m.Channel, id != nil && synced)
	}

	if !synced {
		return // fader has not yet picked up the actual volume
	}
	if id == nil {
		return
	}
	if err := d.pw.SetVolume(ctx, *id, faderPos); err != nil {
		log.Printf("fader ch%d: SetVolume(%d, %.3f): %v", m.Channel, *id, faderPos, err)
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

	type soloUpdate struct {
		ch    int
		id    uint32
		bound bool
		mute  bool
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

	var ledState bool
	switch m.Kind {
	case midi.ButtonMute:
		ledState = ch.Mute || ch.SoloMuted
	case midi.ButtonSolo:
		ledState = ch.Solo
	case midi.ButtonRec:
		ledState = ch.Rec
	case midi.ButtonStop:
		ledState = ch.Stop
	}

	// Capture per-channel state for post-lock PipeWire / MPRIS calls.
	chBound := ch.StreamID != nil
	var chID uint32
	if chBound {
		chID = *ch.StreamID
	}
	chMuteEffective := ch.Mute || ch.SoloMuted
	chKind := ch.Kind
	chMPRIS := ch.MPRISName

	// Solo: recompute SoloMuted for every channel in the same kind group.
	var soloUpdates []soloUpdate
	if m.Kind == midi.ButtonSolo {
		anySoloed := false
		for i := range d.channels {
			if d.channels[i].Kind == chKind && d.channels[i].Solo {
				anySoloed = true
				break
			}
		}
		for i := range d.channels {
			if d.channels[i].StreamID == nil || d.channels[i].Kind != chKind {
				continue
			}
			d.channels[i].SoloMuted = anySoloed && !d.channels[i].Solo
			u := soloUpdate{
				ch:   i,
				mute: d.channels[i].Mute || d.channels[i].SoloMuted,
			}
			if d.channels[i].StreamID != nil {
				u.id = *d.channels[i].StreamID
				u.bound = true
			}
			soloUpdates = append(soloUpdates, u)
		}
	}

	leds := d.leds
	mpris := d.mpris
	d.mu.Unlock()

	if leds != nil {
		leds.SetButtonLED(m.Channel, m.Kind, ledState)
	}

	switch m.Kind {
	case midi.ButtonMute:
		if chBound {
			if err := d.pw.SetMute(ctx, chID, chMuteEffective); err != nil {
				log.Printf("button ch%d mute: SetMute(%d, %v): %v", m.Channel, chID, chMuteEffective, err)
			}
		}
	case midi.ButtonSolo:
		for _, u := range soloUpdates {
			if !u.bound {
				continue
			}
			if err := d.pw.SetMute(ctx, u.id, u.mute); err != nil {
				log.Printf("solo ch%d: SetMute(%d, %v): %v", u.ch, u.id, u.mute, err)
			}
		}
		if leds != nil {
			d.mu.RLock()
			for i, c := range d.channels {
				if c.Kind == chKind {
					leds.SetButtonLED(i, midi.ButtonMute, c.Mute || c.SoloMuted)
					leds.SetButtonLED(i, midi.ButtonSolo, c.Solo)
				}
			}
			d.mu.RUnlock()
		}
	case midi.ButtonStop:
		if chMPRIS != "" && mpris != nil {
			if err := mpris.PlayPause(ctx, chMPRIS); err != nil {
				log.Printf("button ch%d stop: PlayPause(%s): %v", m.Channel, chMPRIS, err)
			}
		}
	}
}
