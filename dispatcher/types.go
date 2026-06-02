package dispatcher

import (
	"context"
	"sync"
	"time"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/midi"
)

type crossGains = [2]float64

// PickupThreshold is the fader-to-actual-volume tolerance for sync detection (≈2 MIDI steps out of 127).
const PickupThreshold = 2.0 / 127.0

// PipeWire is the subset of pipewire.Client used by the dispatcher.
type PipeWire interface {
	SetVolume(ctx context.Context, id uint32, vol float64) error
	SetMute(ctx context.Context, id uint32, muted bool) error
}

// CrossfaderController applies independent per-sink gains for a crossfader channel.
// volA and volB are 0.0–1.0 and do not touch the global sink volumes.
type CrossfaderController interface {
	SetGains(ctx context.Context, volA, volB float64) error
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
	StreamID     *uint32        // nil if unbound
	Name         string         // display name; "" if unbound
	Kind         audio.NodeKind // functional role; set on Bind
	MPRISName    string         // MPRIS player name suffix; "" if not an MPRIS source
	ActualVolume float64        // volume last reported by PipeWire; display only
	FaderPos     float64        // physical hardware fader position, 0.0–1.0
	LastSetVol   float64        // last volume we sent to PipeWire; -1 = never
	Synced       bool           // fader has passed through zero and now controls PipeWire
	Knob         int            // 0–127, accumulated relative position; starts at 64
	Mute         bool           // user-set mute
	SoloMuted    bool           // muted as a side-effect of another same-kind channel being soloed
	Solo         bool
	Rec          bool
	Stop         bool

	// Crossfader: when set, the knob routes audio between two output sinks.
	// Knob 0 = only SinkA, knob 127 = only SinkB, knob 64 = both at full.
	// crossfader is unexported so Snapshot copies work without exposing internals.
	crossfader     CrossfaderController
	CrossSinkAName string // display name for SinkA
	CrossSinkBName string // display name for SinkB
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
	lastStopAt [8]time.Time
	mu         sync.RWMutex

	// Async PipeWire write workers. Each channel has a size-1 latest-value buffer.
	// When volDebounce == 0 (default / tests), writes are synchronous and workers are unused.
	volDebounce  time.Duration
	volWorkers   [8]chan float64
	crossWorkers [8]chan crossGains
}
