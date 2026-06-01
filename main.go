package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/midi"
	"github.com/yfernandes/smc-mixer-tui/pipewire"
	"github.com/yfernandes/smc-mixer-tui/streams"
	"github.com/yfernandes/smc-mixer-tui/ui"
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

	// fixedDevice is the explicit path from --device / config, or "" for auto-discover.
	fixedDevice := *deviceFlag
	if fixedDevice == "" {
		fixedDevice = cfg.MIDI.Device
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pw := pipewire.New()
	enricher := streams.New(pw)

	blacklist := cfg.Streams.Blacklist
	if len(blacklist) == 0 {
		blacklist = []string{"pavucontrol"} // built-in default; override via [streams] blacklist in config
	}
	enricher.SetBlacklist(blacklist)

	initial, err := enricher.Enrich(ctx)
	if err != nil {
		die("initial stream discovery: %v", err)
	}

	disp := dispatcher.New(pw)
	disp.SetMPRISCaller(streams.NewController())
	applyBindings(cfg, disp, initial)

	var (
		enrichedMu   sync.Mutex
		lastEnriched = initial
	)

	midiCh := make(chan midi.Msg, 64) // listener → fan-out
	dispCh := make(chan midi.Msg, 64) // fan-out → dispatcher

	program := tea.NewProgram(ui.New(disp, initial), tea.WithAltScreen())

	// Fan-out: GlobalMsgs go to the UI; channel msgs go to the dispatcher.
	go func() {
		for msg := range midiCh {
			switch m := msg.(type) {
			case midi.GlobalMsg:
				program.Send(m)
				disp.OnGlobal(m)
			default:
				select {
				case dispCh <- msg:
				default: // full buffer: drop rather than block
				}
			}
		}
		close(dispCh)
	}()

	go disp.Run(ctx, dispCh)

	// MIDI watcher: auto-discover on startup and reconnect after hot-unplug.
	go func() {
		defer close(midiCh)
		for {
			dev := fixedDevice
			if dev == "" {
				program.Send(midi.DeviceStatusMsg{Connected: false})
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
			program.Send(midi.DeviceStatusMsg{Connected: true, Device: dev})

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
			program.Send(midi.DeviceStatusMsg{Connected: false})
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}()

	// Poll PipeWire for actual volumes and MPRIS playback status so strips
	// react to external changes (keyboard shortcuts, remote controls, etc.).
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				snap := disp.Snapshot()
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
		program.Send(msg)
	})

	if _, err := program.Run(); err != nil {
		die("UI: %v", err)
	}

	cancel()

	enrichedMu.Lock()
	le := lastEnriched
	enrichedMu.Unlock()

	saveFinalBindings(cfg, disp, le)
	if err := config.Save(cfgPath, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "smc-mixer: save config: %v\n", err)
	}
}

// applyBindings binds dispatcher channels that have a config key matching an
// enriched stream's BindKey (case-insensitive). Already-bound channels whose
// stream is still live are left alone; channels with no config key are skipped.
func applyBindings(cfg *config.Config, disp *dispatcher.Dispatcher, ss []streams.EnrichedStream) {
	snap := disp.Snapshot()
	for ch := range 8 {
		key := cfg.StreamFor(ch)
		if key == "" {
			continue
		}
		if snap[ch].StreamID != nil && streamLive(*snap[ch].StreamID, ss) {
			continue
		}
		for _, s := range ss {
			if strings.EqualFold(s.BindKey, key) {
				mprisName := ""
				if s.Source == streams.SourceMPRIS {
					mprisName = s.Name
				}
				disp.Bind(ch, s.ID, s.Name, dispatcher.NodeKind(s.Kind), mprisName)
				break
			}
		}
	}
}

// streamLive reports whether id is present in ss.
func streamLive(id uint32, ss []streams.EnrichedStream) bool {
	for _, s := range ss {
		if s.ID == id {
			return true
		}
	}
	return false
}

// saveFinalBindings writes the current dispatcher snapshot back into cfg so
// that the next startup auto-binds to the same streams.
func saveFinalBindings(cfg *config.Config, disp *dispatcher.Dispatcher, enriched []streams.EnrichedStream) {
	byID := make(map[uint32]streams.EnrichedStream, len(enriched))
	for _, s := range enriched {
		byID[s.ID] = s
	}
	snap := disp.Snapshot()
	for ch, c := range snap {
		if c.StreamID == nil {
			cfg.SetStream(ch, "")
			continue
		}
		key := c.Name
		if s, ok := byID[*c.StreamID]; ok {
			key = s.BindKey
		}
		cfg.SetStream(ch, key)
	}
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smc-mixer: "+format+"\n", args...)
	os.Exit(1)
}
