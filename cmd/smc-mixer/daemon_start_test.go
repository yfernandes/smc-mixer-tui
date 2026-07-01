package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/yfernandes/smc-mixer-tui/daemon"
)

func TestConnectToDaemonUsesExistingDaemon(t *testing.T) {
	old := connectDaemon
	defer func() { connectDaemon = old }()

	connectDaemon = func() (*daemon.Client, daemon.InitialState, error) {
		return nil, daemon.InitialState{ConfigPath: "already-running"}, nil
	}

	_, state, err := connectToDaemon()
	if err != nil {
		t.Fatalf("connectToDaemon() error = %v", err)
	}
	if state.ConfigPath != "already-running" {
		t.Fatalf("ConfigPath = %q, want existing daemon state", state.ConfigPath)
	}
}

func TestConnectToDaemonReportsMissingDaemon(t *testing.T) {
	old := connectDaemon
	defer func() { connectDaemon = old }()

	connectDaemon = func() (*daemon.Client, daemon.InitialState, error) {
		return nil, daemon.InitialState{}, errors.New("dial unix: connection refused")
	}

	_, _, err := connectToDaemon()
	if err == nil {
		t.Fatal("connectToDaemon() error = nil, want error when no daemon is running")
	}
	// The TUI must not spawn a daemon; it should point the user at the service.
	if !strings.Contains(err.Error(), "systemctl --user start smc-mixer.service") {
		t.Fatalf("error = %v, want guidance to start the service", err)
	}
}
