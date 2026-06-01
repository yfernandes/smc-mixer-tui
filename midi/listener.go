package midi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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
func (l *Listener) Run(ctx context.Context, out chan<- Msg) error {
	f, err := os.Open(l.device)
	if err != nil {
		return fmt.Errorf("open %s: %w", l.device, err)
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
