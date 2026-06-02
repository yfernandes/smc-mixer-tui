package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/daemon"
	"github.com/yfernandes/smc-mixer-tui/ui"
)

func main() {
	cfgFlag := flag.String("config", "", "config file (default: "+config.DefaultPath()+")")
	flag.Parse()

	cfgPath := *cfgFlag
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}

	client, state, err := daemon.Connect()
	if err != nil {
		if startErr := startDaemon(cfgPath); startErr != nil {
			die("start daemon: %v", startErr)
		}
		client, state, err = daemon.ConnectWithRetry(5 * time.Second)
		if err != nil {
			die("connect to daemon: %v", err)
		}
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

// startDaemon launches smc-mixerd as a detached background process.
func startDaemon(cfgPath string) error {
	daemonBin, err := exec.LookPath("smc-mixerd")
	if err != nil {
		return fmt.Errorf("smc-mixerd not found in PATH: %w", err)
	}
	args := []string{}
	if cfgPath != config.DefaultPath() {
		args = append(args, "--config", cfgPath)
	}
	cmd := exec.Command(daemonBin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smc-mixer: "+format+"\n", args...)
	os.Exit(1)
}
