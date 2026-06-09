# Package: `daemon`

## Purpose

Implements Unix domain socket transport communication using a newline-delimited JSON protocol. Translates client commands to the dispatcher and broadcasts status snapshots.

## Exported API

```go
package daemon

func SocketPath() string

type Client struct {
	// contains filtered or unexported fields
}

type InitialState struct {
	Snapshot      [8]dispatcher.Channel
	Streams       []streams.EnrichedStream
	Labels        [8]string
	ConfigPath    string // absolute path to the config file the daemon loaded
	DaemonVersion string // build version of the running daemon
}

func Connect() (*Client, InitialState, error)

func ConnectWithRetry(timeout time.Duration) (*Client, InitialState, error)

func (c *Client) SetProgram(p *tea.Program)

func (c *Client) Snapshot() [8]dispatcher.Channel

func (c *Client) Bind(ch int, id uint32, name string, kind audio.NodeKind, mprisName string, pid uint32)

func (c *Client) Unbind(ch int)

func (c *Client) ToggleMute(ch int)

func (c *Client) ToggleSolo(ch int)

func (c *Client) Run(ctx context.Context)

type Server struct {
	// contains filtered or unexported fields
}

func NewServer(disp *dispatcher.Dispatcher, labels [8]string, configPath string, version string) *Server

func (s *Server) Listen(ctx context.Context) error

func (s *Server) BroadcastSnapshot(snap [8]dispatcher.Channel)

func (s *Server) BroadcastStreams(ss []streams.EnrichedStream)

func (s *Server) BroadcastDevice(msg midi.DeviceStatusMsg)

func (s *Server) BroadcastGlobal(msg midi.GlobalMsg)
```

## Inbound Dependencies

- `cmd/smc-mixerd`
- `cmd/smc-mixer`

## Outbound Dependencies

- `audio`
- `dispatcher`
- `midi`
- `streams`

## Seams

- **`Client` / `ui.Dispatcher` Interface**: `Client` implements the interface consumed by the `ui` package to issue asynchronous backend controls over socket connection.
- **JSON Framing**: Packages messages into structured envelopes (`initial`, `snapshot`, `streams`, `device`, `global`, `bind`, `unbind`, `mute`, `solo`).

## Side Effects

- Creates, binds to, and listens on Unix domain sockets.
- Reads and writes bytes directly over network connection interfaces.
- Deletes stale socket file paths from the disk.
- Spawns goroutines to handle connected clients (`serveConn`).

## Package-level Invariants & Concurrency Assumptions

- `Client` synchronizes socket writes using `writeMu sync.Mutex` and caches snapshots with `mu sync.RWMutex`.
- `Server` manages list registration using `mu sync.RWMutex` and coordinates stream states using `streamsMu sync.RWMutex`.
- Connections (`serverConn`) protect raw writes via private mutex synchronization.
