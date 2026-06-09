# Package: `streams`

## Purpose

Discovers active stream nodes and enriches them with process trees, active Hyprland focused windows, and D-Bus MPRIS player tracks.

## Exported API

```go
package streams

type Source uint8

const (
	SourcePipeWire Source = iota // app.name / node.name from pw-dump
	SourceHyprland               // class from hyprctl clients
	SourceMPRIS                  // player name from DBus MPRIS
)

type EnrichedStream struct {
	ID          uint32 // PipeWire node ID
	PID         uint32 // OS process ID; 0 if unavailable
	Name        string // best display name (may be overwritten by Hyprland/MPRIS enrichment)
	AppName     string // original PipeWire application.name, before enrichment
	NodeName    string // PipeWire node.name (stable, used for pactl sink addressing)
	BindKey     string // stable key for config matching (MPRIS name or app.name)
	Source      Source
	Kind        audio.NodeKind // functional role: source app, microphone, or output sink
	MPRISPlayer string         // MPRIS player name (suffix after "org.mpris.MediaPlayer2.")
	Track       string         // MPRIS: current track title
	Artist      string         // MPRIS: first listed artist
	WinTitle    string         // Hyprland: window title of the owning process
	MediaName   string         // PipeWire: media.name (e.g. YouTube video title)
}

type UpdateMsg []EnrichedStream

type Enricher struct {
	// contains filtered or unexported fields
}

func New(pw *pipewire.Client) *Enricher

func (e *Enricher) SetBlacklist(names []string)

func (e *Enricher) Enrich(ctx context.Context) ([]EnrichedStream, error)

func (e *Enricher) Poll(ctx context.Context, interval time.Duration, send func(UpdateMsg))

type Controller struct{}

func NewController() *Controller

func (c *Controller) PlayPause(ctx context.Context, playerName string) error

func IsPlaying(ctx context.Context, playerName string) bool
```

## Inbound Dependencies

- `daemon`
- `cmd/smc-mixerd`

## Outbound Dependencies

- `audio`
- `pipewire`

## Seams

- **`Poll` Callback**: Accepts a `send func(UpdateMsg)` callback function to pipe periodically fetched results.
- **`Controller`**: Implements `dispatcher.MPRISCaller` via D-Bus session controls.

## Side Effects

- Spawns `hyprctl clients -j` subprocesses to fetch window layouts.
- Opens system-level D-Bus session socket connections.
- Reads `/proc/<pid>/status` files to walk parent process lineages.
- Sets up ticker loops during calls to `Poll`.

## Package-level Invariants & Concurrency Assumptions

- Safe for concurrent invocation (caches no shared mutable state).
