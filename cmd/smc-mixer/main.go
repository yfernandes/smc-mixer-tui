package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/ui"
)

func main() {
	cfgFlag := flag.String("config", "", "config file (default: "+config.DefaultPath()+")")
	flag.Parse()

	cfgPath := resolveConfigPath(*cfgFlag)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		die("load config: %v", err)
	}

	client, state, err := connectOrStartDaemon(cfgPath)
	if err != nil {
		die("%v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// reloadFn re-reads the config file on demand ('r' key). On error it returns
	// the last successfully loaded set of strip configs rather than wiping the display.
	// Prefer the config path reported by the daemon (it may have been started with
	// --config pointing to a different file than the TUI's own --config flag).
	reloadPath := cfgPath
	if state.ConfigPath != "" {
		reloadPath = state.ConfigPath
	}
	lastGood := computeStripConfigs(cfg)
	reloadFn := func() [8]ui.StripConfig {
		fresh, err := config.Load(reloadPath)
		if err != nil {
			return lastGood
		}
		lastGood = computeStripConfigs(fresh)
		return lastGood
	}

	program := tea.NewProgram(
		ui.New(client, state.Snapshot, state.Labels, state.Streams, lastGood, reloadFn),
		tea.WithAltScreen(),
	)
	client.SetProgram(program)
	go client.Run(ctx)

	if _, err := program.Run(); err != nil {
		die("UI: %v", err)
	}
	cancel()
}

// computeStripConfigs derives per-channel split info from the main page config.
// A strip is split when its fader and knob slots reference different device keys.
func computeStripConfigs(cfg *config.Config) [8]ui.StripConfig {
	var cfgs [8]ui.StripConfig
	if cfg.Pages == nil {
		return cfgs
	}
	mainPage, ok := cfg.Pages["main"]
	if !ok {
		return cfgs
	}
	for i := range 8 {
		faderKey := ""
		if k := mainPage.Faders[i]; k != nil {
			faderKey = *k
		}
		knobKey := ""
		if k := mainPage.Knobs[i]; k != nil {
			knobKey = *k
		}
		// Split whenever knob has a config device and the fader is either unset (dynamic)
		// or targets a different device. Same-device channels stay unified.
		if knobKey == "" || faderKey == knobKey {
			continue
		}
		cfgs[i].IsSplit = true
		if dev := cfg.DeviceFor(knobKey); dev != nil {
			cfgs[i].KnobLabel = dev.Label
			cfgs[i].KnobType = string(dev.Type)
		}
		// FaderLabel/FaderType are only set when the fader has a static config device.
		// An empty FaderType signals a dynamic fader whose zone uses runtime stream data.
		if faderKey != "" {
			if dev := cfg.DeviceFor(faderKey); dev != nil {
				cfgs[i].FaderLabel = dev.Label
				cfgs[i].FaderType = string(dev.Type)
			}
		}
	}
	return cfgs
}

func resolveConfigPath(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return config.DefaultPath()
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smc-mixer: "+format+"\n", args...)
	os.Exit(1)
}
