package main

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/daemon"
)

var (
	connectDaemon          = daemon.Connect
	connectDaemonWithRetry = daemon.ConnectWithRetry
	startDaemonProcess     = startDaemon
)

func connectOrStartDaemon(cfgPath string) (*daemon.Client, daemon.InitialState, error) {
	client, state, err := connectDaemon()
	if err == nil {
		return client, state, nil
	}

	if startErr := startDaemonProcess(cfgPath); startErr != nil {
		return nil, daemon.InitialState{}, fmt.Errorf("start daemon: %w", startErr)
	}
	client, state, err = connectDaemonWithRetry(5 * time.Second)
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
	cmd := exec.Command(daemonBin, daemonArgs(cfgPath)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

func daemonArgs(cfgPath string) []string {
	if cfgPath == "" || cfgPath == config.DefaultPath() {
		return nil
	}
	return []string{"--config", cfgPath}
}
