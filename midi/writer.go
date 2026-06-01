package midi

import (
	"fmt"
	"os"
	"sync"
)

// Writer sends LED feedback commands to the MIDI device.
type Writer struct {
	mu sync.Mutex
	f  *os.File
}

// OpenWriter opens the device at path for writing MIDI output.
func OpenWriter(path string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s for write: %w", path, err)
	}
	return &Writer{f: f}, nil
}

// Close closes the underlying device file.
func (w *Writer) Close() error {
	return w.f.Close()
}

// SetButtonLED turns a channel strip button LED on or off.
// Notes: rec=0+ch, solo=8+ch, mute=16+ch, stop=24+ch.
func (w *Writer) SetButtonLED(ch int, kind ButtonKind, on bool) {
	note := buttonNote(ch, kind)
	vel := byte(0x00)
	if on {
		vel = 0x7F
	}
	w.write([3]byte{0x90, note, vel})
}

// SetFaderLED sets the fader-top LED to blink (true) or off (false).
// The hardware has no solid-on state; threshold for blink is MSB ≥ 4.
func (w *Writer) SetFaderLED(ch int, blink bool) {
	msb := byte(0x00)
	if blink {
		msb = 0x04
	}
	w.write([3]byte{0xE0 + byte(ch), 0x00, msb})
}

// SetGlobalLED sets a transport button LED on or off.
// Only Play, Pause, Record, Previous, and Next have LEDs.
func (w *Writer) SetGlobalLED(action GlobalAction, on bool) {
	note, ok := globalNote(action)
	if !ok {
		return
	}
	vel := byte(0x00)
	if on {
		vel = 0x7F
	}
	w.write([3]byte{0x90, note, vel})
}

func globalNote(action GlobalAction) (byte, bool) {
	switch action {
	case ActionPrevious:
		return 91, true
	case ActionNext:
		return 92, true
	case ActionPause:
		return 93, true
	case ActionPlay:
		return 94, true
	case ActionRecord:
		return 95, true
	}
	return 0, false
}

func (w *Writer) write(b [3]byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.f.Write(b[:]) //nolint:errcheck
}

func buttonNote(ch int, kind ButtonKind) byte {
	switch kind {
	case ButtonRec:
		return byte(ch)
	case ButtonSolo:
		return byte(8 + ch)
	case ButtonMute:
		return byte(16 + ch)
	default: // ButtonStop
		return byte(24 + ch)
	}
}
