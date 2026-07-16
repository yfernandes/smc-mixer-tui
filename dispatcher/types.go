package dispatcher

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/midi"
)

// PickupThreshold is the default fader-to-actual-volume tolerance for soft pickup (~1.6% of full travel).
// Expressed as a normalized fraction so it is independent of MIDI bit depth.
const PickupThreshold = 2.0 / 127.0

// SyncMode determines how a channel fader re-establishes control after being unsynced.
type SyncMode uint8

const (
	// SyncModeZero is the default: fader must cross 0 before controlling volume.
	SyncModeZero SyncMode = 0
	// SyncModeSoftPickup requires the fader to reach the current actual volume within tolerance.
	SyncModeSoftPickup SyncMode = 1
)

// PipeWire is the subset of pipewire.Client used by the dispatcher.
type PipeWire interface {
	SetVolume(ctx context.Context, id uint32, vol float64) error
	SetMute(ctx context.Context, id uint32, muted bool) error
}

// LEDWriter sends LED feedback to the MIDI device.
// Fader channels (0xE0–0xE7) are excluded: pitch-bend messages on those channels
// move the motorized faders and trigger the sync LED; only SetFaderPosition may
// write to them, and only when a deliberate position change is intended.
type LEDWriter interface {
	SetButtonLED(ch int, kind midi.ButtonKind, on bool)
	SetFaderPosition(ch int, vol float64)
	SetGlobalLED(action midi.GlobalAction, on bool)
}

// MPRISCaller toggles playback for a named MPRIS media player.
type MPRISCaller interface {
	PlayPause(ctx context.Context, playerName string) error
}

// Channel holds the runtime state for one mixer channel strip.
type Channel struct {
	StreamID        *uint32        // nil if unbound
	BoundPID        uint32         // OS PID of the last user-bound stream; 0 if none. Survives stream death so the next stream from the same process is reattached.
	BoundMediaName  string         // media.name of the last user-bound stream; used to prefer the same tab on PID-based reconnect.
	ManuallyUnbound bool           // user explicitly unbound this channel; suppresses config auto-rebind
	UserBound       bool           // user explicitly bound this channel; suppresses config rebind while stream is live
	Name            string         // display name; "" if unbound
	Kind            audio.NodeKind // functional role; set on Bind
	MPRISName       string         // MPRIS player name suffix; "" if not an MPRIS source

	// FaderPos and ActualVolume are intentionally independent:
	// FaderPos is the raw hardware position from MIDI CC, updated on every message,
	// never throttled or adjusted by software. FaderPosKnown is false until the first
	// CC arrives so the UI can distinguish "at zero" from "not yet received".
	// ActualVolume is the PipeWire-reported volume — the authoritative application
	// target and the pickup reference. They diverge whenever the fader hasn't yet
	// established sync (Synced=false), and converge once pickup is complete.
	FaderPos      float64 // physical hardware fader position, 0.0–1.0; see FaderPosKnown
	FaderPosKnown bool    // true once the first MIDI CC has been received for this channel
	ActualVolume  float64 // volume last reported by PipeWire; the APP-side authority and pickup target

	LastSetVol float64  // last volume we sent to PipeWire; -1 = never
	Synced     bool     // fader has established sync and now controls PipeWire
	SyncMode   SyncMode // how this channel establishes fader sync; set at bind time from config
	Knob       int      // 0–127, accumulated relative position; starts at 64
	Mute       bool     // user-set mute
	SoloMuted  bool     // muted as a side-effect of another same-kind channel being soloed
	Solo       bool
	Rec        bool
	Stop       bool
	Advanced   bool // advanced mode is active; R LED blinks, controls remapped
	Pinned     bool // channel is pinned to the main page; R LED solid on

	// pickupTol is the soft pickup tolerance (0 → use PickupThreshold).
	// pickupSide records which side of ActualVolume the fader was on when sync was last lost:
	//   -1 = below target, +1 = above target, 0 = unset (re-evaluated on next fader message).
	// prevFaderPos is the fader position from the previous moveFader call, used by soft
	// pickup to detect crossings and handle fast sweeps.
	pickupTol    float64
	pickupSide   int8
	prevFaderPos float64

	// KnobStreamID is the PipeWire node ID that this channel's knob controls for
	// gain writes (main page independent knob slots). nil means knob is either a
	// crossfader or unassigned.
	KnobStreamID   *uint32
	CrossSinkAName string // legacy wire/display field; crossfader ownership is in pwbackend
	CrossSinkBName string // legacy wire/display field; crossfader ownership is in pwbackend

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
	pw                 PipeWire
	leds               LEDWriter   // nil when no device is connected
	mpris              MPRISCaller // nil when MPRIS unavailable
	channels           [8]Channel
	globalLEDs         [5]bool // LED state for page buttons, indexed by globalLEDActions
	activePage         string  // current page name; "main" means no page button is lit
	lastStopAt         [8]time.Time
	rPressedAt         [8]time.Time
	pinCallback        func(ch int)
	pageChangeCallback func() // called outside the lock when activePage changes
	mu                 sync.RWMutex

	// advancedCancels holds the cancel function for each channel's blink goroutine.
	// nil means no blink goroutine is running for that channel.
	advancedCancels [8]context.CancelFunc

	// blinkGen is incremented each time a blink goroutine is started for a channel.
	// The goroutine captures its generation at start and exits without writing LEDs
	// if the generation has changed, preventing stale goroutines from clobbering LED state.
	blinkGen [8]uint32

	// Async PipeWire write workers. Each channel has a size-1 latest-value buffer.
	// When volThrottle == 0 (default / tests), writes are synchronous and workers are unused.
	// Run is single-use: calling it a second time logs an error and returns immediately.
	volThrottle    time.Duration
	volWorkers     [8]chan float64
	knobVolWorkers [8]chan float64
	runStarted     atomic.Bool // CAS from false→true on first Run; second call logs and returns
}
