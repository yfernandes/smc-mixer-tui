# Package: `midi`

## Purpose
Accesses physical raw MIDI soundcard addresses, parses running status and real-time messages, and sets up virtual ALSA sequencer bridges.

## Exported API
```go
package midi

type ButtonKind uint8

const (
	ButtonRec  ButtonKind = iota // note 0–7
	ButtonSolo                   // note 8–15
	ButtonMute                   // note 16–23
	ButtonStop                   // note 24–31
)

type GlobalAction uint8

const (
	ActionPlay GlobalAction = iota
	ActionPause
	ActionRecord
	ActionPrevious
	ActionNext
	ActionSeekBack
	ActionSeekForward
	ActionUp
	ActionDown
	ActionLeft
	ActionRight
)

type Msg interface {
	midiMsg()
}

type ButtonMsg struct {
	Channel int // 0–7
	Kind    ButtonKind
	Pressed bool
}

type GlobalMsg struct {
	Action  GlobalAction
	Pressed bool
}

type FaderMsg struct {
	Channel int   // 0–7
	Value   uint8 // 0–127
}

type KnobMsg struct {
	Channel int // 0–7
	Delta   int // +1 or -1
}

type DeviceStatusMsg struct {
	Connected bool
	Device    string // set when Connected == true
}

func Classify(raw [3]byte) (Msg, bool)

type Writer struct {
	// contains filtered or unexported fields
}

func OpenWriter(path string) (*Writer, error)

func (w *Writer) ClearLEDs()

func (w *Writer) Close() error

func (w *Writer) SetButtonLED(ch int, kind ButtonKind, on bool)

func (w *Writer) SetFaderLED(ch int, blink bool)

func (w *Writer) SetFaderPosition(ch int, vol float64)

func (w *Writer) SetGlobalLED(action GlobalAction, on bool)

type Listener struct {
	// contains filtered or unexported fields
}

func NewListener(device string) *Listener

func (l *Listener) Run(ctx context.Context, out chan<- Msg) error

type SeqPort struct {
	Client int
	Port   int
}

type Bridge struct {
	DevPath string
	// contains filtered or unexported fields
}

func (b *Bridge) Close()

func BridgeSequencerPort(nameHint string) (*Bridge, error)

func FindDevice(nameHint string) (string, error)
```

## Inbound Dependencies
- `daemon`
- `dispatcher`
- `ui`
- `cmd/smc-mixerd`

## Outbound Dependencies
None

## Seams
- **`Listener.Run`**: Accepts a channel `chan<- Msg` to stream parsed and structured MIDI events.
- **`Writer`**: Exposes low-level motorized fader and LED control APIs. (Note: The M-Vave SMC-Mixer does not have motorized faders, so the physical fader position is not modified by writing to `SetFaderPosition`).
- **`Bridge`**: Connects ALSA sequencer ports to raw MIDI paths.

## Side Effects
- `FindDevice` reads card files from `/proc/asound/cards` and walks directories under `/dev/snd/`.
- `Listener` opens file descriptors and issues non-blocking syscalls (`syscall.Open`, `syscall.Read`) to flush raw buffers.
- `Writer` writes byte frames to character device files.
- `Bridge` executes command processes: `aconnect` and `modprobe`.

## Package-level Invariants & Concurrency Assumptions
- `Writer` enforces thread-safe writes using `mu sync.Mutex` on files.
