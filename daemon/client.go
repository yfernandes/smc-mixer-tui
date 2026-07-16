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
	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/midi"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

const initialReadTimeout = 5 * time.Second

// Client connects to a running daemon and implements the ui.Dispatcher interface.
// The daemon pushes state changes; Bind/Unbind commands are forwarded over the socket.
type Client struct {
	conn    net.Conn
	scanner *bufio.Scanner
	prog    *tea.Program

	writeMu sync.Mutex
	mu      sync.RWMutex
	snap    [8]dispatcher.Channel
	strips  []StripWire
	page    PageWire
}

// InitialState holds the full state sent by the daemon on connection.
type InitialState struct {
	Snapshot      [8]dispatcher.Channel
	Strips        []StripWire
	RouterPage    PageWire
	Streams       []streams.EnrichedStream
	Labels        [8]string
	ConfigPath    string // absolute path to the config file the daemon loaded
	DaemonVersion string // build version of the running daemon
}

// Connect dials the daemon socket and reads the initial state synchronously.
// Returns an error if the daemon is not running.
func Connect() (*Client, InitialState, error) {
	conn, err := net.Dial("unix", SocketPath())
	if err != nil {
		return nil, InitialState{}, fmt.Errorf("connect to daemon: %w", err)
	}

	conn.SetReadDeadline(time.Now().Add(initialReadTimeout))

	scanner := bufio.NewScanner(conn)
	state, err := readInitialState(scanner)
	if err != nil {
		conn.Close()
		return nil, InitialState{}, err
	}

	conn.SetReadDeadline(time.Time{})

	c := &Client{
		conn:    conn,
		scanner: scanner,
		snap:    state.Snapshot,
		strips:  cloneStrips(state.Strips),
		page:    clonePage(state.RouterPage),
	}
	return c, state, nil
}

func readInitialState(scanner *bufio.Scanner) (InitialState, error) {
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return InitialState{}, fmt.Errorf("read initial state: %w", err)
		}
		return InitialState{}, fmt.Errorf("read initial state: connection closed")
	}

	env, err := decodeEnvelope(scanner.Bytes())
	if err != nil {
		return InitialState{}, err
	}
	if env.Kind != kindInitial {
		return InitialState{}, fmt.Errorf("read initial state: got %q frame", env.Kind)
	}

	var p initialPayload
	if err := json.Unmarshal(env.Data, &p); err != nil {
		return InitialState{}, fmt.Errorf("decode initial state: %w", err)
	}
	return InitialState{
		Snapshot:      snapFromWire(p.Snapshot),
		Strips:        cloneStrips(p.Strips),
		RouterPage:    clonePage(p.RouterPage),
		Streams:       p.Streams,
		Labels:        p.Labels,
		ConfigPath:    p.ConfigPath,
		DaemonVersion: p.DaemonVersion,
	}, nil
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

// Strips returns the latest generic router strips received from the daemon.
func (c *Client) Strips() []StripWire {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneStrips(c.strips)
}

func (c *Client) RouterPage() PageWire {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return clonePage(c.page)
}

// Bind forwards a bind command to the daemon. Implements ui.Dispatcher.
func (c *Client) Bind(ch int, id uint32, name string, kind audio.NodeKind, mprisName string, pid uint32, mediaName string) {
	c.send(kindBind, bindPayload{Ch: ch, ID: id, Name: name, Kind: kind, MPRISName: mprisName, PID: pid, MediaName: mediaName})
}

// Unbind forwards an unbind command to the daemon. Implements ui.Dispatcher.
func (c *Client) Unbind(ch int) {
	c.send(kindUnbind, unbindPayload{Ch: ch})
}

// ToggleMute forwards a mute toggle command to the daemon. Implements ui.Dispatcher.
func (c *Client) ToggleMute(ch int) {
	c.send(kindMute, muteTogglePayload{Ch: ch})
}

// ToggleSolo forwards a solo toggle command to the daemon. Implements ui.Dispatcher.
func (c *Client) ToggleSolo(ch int) {
	c.send(kindSolo, soloTogglePayload{Ch: ch})
}

func (c *Client) SetParam(target, param string, value float64, boolValue bool) {
	c.send(kindSet, setPayload{Target: target, Param: param, Value: value, Bool: boolValue})
}

func (c *Client) ToggleParam(target, param string) {
	c.send(kindToggle, togglePayload{Target: target, Param: param})
}

func (c *Client) RequestBackendView(backendName, view string, data json.RawMessage) {
	c.send(kindBackendViewReq, BackendViewPayload{Backend: backendName, View: view, Data: data})
}

type BackendViewMsg BackendViewPayload

// StripsMsg carries generic router strip state pushed by the daemon.
type StripsMsg StripsWire

func (c *Client) send(kind msgKind, v any) {
	frame, err := encodeFrame(kind, v)
	if err != nil {
		return
	}
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
		env, err := decodeEnvelope(c.scanner.Bytes())
		if err != nil {
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

	case kindStrips:
		var payload StripsWire
		if err := json.Unmarshal(env.Data, &payload); err != nil {
			log.Printf("client: strips: %v", err)
			return
		}
		c.mu.Lock()
		c.strips = cloneStrips(payload.Strips)
		c.page = clonePage(payload.Page)
		c.mu.Unlock()
		if c.prog != nil {
			c.prog.Send(StripsMsg{Page: clonePage(payload.Page), Strips: cloneStrips(payload.Strips)})
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

	case kindBackendViewResp:
		var payload BackendViewPayload
		if err := json.Unmarshal(env.Data, &payload); err != nil {
			log.Printf("client: backend view: %v", err)
			return
		}
		if c.prog != nil {
			c.prog.Send(BackendViewMsg(payload))
		}
	}
}
