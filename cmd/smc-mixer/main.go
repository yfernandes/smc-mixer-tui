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
