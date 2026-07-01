package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/yfernandes/smc-mixer-tui/daemon"
)

// acquireSingletonLock ensures exactly one smc-mixerd is running.
//
// Why this exists: the daemon is the sole driver of MIDI, PipeWire, and the
// crossfader modules. A second instance grabs the same MIDI device, steals the
// IPC socket (Server.Listen used to os.Remove it unconditionally), and runs
// CleanupCrossfaderTag — tearing down the first instance's crossfader graph.
// The symptom is a dead/erratic controller and a crossfader knob that stops
// working. See docs/DAEMON_AND_AUDIO.md ("Daemon stacking").
//
// The lock is a BSD flock on a file in XDG_RUNTIME_DIR. flock is released
// automatically when the process dies (crash, kill, or exit), so there is no
// stale-lock problem — unlike a PID file or the socket. The returned *os.File
// must be kept open for the daemon's whole lifetime; do not close it.
func acquireSingletonLock() (*os.File, error) {
	path := lockPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, errAlreadyRunning
		}
		return nil, fmt.Errorf("flock %s: %w", path, err)
	}
	return f, nil
}

// errAlreadyRunning signals that another smc-mixerd already holds the lock.
var errAlreadyRunning = fmt.Errorf("another smc-mixerd instance is already running")

// lockPath sits next to the IPC socket so both share the XDG_RUNTIME_DIR lifecycle.
func lockPath() string {
	return filepath.Join(filepath.Dir(daemon.SocketPath()), "smc-mixer.lock")
}
