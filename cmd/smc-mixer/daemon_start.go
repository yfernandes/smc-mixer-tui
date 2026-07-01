package main

import (
	"fmt"

	"github.com/yfernandes/smc-mixer-tui/daemon"
)

var connectDaemon = daemon.Connect

// connectToDaemon attaches to the already-running daemon. The TUI is a pure
// client: it never spawns a daemon of its own.
//
// Why not auto-spawn: the daemon is a long-running singleton driver (owns MIDI,
// PipeWire, the crossfader graph) and is meant to run under `systemctl --user`,
// independent of any client. A TUI-spawned daemon is detached (setsid), unmanaged
// by systemd, and used to collide with the service instance — two daemons fighting
// over the MIDI device, socket, and smc_* modules, which killed the controller and
// the crossfader. See docs/DAEMON_AND_AUDIO.md. A stray spawn is now also blocked
// by the daemon's singleton lock, but the TUI simply doesn't try.
func connectToDaemon() (*daemon.Client, daemon.InitialState, error) {
	client, state, err := connectDaemon()
	if err != nil {
		return nil, daemon.InitialState{}, fmt.Errorf(
			"no smc-mixerd daemon running: %w\n"+
				"start it with:  systemctl --user start smc-mixer.service", err)
	}
	return client, state, nil
}
