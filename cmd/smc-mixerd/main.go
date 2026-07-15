package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/yfernandes/smc-mixer-tui/backend"
	"github.com/yfernandes/smc-mixer-tui/backend/pwbackend"
	"github.com/yfernandes/smc-mixer-tui/backend/shellexec"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/daemon"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/midi"
	"github.com/yfernandes/smc-mixer-tui/pipewire"
	"github.com/yfernandes/smc-mixer-tui/router"
	"github.com/yfernandes/smc-mixer-tui/streams"
	"github.com/yfernandes/smc-mixer-tui/surface"
	"github.com/yfernandes/smc-mixer-tui/surface/smc46"
)

// Version is set at build time via -ldflags "-X main.Version=...".
var Version = "dev"

func main() {
	var (
		deviceFlag = flag.String("device", "", "MIDI device path (default: auto-detect SMC46)")
		cfgFlag    = flag.String("config", "", "config file (default: "+config.DefaultPath()+")")
	)
	flag.Parse()

	// Singleton guard: refuse to start a second driver. Must run before any MIDI
	// or PipeWire work — a second instance would grab the MIDI device and tear
	// down the running daemon's crossfader modules. Exit 0 (not a failure) so a
	// systemd Restart=on-failure policy does not restart-loop.
	lock, err := acquireSingletonLock()
	if err == errAlreadyRunning {
		log.Print("smc-mixerd already running; exiting")
		return
	}
	if err != nil {
		die("singleton lock: %v", err)
	}
	defer lock.Close()

	cfgPath := *cfgFlag
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		die("load config: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		die("invalid config: %v", err)
	}
	if err := cfg.ValidateRouter(smc46.Descriptor().Strips); err != nil {
		die("invalid router config: %v", err)
	}

	fixedDevice := *deviceFlag
	if fixedDevice == "" {
		fixedDevice = cfg.MIDI.Device
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pw := pipewire.New()
	enricher := streams.New(pw)
	enricher.SetBlacklist([]string{"pavucontrol", "smc_", "loopback-"})

	initial, err := enricher.Enrich(ctx)
	if err != nil {
		die("initial stream discovery: %v", err)
	}

	pinned := newPinnedState(pinnedStatePath())
	pinned.load(cfg)

	disp := dispatcher.New(pw)
	disp.SetVolThrottle(20 * time.Millisecond)
	disp.SetMPRISCaller(streams.NewController())
	disp.SetPinCallback(func(ch int) {
		page := disp.ActivePage()
		key := cfg.DeviceKeyForPage(page, ch)
		if key == "" {
			return
		}
		nowPinned := pinned.toggle(cfg, ch, key)
		disp.SetPinned(ch, nowPinned)
	})
	applyBindings(ctx, cfg, disp, initial, pinned.snapshot(), pw.GetVolume)

	execBackend := shellexec.New(cfg.Exec)
	pwBackend := pwbackend.New(pw, enricher, cfg.Devices)
	rt, err := router.NewFromConfigWithStrips(map[string]backend.Backend{
		execBackend.Name(): execBackend,
		pwBackend.Name():   pwBackend,
	}, cfg.Router, smc46.Descriptor().Strips)
	if err != nil {
		die("router config: %v", err)
	}

	manageCrossfaders := newCrossfaderManager(cfg, pw, disp)
	defer manageCrossfaders.Close(context.Background())
	manageCrossfaders.Sync(ctx, disp.Snapshot(), initial)

	srv := daemon.NewServer(disp, configLabels(cfg), cfgPath, Version)
	srv.RouterSet = rt.SetParam
	srv.RouterToggle = rt.ToggleParam
	srv.RouterOwnsStrip = rt.OwnsStrip
	srv.RouterBind = func(ctx context.Context, strip int, nodeID uint32) error {
		return rt.ReassignStrip(ctx, strip, backend.TargetID(fmt.Sprintf("pipewire:node/%d", nodeID)))
	}
	srv.RouterUnbind = rt.ClearStrip
	srv.RouterMute = func(ctx context.Context, strip int) error { return rt.ToggleStripParam(ctx, strip, surface.RoleMute) }
	srv.RouterSolo = rt.ToggleSolo
	rt.SetChangeCallback(func(strips []router.StripState, page router.PageInfo) {
		srv.BroadcastStrips(routerStripsToWire(strips), routerPageToWire(page))
	})
	rt.Activate(ctx)
	srv.BroadcastStrips(routerStripsToWire(rt.Snapshot()), routerPageToWire(rt.PageInfo()))
	srv.RoutingSnapshot = func(ctx context.Context) daemon.RoutingSnapshot {
		return buildRoutingSnapshot(ctx, pw, disp, manageCrossfaders, cfg)
	}
	srv.RetargetOutput = func(ctx context.Context, deviceKey, branch, sinkNodeName, sinkDisplayName string) error {
		return manageCrossfaders.RetargetOutput(ctx, deviceKey, branch, sinkNodeName, sinkDisplayName)
	}
	srv.AfterCmd = func(ctx context.Context) {
		snap := disp.Snapshot()
		// Fast synchronous path: update dispatcher knob attachment for any routing
		// that already exists (e.g. stream moving between channels).
		manageCrossfaders.Reattach(snap)
		// Async path: create PipeWire routing for newly-bound streams if the cached
		// stream list already knows about them. SetupCrossfader sleeps ~190 ms, so
		// running it inline would stall every bind response.
		go func() {
			manageCrossfaders.SyncIfAble(ctx, disp.Snapshot())
			srv.BroadcastSnapshot(disp.Snapshot())
		}()
	}
	srv.BroadcastStreams(initial)

	go func() {
		if err := srv.Listen(ctx); err != nil {
			log.Printf("daemon socket: %v", err)
		}
	}()

	midiCh := make(chan midi.Msg, 256)
	dispCh := make(chan midi.Msg, 256)

	// rebindCh is written by OnGlobal (via page change callback) and consumed
	// by pollStreams to trigger an immediate applyBindings after a page switch.
	rebindCh := make(chan struct{}, 1)
	disp.SetPageChangeCallback(func() {
		select {
		case rebindCh <- struct{}{}:
		default:
		}
	})

	go routeMIDI(ctx, midiCh, dispCh, srv, disp, rt)
	go rt.Run(ctx)
	go disp.Run(ctx, dispCh)
	go runMIDIDeviceLoop(ctx, fixedDevice, srv, disp, rt, midiCh)
	go runVolumePoller(ctx, pw, disp, srv)
	go pollStreams(ctx, enricher, cfg, disp, srv, pinned, manageCrossfaders.Sync, rebindCh, pw.GetVolume)

	<-ctx.Done()
}

func routeMIDI(ctx context.Context, midiCh <-chan midi.Msg, dispCh chan<- midi.Msg, srv *daemon.Server, disp *dispatcher.Dispatcher, rt *router.Router) {
	for msg := range midiCh {
		routerHandled := false
		if ev, ok := smc46.EventFromMsg(msg); ok && rt != nil {
			routerHandled = rt.OwnsStrip(ev.Strip)
			rt.HandleEvent(ctx, ev)
		}
		switch m := msg.(type) {
		case midi.GlobalMsg:
			if routeGlobalMsg(ctx, rt, m) {
				continue
			}
			srv.BroadcastGlobal(m)
			disp.OnGlobal(m)
		default:
			if routerHandled {
				continue
			}
			select {
			case dispCh <- msg:
			default:
			}
		}
	}
	close(dispCh)
}

func routeGlobalMsg(ctx context.Context, rt *router.Router, m midi.GlobalMsg) bool {
	if rt == nil || !m.Pressed {
		return false
	}
	role := smc46.RoleFromGlobal(m.Action)
	if role == "" {
		return false
	}
	if rt.HasPageButton(role) || (rt.ActivePage() && isRouterScrollRole(role)) {
		return rt.HandleGlobal(ctx, role, true)
	}
	return false
}

func isRouterScrollRole(role surface.Role) bool {
	switch role {
	case "up", "down", "left", "right":
		return true
	default:
		return false
	}
}

func routerStripsToWire(strips []router.StripState) []daemon.StripWire {
	out := make([]daemon.StripWire, 0, len(strips))
	for _, s := range strips {
		params := make(map[string]daemon.ParamWire, len(s.Params))
		for id, p := range s.Params {
			params[id] = daemon.ParamWire{
				Kind:     uint8(p.Kind),
				Value:    p.Value.F,
				Bool:     p.Value.B,
				Readable: p.Readable,
				Synced:   p.Synced,
			}
		}
		out = append(out, daemon.StripWire{
			Strip:    s.Strip,
			Label:    s.Label,
			Backend:  s.Backend,
			TargetID: s.TargetID,
			Params:   params,
			Ext:      s.Ext,
		})
	}
	return out
}

func routerPageToWire(page router.PageInfo) daemon.PageWire {
	return daemon.PageWire{
		Name:   page.Name,
		Offset: page.Offset,
		Total:  page.Total,
		Labels: append([]string(nil), page.Labels...),
		Active: page.Active,
	}
}

func runMIDIDeviceLoop(ctx context.Context, fixedDevice string, srv *daemon.Server, disp *dispatcher.Dispatcher, rt *router.Router, midiCh chan<- midi.Msg) {
	defer close(midiCh)
	for {
		dev, cleanup, ok := waitForMIDIDevice(ctx, fixedDevice, srv)
		if !ok {
			return
		}

		log.Printf("MIDI device: %s", dev)
		srv.BroadcastDevice(midi.DeviceStatusMsg{Connected: true, Device: dev})

		w, werr := midi.OpenWriter(dev)
		if werr != nil {
			log.Printf("MIDI LED writer: %v", werr)
		} else {
			disp.SetLEDWriter(w)
			rt.SetFeedbackWriter(smc46.Feedback{Writer: w})
			disp.SyncLEDs()
		}

		listener := midi.NewListener(dev)
		if err := listener.Run(ctx, midiCh); err != nil && ctx.Err() == nil {
			log.Printf("MIDI listener: %v", err)
		}

		if w != nil {
			disp.SetLEDWriter(nil)
			rt.SetFeedbackWriter(nil)
			w.ClearLEDs()
			w.Close()
		}

		if cleanup != nil {
			cleanup()
		}

		if ctx.Err() != nil {
			return
		}

		log.Printf("MIDI device disconnected, waiting for reconnect…")
		srv.BroadcastDevice(midi.DeviceStatusMsg{Connected: false})
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// waitForMIDIDevice blocks until a MIDI device is available and returns its path
// and an optional cleanup func (non-nil when a virmidi bridge was created).
// If fixedDevice is set, it polls that path directly; otherwise it auto-detects
// a USB SMC device first, then falls back to a Bluetooth sequencer bridge.
// Broadcasts Connected:false once before the first poll attempt.
func waitForMIDIDevice(ctx context.Context, fixedDevice string, srv *daemon.Server) (dev string, cleanup func(), ok bool) {
	srv.BroadcastDevice(midi.DeviceStatusMsg{Connected: false})
	for {
		if fixedDevice != "" {
			if _, err := os.Stat(fixedDevice); err == nil {
				return fixedDevice, nil, true
			}
		} else {
			if dev, err := midi.FindDevice("SMC"); err == nil {
				return dev, nil, true
			}
			if bridge, err := midi.BridgeSequencerPort("SMC"); err == nil {
				return bridge.DevPath, bridge.Close, true
			} else {
				log.Printf("BLE MIDI bridge failed: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return "", nil, false
		case <-time.After(2 * time.Second):
		}
	}
}

func runVolumePoller(ctx context.Context, pw *pipewire.Client, disp *dispatcher.Dispatcher, srv *daemon.Server) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if pollChannelVolumes(ctx, pw, disp) {
				srv.BroadcastSnapshot(disp.Snapshot())
			}
		}
	}
}

func pollChannelVolumes(ctx context.Context, pw *pipewire.Client, disp *dispatcher.Dispatcher) bool {
	snap := disp.Snapshot()
	changed := false
	for ch, c := range snap {
		if c.StreamID != nil {
			vol, _, err := pw.GetVolume(ctx, *c.StreamID)
			if err == nil {
				disp.UpdateActualVolume(ch, vol)
				if c.MPRISName != "" {
					// Pass the stream ID so UpdatePlaybackStatus can verify the channel
					// is still bound to the same stream (guards against stale-snapshot races
					// where Unbind ran between Snapshot() and here).
					disp.UpdatePlaybackStatusForStream(ch, *c.StreamID, streams.IsPlaying(ctx, c.MPRISName))
				}
				changed = true
			}
		}
		if c.KnobStreamID != nil {
			vol, _, err := pw.GetVolume(ctx, *c.KnobStreamID)
			if err == nil {
				disp.SetKnob(ch, int(math.Round(vol*127)))
				changed = true
			}
		}
	}
	return changed
}

func pollStreams(
	ctx context.Context,
	enricher *streams.Enricher,
	cfg *config.Config,
	disp *dispatcher.Dispatcher,
	srv *daemon.Server,
	pinned *pinnedState,
	manageCrossfaders func(context.Context, [8]dispatcher.Channel, []streams.EnrichedStream),
	rebindCh <-chan struct{},
	getVol knobVolumeGetter,
) {
	var (
		lastMu sync.Mutex
		lastSS []streams.EnrichedStream
		bindMu sync.Mutex // serialises clearPageAssignments+applyBindings vs enricher applyBindings
	)

	// Listen for immediate-rebind triggers from page switches.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-rebindCh:
				lastMu.Lock()
				ss := lastSS
				lastMu.Unlock()
				if ss == nil {
					continue
				}
				bindMu.Lock()
				clearPageAssignments(disp)
				applyBindings(ctx, cfg, disp, ss, pinned.snapshot(), getVol)
				manageCrossfaders(ctx, disp.Snapshot(), ss)
				srv.BroadcastSnapshot(disp.Snapshot())
				bindMu.Unlock()
			}
		}
	}()

	enricher.Poll(ctx, 2*time.Second, func(msg streams.UpdateMsg) {
		ss := []streams.EnrichedStream(msg)
		lastMu.Lock()
		lastSS = ss
		lastMu.Unlock()
		bindMu.Lock()
		applyBindings(ctx, cfg, disp, ss, pinned.snapshot(), getVol)
		manageCrossfaders(ctx, disp.Snapshot(), ss)
		srv.BroadcastStreams(ss)
		srv.BroadcastSnapshot(disp.Snapshot())
		bindMu.Unlock()
	})
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smc-mixerd: "+format+"\n", args...)
	os.Exit(1)
}
