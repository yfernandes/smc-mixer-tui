package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/yfernandes/smc-mixer-tui/audio"
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
	manageCrossfaders(ctx, disp.Snapshot(), initial)

	srv := daemon.NewServer(disp)
	srv.BroadcastStreams(initial)

	go func() {
		if err := srv.Listen(ctx); err != nil {
			log.Printf("daemon socket: %v", err)
		}
	}()

	var (
		enrichedMu   sync.Mutex
		lastEnriched = initial
	)

	midiCh := make(chan midi.Msg, 64)
	dispCh := make(chan midi.Msg, 64)

	go func() {
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
	}()

	go disp.Run(ctx, dispCh)

	go func() {
		defer close(midiCh)
		for {
			dev := fixedDevice
			if dev == "" {
				srv.BroadcastDevice(midi.DeviceStatusMsg{Connected: false})
				for {
					var ferr error
					dev, ferr = midi.FindDevice("SMC")
					if ferr == nil {
						break
					}
					select {
					case <-ctx.Done():
						return
					case <-time.After(2 * time.Second):
					}
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
	}()

	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
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
				if changed {
					srv.BroadcastSnapshot(disp.Snapshot())
				}
			}
		}
	}()

	go enricher.Poll(ctx, 2*time.Second, func(msg streams.UpdateMsg) {
		ss := []streams.EnrichedStream(msg)

		enrichedMu.Lock()
		lastEnriched = ss
		enrichedMu.Unlock()

		applyBindings(cfg, disp, ss)
		manageCrossfaders(ctx, disp.Snapshot(), ss)
		srv.BroadcastStreams(ss)
		srv.BroadcastSnapshot(disp.Snapshot())
	})

	<-ctx.Done()
	_ = lastEnriched
}

func applyBindings(cfg *config.Config, disp *dispatcher.Dispatcher, ss []streams.EnrichedStream) {
	snap := disp.Snapshot()
	for ch := range 8 {
		chCfg := cfg.ChannelFor(ch)
		if chCfg == nil {
			continue
		}
		if snap[ch].StreamID != nil && streamLive(*snap[ch].StreamID, ss) {
			continue
		}
		matchStr := cfg.MatchStringFor(ch)
		for _, s := range ss {
			if !streamMatchesBind(s, chCfg.Bind, matchStr) {
				continue
			}
			mprisName := ""
			if s.Source == streams.SourceMPRIS {
				mprisName = s.Name
			}
			disp.Bind(ch, s.ID, s.Name, s.Kind, mprisName)
			break
		}
	}
}

func streamMatchesBind(s streams.EnrichedStream, bind config.BindConfig, resolvedMatch string) bool {
	switch bind.Type {
	case "input":
		if s.Kind != audio.KindMic {
			return false
		}
	case "playback":
		if s.Kind != audio.KindSource {
			return false
		}
	case "output":
		if s.Kind != audio.KindSink {
			return false
		}
	}
	if bind.MatchRegex != "" {
		re, err := regexp.Compile("(?i)" + bind.MatchRegex)
		if err == nil && (re.MatchString(s.Name) || re.MatchString(s.BindKey)) {
			return true
		}
		return false
	}
	if resolvedMatch != "" {
		lower := strings.ToLower(resolvedMatch)
		return strings.Contains(strings.ToLower(s.Name), lower) ||
			strings.Contains(strings.ToLower(s.BindKey), lower)
	}
	return false
}

func streamLive(id uint32, ss []streams.EnrichedStream) bool {
	for _, s := range ss {
		if s.ID == id {
			return true
		}
	}
	return false
}

type crossfaderState struct {
	routing  *pipewire.CrossfaderRouting
	streamID uint32
}

type channelCrossfader struct {
	pw      *pipewire.Client
	routing *pipewire.CrossfaderRouting
}

func (c *channelCrossfader) SetGains(ctx context.Context, volA, volB float64) error {
	return c.pw.SetCrossfaderGains(ctx, c.routing, volA, volB)
}

func newCrossfaderManager(cfg *config.Config, pw *pipewire.Client, disp *dispatcher.Dispatcher) func(context.Context, [8]dispatcher.Channel, []streams.EnrichedStream) {
	active := [8]*crossfaderState{}

	return func(ctx context.Context, snap [8]dispatcher.Channel, ss []streams.EnrichedStream) {
		for ch := range 8 {
			knob, ok := cfg.KnobFor(ch)
			isCrossfade := ok && knob.Type == "crossfade"
			streamID := snap[ch].StreamID

			if active[ch] != nil {
				gone := !isCrossfade || streamID == nil || *streamID != active[ch].streamID
				if gone {
					pw.TeardownCrossfader(ctx, active[ch].routing)
					disp.SetCrossfader(ch, nil, "", "")
					active[ch] = nil
				}
			}

			if !isCrossfade || streamID == nil || active[ch] != nil {
				continue
			}

			sinkANodeName, sinkBNodeName, nameA, nameB := resolveCrossfaderSinks(cfg, knob, ss)
			if sinkANodeName == "" || sinkBNodeName == "" {
				log.Printf("crossfader ch%d: sinks not found (A=%q B=%q)", ch, knob.OutputA, knob.OutputB)
				continue
			}

			var streamNodeName string
			for _, s := range ss {
				if s.ID == *streamID {
					streamNodeName = s.NodeName
					break
				}
			}

			tag := fmt.Sprintf("ch%d", ch)
			routing, err := pw.SetupCrossfader(ctx, tag, *streamID, streamNodeName, sinkANodeName, sinkBNodeName)
			if err != nil {
				log.Printf("crossfader ch%d setup: %v", ch, err)
				continue
			}

			active[ch] = &crossfaderState{routing: routing, streamID: *streamID}
			ctrl := &channelCrossfader{pw: pw, routing: routing}
			disp.SetCrossfader(ch, ctrl, nameA, nameB)
			log.Printf("crossfader ch%d: %s ↔ %s", ch, nameA, nameB)
		}
	}
}

func resolveCrossfaderSinks(cfg *config.Config, knob config.KnobConfig, ss []streams.EnrichedStream) (nodeA, nodeB, nameA, nameB string) {
	descA := strings.ToLower(cfg.ResolveOutput(knob.OutputA))
	descB := strings.ToLower(cfg.ResolveOutput(knob.OutputB))
	for _, s := range ss {
		if s.Kind != audio.KindSink {
			continue
		}
		lower := strings.ToLower(s.Name)
		if nodeA == "" && descA != "" && strings.Contains(lower, descA) {
			nodeA, nameA = s.NodeName, s.Name
		}
		if nodeB == "" && descB != "" && strings.Contains(lower, descB) {
			nodeB, nameB = s.NodeName, s.Name
		}
	}
	return
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smc-mixerd: "+format+"\n", args...)
	os.Exit(1)
}
