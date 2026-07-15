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

	"github.com/yfernandes/smc-mixer-tui/backend"
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
	version    string

	mu      sync.RWMutex
	clients map[*serverConn]struct{}

	streamsMu      sync.RWMutex
	currentStreams []streams.EnrichedStream

	stripsMu      sync.RWMutex
	currentStrips []StripWire
	currentPage   PageWire

	// AfterCmd is called after each bind/unbind command is applied to the
	// dispatcher, before BroadcastSnapshot. Use it to update any state that
	// depends on the current channel snapshot (e.g. crossfader attachment).
	AfterCmd func(ctx context.Context)

	// RoutingSnapshot builds the routing inspector payload on demand. Nil until
	// set by main; requests are ignored until then.
	RoutingSnapshot func(ctx context.Context) RoutingSnapshot

	// RetargetOutput repoints a crossfade branch's output sink. Nil until set
	// by main; requests are ignored until then.
	RetargetOutput func(ctx context.Context, deviceKey, branch, sinkNodeName, sinkDisplayName string) error

	// RouterSet and RouterToggle handle generic strip commands.
	RouterSet       func(ctx context.Context, target, param string, value backend.Value) error
	RouterToggle    func(ctx context.Context, target, param string) error
	RouterOwnsStrip func(strip int) bool
	RouterBind      func(ctx context.Context, strip int, nodeID uint32) error
	RouterUnbind    func(ctx context.Context, strip int) error
	RouterMute      func(ctx context.Context, strip int) error
	RouterSolo      func(ctx context.Context, strip int) error
}

// NewServer creates a Server backed by disp. labels are the per-channel config
// labels sent to clients on connect so the TUI can show them for unbound strips.
func NewServer(disp *dispatcher.Dispatcher, labels [8]string, configPath string, version string) *Server {
	return &Server{
		disp:       disp,
		labels:     labels,
		configPath: configPath,
		version:    version,
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

// BroadcastStrips pushes generic router strip state to all connected clients.
func (s *Server) BroadcastStrips(strips []StripWire, page PageWire) {
	s.stripsMu.Lock()
	s.currentStrips = cloneStrips(strips)
	s.currentPage = clonePage(page)
	s.stripsMu.Unlock()
	s.broadcast(kindStrips, StripsWire{Page: clonePage(page), Strips: strips})
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
	s.stripsMu.RLock()
	currentStrips := cloneStrips(s.currentStrips)
	currentPage := clonePage(s.currentPage)
	s.stripsMu.RUnlock()

	init := initialPayload{
		Snapshot:      snapToWire(s.disp.Snapshot()),
		Strips:        currentStrips,
		RouterPage:    currentPage,
		Streams:       currentStreams,
		Labels:        s.labels,
		ConfigPath:    s.configPath,
		DaemonVersion: s.version,
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
		if env.Kind == kindRoutingRequest {
			if s.RoutingSnapshot != nil {
				sc.writeMsg(kindRouting, s.RoutingSnapshot(ctx))
			}
			continue
		}
		if env.Kind == kindRetarget {
			var p retargetPayload
			if err := json.Unmarshal(env.Data, &p); err != nil {
				log.Printf("daemon: retarget decode: %v", err)
				continue
			}
			if s.RetargetOutput != nil {
				if err := s.RetargetOutput(ctx, p.DeviceKey, p.Branch, p.SinkNodeName, p.SinkDisplayName); err != nil {
					log.Printf("daemon: retarget %s/%s -> %s: %v", p.DeviceKey, p.Branch, p.SinkNodeName, err)
				}
			}
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
	if s.AfterCmd != nil {
		s.AfterCmd(ctx)
	}
	s.BroadcastSnapshot(s.disp.Snapshot())
}

type clientCommand struct {
	kind msgKind
	bind bindPayload
	set  setPayload
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

	case kindSet:
		var p setPayload
		if err := json.Unmarshal(env.Data, &p); err != nil {
			return clientCommand{}, false, err
		}
		return clientCommand{kind: env.Kind, set: p}, true, nil

	case kindToggle:
		var p togglePayload
		if err := json.Unmarshal(env.Data, &p); err != nil {
			return clientCommand{}, false, err
		}
		return clientCommand{kind: env.Kind, set: setPayload{Target: p.Target, Param: p.Param}}, true, nil
	}
	return clientCommand{}, false, nil
}

func (cmd clientCommand) apply(ctx context.Context, s *Server) {
	routerOwned := s.RouterOwnsStrip != nil && s.RouterOwnsStrip(cmd.ch)
	switch cmd.kind {
	case kindBind:
		p := cmd.bind
		if routerOwned && s.RouterBind != nil {
			if err := s.RouterBind(ctx, p.Ch, p.ID); err != nil {
				log.Printf("daemon: router bind: %v", err)
			}
			return
		}
		s.disp.UserBind(p.Ch, p.ID, p.Name, p.Kind, p.MPRISName, p.PID, p.MediaName)
	case kindUnbind:
		if routerOwned && s.RouterUnbind != nil {
			_ = s.RouterUnbind(ctx, cmd.ch)
			return
		}
		s.disp.Unbind(cmd.ch)
	case kindMute:
		if routerOwned && s.RouterMute != nil {
			_ = s.RouterMute(ctx, cmd.ch)
			return
		}
		s.disp.ToggleMute(cmd.ch)
	case kindSolo:
		if routerOwned && s.RouterSolo != nil {
			_ = s.RouterSolo(ctx, cmd.ch)
			return
		}
		s.disp.ToggleSolo(cmd.ch)
	case kindSet:
		if s.RouterSet != nil {
			if err := s.RouterSet(ctx, cmd.set.Target, cmd.set.Param, backend.Value{F: cmd.set.Value, B: cmd.set.Bool}); err != nil {
				log.Printf("daemon: router set %s/%s: %v", cmd.set.Target, cmd.set.Param, err)
			}
		}
	case kindToggle:
		if s.RouterToggle != nil {
			if err := s.RouterToggle(ctx, cmd.set.Target, cmd.set.Param); err != nil {
				log.Printf("daemon: router toggle %s/%s: %v", cmd.set.Target, cmd.set.Param, err)
			}
		}
	}
}

func cloneStrips(in []StripWire) []StripWire {
	if in == nil {
		return nil
	}
	out := make([]StripWire, len(in))
	for i, s := range in {
		out[i] = s
		if s.Params != nil {
			out[i].Params = make(map[string]ParamWire, len(s.Params))
			for k, v := range s.Params {
				out[i].Params[k] = v
			}
		}
		out[i].Ext = append([]byte(nil), s.Ext...)
	}
	return out
}

func clonePage(in PageWire) PageWire {
	in.Labels = append([]string(nil), in.Labels...)
	return in
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
