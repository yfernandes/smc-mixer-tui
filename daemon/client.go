package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/midi"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

// Client connects to a running daemon and implements the ui.Dispatcher interface.
// The daemon pushes state changes; Bind/Unbind commands are forwarded over the socket.
type Client struct {
	conn    net.Conn
	scanner *bufio.Scanner
	prog    *tea.Program

	writeMu sync.Mutex
	mu      sync.RWMutex
	snap    [8]dispatcher.Channel
}

// InitialState holds the full state sent by the daemon on connection.
type InitialState struct {
	Snapshot [8]dispatcher.Channel
	Streams  []streams.EnrichedStream
}

// Connect dials the daemon socket and reads the initial state synchronously.
// Returns an error if the daemon is not running.
func Connect() (*Client, InitialState, error) {
	conn, err := net.Dial("unix", SocketPath())
	if err != nil {
		return nil, InitialState{}, fmt.Errorf("connect to daemon: %w", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	scanner := bufio.NewScanner(conn)
	var state InitialState

	if scanner.Scan() {
		var env envelope
		if err := json.Unmarshal(scanner.Bytes(), &env); err == nil && env.Kind == kindInitial {
			var p initialPayload
			if err := json.Unmarshal(env.Data, &p); err == nil {
				state.Snapshot = snapFromWire(p.Snapshot)
				state.Streams = p.Streams
			}
		}
	}

	conn.SetReadDeadline(time.Time{})

	c := &Client{
		conn:    conn,
		scanner: scanner,
		snap:    state.Snapshot,
	}
	return c, state, nil
}

// ConnectWithRetry dials the daemon socket, retrying until timeout elapses.
func ConnectWithRetry(timeout time.Duration) (*Client, InitialState, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		c, state, err := Connect()
		if err == nil {
			return c, state, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, InitialState{}, lastErr
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// SetProgram registers the Bubbletea program that receives non-snapshot pushes
// (streams, device status, global transport). Must be called before Run.
func (c *Client) SetProgram(p *tea.Program) {
	c.prog = p
}

// Snapshot returns the latest channel state received from the daemon.
// Implements ui.Dispatcher.
func (c *Client) Snapshot() [8]dispatcher.Channel {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snap
}

// Bind forwards a bind command to the daemon. Implements ui.Dispatcher.
func (c *Client) Bind(ch int, id uint32, name string, kind dispatcher.NodeKind, mprisName string) {
	c.send(kindBind, bindPayload{Ch: ch, ID: id, Name: name, Kind: kind, MPRISName: mprisName})
}

// Unbind forwards an unbind command to the daemon. Implements ui.Dispatcher.
func (c *Client) Unbind(ch int) {
	c.send(kindUnbind, unbindPayload{Ch: ch})
}

func (c *Client) send(kind msgKind, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	env := envelope{Kind: kind, Data: json.RawMessage(data)}
	frame, err := json.Marshal(env)
	if err != nil {
		return
	}
	frame = append(frame, '\n')
	c.writeMu.Lock()
	c.conn.Write(frame) //nolint:errcheck
	c.writeMu.Unlock()
}

// Run reads daemon push messages until ctx is cancelled or the connection drops.
// Snapshot updates go into the local cache; other messages are forwarded to the
// Bubbletea program via prog.Send.
func (c *Client) Run(ctx context.Context) {
	defer c.conn.Close()
	for c.scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		var env envelope
		if err := json.Unmarshal(c.scanner.Bytes(), &env); err != nil {
			log.Printf("client: decode: %v", err)
			continue
		}
		c.handlePush(env)
	}
}

func (c *Client) handlePush(env envelope) {
	switch env.Kind {
	case kindSnapshot:
		var w snapshotWire
		if err := json.Unmarshal(env.Data, &w); err != nil {
			log.Printf("client: snapshot: %v", err)
			return
		}
		snap := snapFromWire(w)
		c.mu.Lock()
		c.snap = snap
		c.mu.Unlock()

	case kindStreams:
		var ss []streams.EnrichedStream
		if err := json.Unmarshal(env.Data, &ss); err != nil {
			log.Printf("client: streams: %v", err)
			return
		}
		if c.prog != nil {
			c.prog.Send(streams.UpdateMsg(ss))
		}

	case kindDevice:
		var msg midi.DeviceStatusMsg
		if err := json.Unmarshal(env.Data, &msg); err != nil {
			log.Printf("client: device: %v", err)
			return
		}
		if c.prog != nil {
			c.prog.Send(msg)
		}

	case kindGlobal:
		var msg midi.GlobalMsg
		if err := json.Unmarshal(env.Data, &msg); err != nil {
			log.Printf("client: global: %v", err)
			return
		}
		if c.prog != nil {
			c.prog.Send(msg)
		}
	}
}
