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
	client, state, err := connectOrStartDaemon(cfgPath)
	if err != nil {
		die("%v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	program := tea.NewProgram(
		ui.New(client, state.Snapshot, state.Streams),
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
