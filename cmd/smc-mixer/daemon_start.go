package main

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/daemon"
)

func connectOrStartDaemon(cfgPath string) (*daemon.Client, daemon.InitialState, error) {
	client, state, err := daemon.Connect()
	if err == nil {
		return client, state, nil
	}

	if startErr := startDaemon(cfgPath); startErr != nil {
		return nil, daemon.InitialState{}, fmt.Errorf("start daemon: %w", startErr)
	}
	client, state, err = daemon.ConnectWithRetry(5 * time.Second)
	if err != nil {
		return nil, daemon.InitialState{}, fmt.Errorf("connect to daemon: %w", err)
	}
	return client, state, nil
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
