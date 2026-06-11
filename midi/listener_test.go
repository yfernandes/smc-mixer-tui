package midi

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"
)

// pipeListener wraps readMIDI over an in-memory pipe so we can feed raw bytes
// without touching /dev/snd.
func pipeListener(data []byte) ([]Msg, error) {
	out := make(chan Msg, 32)
	err := readMIDI(bytes.NewReader(data), out)
	if err != nil && err != io.EOF {
		return nil, err
	}
	close(out)
	var msgs []Msg
	for m := range out {
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func TestReadMIDI_ThreeMessageTypes(t *testing.T) {
	raw := []byte{
		0x90, 0x10, 0x7f, // mute ch0 pressed
		0xe2, 0x00, 0x60, // fader ch2 at 0x60
		0xb0, 0x11, 0x01, // knob ch1 increment
	}
	msgs, err := pipeListener(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("want 3 msgs, got %d: %v", len(msgs), msgs)
	}
	if b, ok := msgs[0].(ButtonMsg); !ok || b.Kind != ButtonMute || b.Channel != 0 || !b.Pressed {
		t.Errorf("msg[0]: got %+v", msgs[0])
	}
	if f, ok := msgs[1].(FaderMsg); !ok || f.Channel != 2 || f.Value != uint16(0x60)<<7 {
		t.Errorf("msg[1]: got %+v", msgs[1])
	}
	if k, ok := msgs[2].(KnobMsg); !ok || k.Channel != 1 || k.Delta != 1 {
		t.Errorf("msg[2]: got %+v", msgs[2])
	}
}

func TestReadMIDI_RunningStatus(t *testing.T) {
	// Two NoteOn messages; second omits the status byte.
	raw := []byte{
		0x90, 0x00, 0x7f, // rec ch0 pressed (full)
		0x01, 0x7f, // rec ch1 pressed (running status)
	}
	msgs, err := pipeListener(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("want 2 msgs, got %d", len(msgs))
	}
	b0 := msgs[0].(ButtonMsg)
	b1 := msgs[1].(ButtonMsg)
	if b0.Channel != 0 || b1.Channel != 1 {
		t.Errorf("channels: got %d, %d", b0.Channel, b1.Channel)
	}
}

func TestReadMIDI_DataBeforeStatusIgnored(t *testing.T) {
	raw := []byte{
		0x00, 0x7f, // data bytes without running status
		0x90, 0x00, 0x7f, // rec ch0 pressed
	}
	msgs, err := pipeListener(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("want only the message after explicit status, got %d: %v", len(msgs), msgs)
	}
	b := msgs[0].(ButtonMsg)
	if b.Channel != 0 || b.Kind != ButtonRec || !b.Pressed {
		t.Fatalf("unexpected message: %+v", b)
	}
}

func TestReadMIDI_SysExClearsRunningStatus(t *testing.T) {
	raw := []byte{
		0x90, 0x00, 0x7f, // rec ch0 pressed
		0xf0, 0x41, 0xf7, // SysEx clears running status
		0x01, 0x7f, // would be rec ch1 if running status survived
		0x90, 0x02, 0x7f, // rec ch2 pressed
	}
	msgs, err := pipeListener(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("want 2 msgs, got %d: %v", len(msgs), msgs)
	}
	if b := msgs[0].(ButtonMsg); b.Channel != 0 {
		t.Fatalf("msg[0] channel = %d, want 0", b.Channel)
	}
	if b := msgs[1].(ButtonMsg); b.Channel != 2 {
		t.Fatalf("msg[1] channel = %d, want 2", b.Channel)
	}
}

func TestReadMIDI_RealTimeIgnored(t *testing.T) {
	// 0xf8 (MIDI Clock) injected between bytes of a NoteOn.
	raw := []byte{
		0x90, 0xf8, 0x10, 0xf8, 0x7f, // clock bytes interleaved
	}
	msgs, err := pipeListener(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("want 1 msg, got %d", len(msgs))
	}
	b := msgs[0].(ButtonMsg)
	if b.Channel != 0 || b.Kind != ButtonMute || !b.Pressed {
		t.Errorf("got %+v", b)
	}
}

func TestReadMIDI_SysExSkipped(t *testing.T) {
	raw := []byte{
		0xf0, 0x41, 0x10, 0xf7, // SysEx (discarded)
		0x90, 0x00, 0x7f, // rec ch0 pressed
	}
	msgs, err := pipeListener(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("want 1 msg, got %d", len(msgs))
	}
	if _, ok := msgs[0].(ButtonMsg); !ok {
		t.Errorf("expected ButtonMsg, got %T", msgs[0])
	}
}

func TestReadMIDI_UnmappedDropped(t *testing.T) {
	raw := []byte{
		0xb0, 0x08, 0x40, // CC 8 — not a knob, dropped
		0x90, 0x32, 0x7f, // note 50 — not a button, dropped
		0x90, 0x10, 0x7f, // mute ch0 — kept
	}
	msgs, err := pipeListener(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("want 1 msg, got %d: %v", len(msgs), msgs)
	}
}

func TestListener_ContextCancel(t *testing.T) {
	// Verify that readMIDI exits when the underlying file is closed (simulating
	// the goroutine that closes the file on ctx cancellation).
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal("pipe:", err)
	}
	defer pw.Close()

	out := make(chan Msg, 4)
	done := make(chan error, 1)
	go func() {
		done <- readMIDI(pr, out)
	}()

	// Closing the read end unblocks the blocked Read inside readMIDI.
	pr.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readMIDI did not return after file closed")
	}
}
