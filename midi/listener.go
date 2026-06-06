package midi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

// Listener reads raw MIDI bytes from an ALSA raw MIDI device file and emits
// classified Msgs into a channel.
type Listener struct {
	device string
}

// NewListener creates a Listener for the given device path (e.g. "/dev/snd/midiC1D0").
func NewListener(device string) *Listener {
	return &Listener{device: device}
}

// Run opens the device and pushes classified messages into out until ctx is
// cancelled or the device closes. Blocks; launch in a goroutine.
//
// The ALSA rawmidi kernel buffer persists across close/reopen cycles, so any
// events buffered from a previous session are drained before the normal read
// loop begins. This prevents stale button presses (e.g. Play/Pause pressed
// while the daemon was down) from being replayed as if they just happened.
func (l *Listener) Run(ctx context.Context, out chan<- Msg) error {
	f, err := os.OpenFile(l.device, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", l.device, err)
	}

	// Drain stale input. Read and discard until the non-blocking file returns
	// an error (EAGAIN when empty, or any other I/O error).
	drain := make([]byte, 64)
	for {
		if _, err := f.Read(drain); err != nil {
			break
		}
	}

	// Switch to blocking mode for the normal read loop.
	if err := syscall.SetNonblock(int(f.Fd()), false); err != nil {
		f.Close()
		return fmt.Errorf("setnonblock %s: %w", l.device, err)
	}

	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			f.Close()
		case <-stop:
		}
	}()

	err = readMIDI(f, out)
	close(stop)

	if isClosedErr(err) || errors.Is(err, io.EOF) {
		return ctx.Err()
	}
	return err
}
