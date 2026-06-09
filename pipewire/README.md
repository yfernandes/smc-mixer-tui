# Package: `pipewire`

## Purpose

Interacts with the local PipeWire audio graph. Commands volume adjustments and builds pulse-loopback routes to implement hardware crossfading.

## Exported API

```go
package pipewire

type Stream struct {
	ID        uint32
	Name      string         // application.name → node.description → node.name → "stream-<id>"
	NodeName  string         // node.name (stable PW/pactl-addressable name, e.g. alsa_output.pci-...)
	MediaName string         // media.name (e.g. YouTube video title, track name)
	PID       uint32         // application.process.id; 0 if absent
	Kind      audio.NodeKind // functional role of the node
}

type Client struct {
	// contains filtered or unexported fields
}

func New() *Client

func (c *Client) ListStreams(ctx context.Context) ([]Stream, error) {

func (c *Client) SetVolume(ctx context.Context, id uint32, vol float64) error

func (c *Client) SetMute(ctx context.Context, id uint32, muted bool) error

func (c *Client) GetVolume(ctx context.Context, id uint32) (float64, bool, error)

type SinkInput struct {
	Index       uint32
	OwnerModule uint32 // pactl module ID; 0xFFFFFFFF = no owner
	NodeID      uint32 // PipeWire node.id from Properties; may be 0 for native PW streams
	NodeName    string // node.name from Properties; reliable fallback when NodeID is absent
}

func (c *Client) ListSinkInputs(ctx context.Context) ([]SinkInput, error)

func (c *Client) LoadModule(ctx context.Context, name, args string) (uint32, error)

func (c *Client) UnloadModule(ctx context.Context, id uint32) error

func (c *Client) MoveSinkInput(ctx context.Context, si uint32, sinkName string) error

func (c *Client) SetSinkInputVolume(ctx context.Context, si uint32, vol float64) error

func (c *Client) SetSinkVolume(ctx context.Context, sinkName string, vol float64) error

func (c *Client) RouteStreamToSink(ctx context.Context, streamNodeID, sinkNodeID uint32) error

func (c *Client) ClearStreamRoute(ctx context.Context, streamNodeID uint32) error

type CrossfaderRouting struct {
	NullSinkModule uint32 // pactl module ID for the main null sink
	GainAModule    uint32 // pactl module ID for gain-stage null sink A
	GainBModule    uint32 // pactl module ID for gain-stage null sink B
	LoopAModule    uint32 // pactl module ID for loopback: NullSink.monitor → GainA
	LoopBModule    uint32 // pactl module ID for loopback: NullSink.monitor → GainB
	Loop2AModule   uint32 // pactl module ID for loopback: GainA.monitor → SinkA
	Loop2BModule   uint32 // pactl module ID for loopback: GainB.monitor → SinkB
	StreamSI       uint32 // pactl sink-input index for the stream (for teardown)
	NullSinkName   string // e.g. "smc_ch0_void"
	GainAName      string // e.g. "smc_ch0_gain_a"
	GainBName      string // e.g. "smc_ch0_gain_b"
	StreamNodeID   uint32 // PipeWire stream node ID; used to mute/unmute around routing changes
}

func (c *Client) SetupCrossfader(ctx context.Context, tag string, streamNodeID uint32, streamNodeName, sinkANodeName, sinkBNodeName string) (*CrossfaderRouting, error)

func (c *Client) SetCrossfaderGains(ctx context.Context, r *CrossfaderRouting, volA, volB float64) error

func (c *Client) TeardownCrossfader(ctx context.Context, r *CrossfaderRouting)

type PulseModule struct {
	ID   uint32
	Name string
	Args string
}

func (c *Client) ListModules(ctx context.Context) ([]PulseModule, error)

func (c *Client) CleanupCrossfaderTag(ctx context.Context, tag string) error
```

## Inbound Dependencies

- `streams`
- `cmd/smc-mixerd`

## Outbound Dependencies

- `audio`

## Seams

- **Exec Injection**: An unexported `exec` function pointer inside `Client` allows callers to override CLI call executions, facilitating integration testing.
- **Crossfader Setup Contract**: `SetupCrossfader` takes a unique channel tag and targets, returning a mapped module routing configuration (`CrossfaderRouting`).

## Side Effects

- Spawns subprocesses to interface with the audio server: `pw-dump`, `wpctl`, `pactl`, `pw-metadata`.
- Blocks thread operations temporarily using sleeps during modules loading/teardown sequence.

## Package-level Invariants & Concurrency Assumptions

- No internal mutex locks. Assumes callers structure external concurrency safely.
