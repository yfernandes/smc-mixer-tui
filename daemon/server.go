package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/midi"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

// Server is the daemon-side IPC listener. It broadcasts state to all connected
// TUI clients and dispatches bind/unbind commands back to the dispatcher.
type Server struct {
	disp       *dispatcher.Dispatcher
	labels     [8]string
	configPath string

	mu      sync.RWMutex
	clients map[*serverConn]struct{}

	streamsMu      sync.RWMutex
	currentStreams []streams.EnrichedStream
}

// NewServer creates a Server backed by disp. labels are the per-channel config
// labels sent to clients on connect so the TUI can show them for unbound strips.
func NewServer(disp *dispatcher.Dispatcher, labels [8]string, configPath string) *Server {
	return &Server{
		disp:       disp,
		labels:     labels,
		configPath: configPath,
		clients:    make(map[*serverConn]struct{}),
	}
}

// Listen binds to the daemon socket and accepts TUI connections until ctx is
// cancelled. It removes any stale socket file before listening.
func (s *Server) Listen(ctx context.Context) error {
	path := SocketPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	_ = os.Remove(path)

	l, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		l.Close()
	}()

	log.Printf("daemon: socket %s", path)
	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("daemon: accept: %v", err)
			continue
		}
		sc := &serverConn{conn: conn}
		s.mu.Lock()
		s.clients[sc] = struct{}{}
		s.mu.Unlock()
		go s.serveConn(ctx, sc)
	}
}

// ── Broadcast helpers ─────────────────────────────────────────────────────────

// BroadcastSnapshot pushes the current channel state to all connected clients.
func (s *Server) BroadcastSnapshot(snap [8]dispatcher.Channel) {
	s.broadcast(kindSnapshot, snapToWire(snap))
}

// BroadcastStreams pushes an updated stream list and caches it for new clients.
func (s *Server) BroadcastStreams(ss []streams.EnrichedStream) {
	s.streamsMu.Lock()
	s.currentStreams = ss
	s.streamsMu.Unlock()
	s.broadcast(kindStreams, ss)
}

// BroadcastDevice notifies clients of a MIDI device connect/disconnect.
func (s *Server) BroadcastDevice(msg midi.DeviceStatusMsg) {
	s.broadcast(kindDevice, msg)
}

// BroadcastGlobal notifies clients of a transport button press.
func (s *Server) BroadcastGlobal(msg midi.GlobalMsg) {
	s.broadcast(kindGlobal, msg)
}

func (s *Server) broadcast(kind msgKind, v any) {
	frame, err := encodeFrame(kind, v)
	if err != nil {
		log.Printf("daemon: marshal %s: %v", kind, err)
		return
	}

	s.mu.RLock()
	clients := make([]*serverConn, 0, len(s.clients))
	for sc := range s.clients {
		clients = append(clients, sc)
	}
	s.mu.RUnlock()

	for _, sc := range clients {
		sc.write(frame)
	}
}

// ── Per-connection handling ───────────────────────────────────────────────────

func (s *Server) serveConn(ctx context.Context, sc *serverConn) {
	defer func() {
		s.mu.Lock()
		delete(s.clients, sc)
		s.mu.Unlock()
		sc.conn.Close()
	}()

	// Send the full current state immediately so the TUI can render right away.
	s.streamsMu.RLock()
	currentStreams := s.currentStreams
	s.streamsMu.RUnlock()

	init := initialPayload{
		Snapshot:   snapToWire(s.disp.Snapshot()),
		Streams:    currentStreams,
		Labels:     s.labels,
		ConfigPath: s.configPath,
	}
	if currentStreams == nil {
		init.Streams = []streams.EnrichedStream{}
	}
	sc.writeMsg(kindInitial, init)

	scanner := bufio.NewScanner(sc.conn)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		env, err := decodeEnvelope(scanner.Bytes())
		if err != nil {
			log.Printf("daemon: client decode: %v", err)
			continue
		}
		s.handleCmd(ctx, env)
	}
}

func (s *Server) handleCmd(ctx context.Context, env envelope) {
	cmd, ok, err := decodeCommand(env)
	if err != nil {
		log.Printf("daemon: %s decode: %v", env.Kind, err)
		return
	}
	if !ok {
		return
	}
	cmd.apply(ctx, s)
	s.BroadcastSnapshot(s.disp.Snapshot())
}

type clientCommand struct {
	kind msgKind
	bind bindPayload
	ch   int
}

func decodeCommand(env envelope) (clientCommand, bool, error) {
	switch env.Kind {
	case kindBind:
		var p bindPayload
		if err := json.Unmarshal(env.Data, &p); err != nil {
			return clientCommand{}, false, err
		}
		return clientCommand{kind: env.Kind, bind: p, ch: p.Ch}, true, nil

	case kindUnbind:
		var p unbindPayload
		if err := json.Unmarshal(env.Data, &p); err != nil {
			return clientCommand{}, false, err
		}
		return clientCommand{kind: env.Kind, ch: p.Ch}, true, nil

	case kindMute:
		var p muteTogglePayload
		if err := json.Unmarshal(env.Data, &p); err != nil {
			return clientCommand{}, false, err
		}
		return clientCommand{kind: env.Kind, ch: p.Ch}, true, nil

	case kindSolo:
		var p soloTogglePayload
		if err := json.Unmarshal(env.Data, &p); err != nil {
			return clientCommand{}, false, err
		}
		return clientCommand{kind: env.Kind, ch: p.Ch}, true, nil
	}
	return clientCommand{}, false, nil
}

func (cmd clientCommand) apply(ctx context.Context, s *Server) {
	switch cmd.kind {
	case kindBind:
		p := cmd.bind
		s.disp.UserBind(p.Ch, p.ID, p.Name, p.Kind, p.MPRISName)
	case kindUnbind:
		s.disp.Unbind(cmd.ch)
	case kindMute:
		s.disp.ToggleMute(cmd.ch)
	case kindSolo:
		s.disp.ToggleSolo(cmd.ch)
	}
}

// ── serverConn ────────────────────────────────────────────────────────────────

type serverConn struct {
	conn net.Conn
	mu   sync.Mutex
}

func (sc *serverConn) write(frame []byte) {
	sc.mu.Lock()
	sc.conn.Write(frame) //nolint:errcheck — closed conn errors handled by scanner
	sc.mu.Unlock()
}

func (sc *serverConn) writeMsg(kind msgKind, v any) {
	frame, err := encodeFrame(kind, v)
	if err != nil {
		return
	}
	sc.write(frame)
}
