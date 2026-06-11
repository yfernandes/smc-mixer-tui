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

// ClearLEDs turns off all button LEDs and global transport LEDs.
// Call before closing to leave the hardware in a clean state.
// Fader channels (0xE0–0xE7) are intentionally not touched: sending pitch-bend
// messages as a "clear" would move the motorized faders and trigger the sync LED.
func (w *Writer) ClearLEDs() {
	for ch := range 8 {
		for _, kind := range []ButtonKind{ButtonRec, ButtonSolo, ButtonMute, ButtonStop} {
			w.SetButtonLED(ch, kind, false)
		}
	}
	for _, action := range []GlobalAction{ActionPrevious, ActionNext, ActionPause, ActionPlay, ActionRecord} {
		w.SetGlobalLED(action, false)
	}
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

// SetFaderPosition moves the motorized fader to vol (0.0–1.0) using full 14-bit resolution.
func (w *Writer) SetFaderPosition(ch int, vol float64) {
	if vol < 0 {
		vol = 0
	} else if vol > 1 {
		vol = 1
	}
	w.writeFader(ch, uint16(vol*16383))
}

// writeFader is the sole path that may emit pitch-bend messages (0xE0–0xE7).
// raw is a 14-bit position value (0–16383); the MSB is always derived from it,
// so there is no way to inject the hardware fader-lock command (MSB=0x04, LSB=0x00).
func (w *Writer) writeFader(ch int, raw uint16) {
	lsb := byte(raw & 0x7F)
	msb := byte(raw >> 7)
	w.writeRaw([3]byte{0xE0 + byte(ch), lsb, msb})
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
	if b[0] >= 0xE0 && b[0] <= 0xE7 {
		panic(fmt.Sprintf("midi: use writeFader for pitch-bend channel 0x%02X", b[0]))
	}
	w.writeRaw(b)
}

func (w *Writer) writeRaw(b [3]byte) {
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
