package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/daemon"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/midi"
	"github.com/yfernandes/smc-mixer-tui/pipewire"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

func main() {
	var (
		deviceFlag = flag.String("device", "", "MIDI device path (default: auto-detect SMC46)")
		cfgFlag    = flag.String("config", "", "config file (default: "+config.DefaultPath()+")")
	)
	flag.Parse()

	cfgPath := *cfgFlag
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		die("load config: %v", err)
	}

	fixedDevice := *deviceFlag
	if fixedDevice == "" {
		fixedDevice = cfg.MIDI.Device
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pw := pipewire.New()
	enricher := streams.New(pw)
	enricher.SetBlacklist([]string{"pavucontrol"})

	initial, err := enricher.Enrich(ctx)
	if err != nil {
		die("initial stream discovery: %v", err)
	}

	disp := dispatcher.New(pw)
	disp.SetMPRISCaller(streams.NewController())
	applyBindings(cfg, disp, initial)

	manageCrossfaders := newCrossfaderManager(cfg, pw, disp)
	defer manageCrossfaders.Close(context.Background())
	manageCrossfaders.Sync(ctx, disp.Snapshot(), initial)

	srv := daemon.NewServer(disp)
	srv.BroadcastStreams(initial)

	go func() {
		if err := srv.Listen(ctx); err != nil {
			log.Printf("daemon socket: %v", err)
		}
	}()

	midiCh := make(chan midi.Msg, 64)
	dispCh := make(chan midi.Msg, 64)

	go routeMIDI(midiCh, dispCh, srv, disp)
	go disp.Run(ctx, dispCh)
	go runMIDIDeviceLoop(ctx, fixedDevice, srv, disp, midiCh)
	go runVolumePoller(ctx, pw, disp, srv)
	go pollStreams(ctx, enricher, cfg, disp, srv, manageCrossfaders.Sync)

	<-ctx.Done()
}

func routeMIDI(midiCh <-chan midi.Msg, dispCh chan<- midi.Msg, srv *daemon.Server, disp *dispatcher.Dispatcher) {
	for msg := range midiCh {
		switch m := msg.(type) {
		case midi.GlobalMsg:
			srv.BroadcastGlobal(m)
			disp.OnGlobal(m)
		default:
			select {
			case dispCh <- msg:
			default:
			}
		}
	}
	close(dispCh)
}

func runMIDIDeviceLoop(ctx context.Context, fixedDevice string, srv *daemon.Server, disp *dispatcher.Dispatcher, midiCh chan<- midi.Msg) {
	defer close(midiCh)
	for {
		dev := fixedDevice
		if dev == "" {
			var ok bool
			dev, ok = waitForMIDIDevice(ctx, srv)
			if !ok {
				return
			}
		}

		log.Printf("MIDI device: %s", dev)
		srv.BroadcastDevice(midi.DeviceStatusMsg{Connected: true, Device: dev})

		w, werr := midi.OpenWriter(dev)
		if werr != nil {
			log.Printf("MIDI LED writer: %v", werr)
		} else {
			disp.SetLEDWriter(w)
			disp.SyncLEDs()
		}

		listener := midi.NewListener(dev)
		if err := listener.Run(ctx, midiCh); err != nil && ctx.Err() == nil {
			log.Printf("MIDI listener: %v", err)
		}

		if w != nil {
			disp.SetLEDWriter(nil)
			w.Close()
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

func waitForMIDIDevice(ctx context.Context, srv *daemon.Server) (string, bool) {
	srv.BroadcastDevice(midi.DeviceStatusMsg{Connected: false})
	for {
		dev, err := midi.FindDevice("SMC")
		if err == nil {
			return dev, true
		}
		select {
		case <-ctx.Done():
			return "", false
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
		if c.StreamID == nil {
			continue
		}
		vol, _, err := pw.GetVolume(ctx, *c.StreamID)
		if err != nil {
			continue
		}
		disp.UpdateActualVolume(ch, vol)
		if c.MPRISName != "" {
			disp.UpdatePlaybackStatus(ch, streams.IsPlaying(ctx, c.MPRISName))
		}
		changed = true
	}
	return changed
}

func pollStreams(
	ctx context.Context,
	enricher *streams.Enricher,
	cfg *config.Config,
	disp *dispatcher.Dispatcher,
	srv *daemon.Server,
	manageCrossfaders func(context.Context, [8]dispatcher.Channel, []streams.EnrichedStream),
) {
	enricher.Poll(ctx, 2*time.Second, func(msg streams.UpdateMsg) {
		ss := []streams.EnrichedStream(msg)
		applyBindings(cfg, disp, ss)
		manageCrossfaders(ctx, disp.Snapshot(), ss)
		srv.BroadcastStreams(ss)
		srv.BroadcastSnapshot(disp.Snapshot())
	})
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smc-mixerd: "+format+"\n", args...)
	os.Exit(1)
}
