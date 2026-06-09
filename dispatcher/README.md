# Package: `dispatcher`

## Purpose

Contains the core state machine mapping physical controls (knobs, faders, buttons) to functional PipeWire and MPRIS actions. Handles fader pickup calibration and LED lighting states.

## Exported API

```go
package dispatcher

const PickupThreshold = 2.0 / 127.0

type PipeWire interface {
	SetVolume(ctx context.Context, id uint32, vol float64) error
	SetMute(ctx context.Context, id uint32, muted bool) error
}

type CrossfaderController interface {
	SetGains(ctx context.Context, volA, volB float64) error
}

type LEDWriter interface {
	SetButtonLED(ch int, kind midi.ButtonKind, on bool)
	SetFaderLED(ch int, blink bool)
	SetFaderPosition(ch int, vol float64)
	SetGlobalLED(action midi.GlobalAction, on bool)
}

type MPRISCaller interface {
	PlayPause(ctx context.Context, playerName string) error
}

type Channel struct {
	StreamID        *uint32        // nil if unbound
	BoundPID        uint32         // OS PID of the last user-bound stream; 0 if none.
	ManuallyUnbound bool           // user explicitly unbound this channel; suppresses config auto-rebind
	UserBound       bool           // user explicitly bound this channel; suppresses config rebind while stream is live
	Name            string         // display name; "" if unbound
	Kind            audio.NodeKind // functional role; set on Bind
	MPRISName       string         // MPRIS player name suffix; "" if not an MPRIS source
	ActualVolume    float64        // volume last reported by PipeWire; display only
	FaderPos        float64        // physical hardware fader position, 0.0–1.0
	LastSetVol      float64        // last volume we sent to PipeWire; -1 = never
	Synced          bool           // fader has passed through zero and now controls PipeWire
	Knob            int            // 0–127, accumulated relative position; starts at 64
	Mute            bool           // user-set mute
	SoloMuted       bool           // muted as a side-effect of another same-kind channel being soloed
	Solo            bool
	Rec             bool
	Stop            bool
	Advanced        bool // advanced mode is active; R LED blinks, controls remapped
	Pinned          bool // channel is pinned to the main page; R LED solid on

	KnobStreamID *uint32

	CrossSinkAName string // display name for SinkA
	CrossSinkBName string // display name for SinkB
}

type Dispatcher struct {
	// contains filtered or unexported fields
}

func New(pw PipeWire) *Dispatcher

func (d *Dispatcher) SetVolDebounce(delay time.Duration)

func (d *Dispatcher) SetMPRISCaller(m MPRISCaller)

func (d *Dispatcher) Snapshot() [8]Channel

func (d *Dispatcher) SetPageChangeCallback(cb func())

func (d *Dispatcher) Run(ctx context.Context, msgs <-chan midi.Msg)

func (d *Dispatcher) ToggleMute(ch int)

func (d *Dispatcher) ToggleSolo(ch int)

func (d *Dispatcher) Bind(ch int, id uint32, name string, kind audio.NodeKind, mprisName string)

func (d *Dispatcher) UserBind(ch int, id uint32, name string, kind audio.NodeKind, mprisName string, pid uint32)

func (d *Dispatcher) UpdateBindingMetadata(ch int, id uint32, name, mprisName string)

func (d *Dispatcher) LoseBinding(ch int)

func (d *Dispatcher) ResetStrip(ch int)

func (d *Dispatcher) BindKnob(ch int, id uint32) bool

func (d *Dispatcher) SetKnob(ch int, val int)

func (d *Dispatcher) LoseKnob(ch int)

func (d *Dispatcher) Unbind(ch int)

func (d *Dispatcher) UpdateActualVolume(ch int, vol float64)

func (d *Dispatcher) SetCrossfader(ch int, ctrl CrossfaderController, nameA, nameB string)

func (d *Dispatcher) SetLEDWriter(w LEDWriter)

func (d *Dispatcher) SyncLEDs()

func (d *Dispatcher) OnGlobal(m midi.GlobalMsg)

func (d *Dispatcher) ActivePage() string

func (d *Dispatcher) UpdatePlaybackStatus(ch int, playing bool)

type AdvancedSpec struct {
	FaderEffect      string
	KnobEffect       string
	MuteButtonAction string
	SoloButtonAction string
	StopButtonAction string
}

func (d *Dispatcher) SetAdvancedSpec(ch int, spec *AdvancedSpec)

func (d *Dispatcher) SetPinCallback(cb func(ch int))

func (d *Dispatcher) SetPinned(ch int, pinned bool)
```

## Inbound Dependencies

- `daemon`
- `cmd/smc-mixerd`

## Outbound Dependencies

- `audio`
- `midi`

## Seams

- **Injectable Interfaces (`PipeWire`, `LEDWriter`, `MPRISCaller`, `CrossfaderController`)**: Caller isolates system PipeWire operations and DBus MPRIS callers.
- **`Run(msgs <-chan midi.Msg)`**: Acts as the entrypoint for executing parsed controller commands.
- **`Snapshot()`**: Exposes current state maps safely without exposing private controllers.

## Side Effects

- Spawns debounced volume write goroutines (`runVolWorker`, `runKnobVolWorker`, `runCrossWorker`) if `volDebounce > 0` is set.
- Spawns asynchronous blinking goroutines (`runAdvancedBlink`) when entering Advanced mode.

## Package-level Invariants & Concurrency Assumptions

- Access to configurations, listener events, and channel maps is protected by `mu sync.RWMutex`.
- An atomic boolean check (`runStarted`) guards against starting concurrent PipeWire workers.
- Generation checks (`blinkGen [8]uint32`) ensure blink routines exit safely without clobbering updated LED states.
