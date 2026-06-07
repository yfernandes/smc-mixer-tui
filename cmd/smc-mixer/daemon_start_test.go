package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/daemon"
)

func TestConnectOrStartDaemonUsesExistingDaemon(t *testing.T) {
	restore := stubDaemonStartDeps(t)
	defer restore()

	starts := 0
	connectDaemon = func() (*daemon.Client, daemon.InitialState, error) {
		return nil, daemon.InitialState{ConfigPath: "already-running"}, nil
	}
	startDaemonProcess = func(string) error {
		starts++
		return nil
	}

	_, state, err := connectOrStartDaemon("config.yaml")

	if err != nil {
		t.Fatalf("connectOrStartDaemon() error = %v", err)
	}
	if state.ConfigPath != "already-running" {
		t.Fatalf("ConfigPath = %q, want existing daemon state", state.ConfigPath)
	}
	if starts != 0 {
		t.Fatalf("startDaemon called %d times, want 0", starts)
	}
}

func TestConnectOrStartDaemonStartsAndRetries(t *testing.T) {
	restore := stubDaemonStartDeps(t)
	defer restore()

	startedWith := ""
	retryTimeout := time.Duration(0)
	connectDaemon = func() (*daemon.Client, daemon.InitialState, error) {
		return nil, daemon.InitialState{}, errors.New("not running")
	}
	startDaemonProcess = func(path string) error {
		startedWith = path
		return nil
	}
	connectDaemonWithRetry = func(timeout time.Duration) (*daemon.Client, daemon.InitialState, error) {
		retryTimeout = timeout
		return nil, daemon.InitialState{ConfigPath: "after-start"}, nil
	}

	_, state, err := connectOrStartDaemon("custom.yaml")

	if err != nil {
		t.Fatalf("connectOrStartDaemon() error = %v", err)
	}
	if startedWith != "custom.yaml" {
		t.Fatalf("daemon started with %q, want custom.yaml", startedWith)
	}
	if retryTimeout != 5*time.Second {
		t.Fatalf("retry timeout = %s, want 5s", retryTimeout)
	}
	if state.ConfigPath != "after-start" {
		t.Fatalf("ConfigPath = %q, want retry state", state.ConfigPath)
	}
}

func TestConnectOrStartDaemonReportsStartFailure(t *testing.T) {
	restore := stubDaemonStartDeps(t)
	defer restore()

	connectDaemon = func() (*daemon.Client, daemon.InitialState, error) {
		return nil, daemon.InitialState{}, errors.New("not running")
	}
	startDaemonProcess = func(string) error {
		return errors.New("boom")
	}

	_, _, err := connectOrStartDaemon("config.yaml")

	if err == nil || !strings.Contains(err.Error(), "start daemon") {
		t.Fatalf("error = %v, want wrapped start daemon error", err)
	}
}

func TestConnectOrStartDaemonReportsRetryFailure(t *testing.T) {
	restore := stubDaemonStartDeps(t)
	defer restore()

	connectDaemon = func() (*daemon.Client, daemon.InitialState, error) {
		return nil, daemon.InitialState{}, errors.New("not running")
	}
	startDaemonProcess = func(string) error { return nil }
	connectDaemonWithRetry = func(time.Duration) (*daemon.Client, daemon.InitialState, error) {
		return nil, daemon.InitialState{}, errors.New("still unavailable")
	}

	_, _, err := connectOrStartDaemon("config.yaml")

	if err == nil || !strings.Contains(err.Error(), "connect to daemon") {
		t.Fatalf("error = %v, want wrapped retry error", err)
	}
}

func TestDaemonArgs(t *testing.T) {
	cases := []struct {
		name string
		path string
		want []string
	}{
		{name: "empty", path: "", want: nil},
		{name: "default", path: config.DefaultPath(), want: nil},
		{name: "custom", path: "/tmp/smc.yaml", want: []string{"--config", "/tmp/smc.yaml"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := daemonArgs(tc.path); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("daemonArgs(%q) = %#v, want %#v", tc.path, got, tc.want)
			}
		})
	}
}

func stubDaemonStartDeps(t *testing.T) func() {
	t.Helper()
	oldConnect := connectDaemon
	oldRetry := connectDaemonWithRetry
	oldStart := startDaemonProcess
	connectDaemonWithRetry = func(time.Duration) (*daemon.Client, daemon.InitialState, error) {
		t.Fatal("connectDaemonWithRetry called unexpectedly")
		return nil, daemon.InitialState{}, nil
	}
	return func() {
		connectDaemon = oldConnect
		connectDaemonWithRetry = oldRetry
		startDaemonProcess = oldStart
	}
}
