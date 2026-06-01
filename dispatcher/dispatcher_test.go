package dispatcher

import (
	"context"
	"testing"

	"github.com/yago/smc-mixer/midi"
)

// fakePW records the last SetVolume/SetMute call per stream ID.
type fakePW struct {
	volumes map[uint32]float64
	mutes   map[uint32]bool
}

func newFakePW() *fakePW {
	return &fakePW{volumes: make(map[uint32]float64), mutes: make(map[uint32]bool)}
}

func (f *fakePW) SetVolume(_ context.Context, id uint32, vol float64) error {
	f.volumes[id] = vol
	return nil
}

func (f *fakePW) SetMute(_ context.Context, id uint32, muted bool) error {
	f.mutes[id] = muted
	return nil
}

// send drives a single message through a fresh Run call.
func send(d *Dispatcher, msg midi.Msg) {
	ch := make(chan midi.Msg, 1)
	ch <- msg
	close(ch)
	d.Run(context.Background(), ch)
}

func approxEq(a, b float64) bool { return abs64(a-b) < 1e-9 }

func abs64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// — fader tests —

func TestFaderSetsVolume(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 42, "Firefox")

	send(d, midi.FaderMsg{Channel: 0, Value: 64})

	want := 64.0 / 127.0
	if !approxEq(pw.volumes[42], want) {
		t.Fatalf("SetVolume got %.9f, want %.9f", pw.volumes[42], want)
	}
	if !approxEq(d.Snapshot()[0].Volume, want) {
		t.Fatalf("snapshot volume = %.9f, want %.9f", d.Snapshot()[0].Volume, want)
	}
}

func TestFaderZeroAndMax(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 1, "test")

	send(d, midi.FaderMsg{Channel: 0, Value: 0})
	if pw.volumes[1] != 0.0 {
		t.Fatalf("value 0 → volume %.9f, want 0", pw.volumes[1])
	}

	send(d, midi.FaderMsg{Channel: 0, Value: 127})
	if !approxEq(pw.volumes[1], 1.0) {
		t.Fatalf("value 127 → volume %.9f, want 1.0", pw.volumes[1])
	}
}

func TestFaderUnboundUpdatesStateOnly(t *testing.T) {
	pw := newFakePW()
	d := New(pw)

	send(d, midi.FaderMsg{Channel: 0, Value: 100})

	if len(pw.volumes) != 0 {
		t.Fatal("unbound channel should not call SetVolume")
	}
	if !approxEq(d.Snapshot()[0].Volume, 100.0/127.0) {
		t.Fatal("state should update even when unbound")
	}
}

// — knob tests —

func TestKnobStartsAtCenter(t *testing.T) {
	d := New(newFakePW())
	for i, ch := range d.Snapshot() {
		if ch.Knob != 64 {
			t.Fatalf("channel %d knob = %d, want 64", i, ch.Knob)
		}
	}
}

func TestKnobAccumulates(t *testing.T) {
	d := New(newFakePW())
	for i := 0; i < 5; i++ {
		send(d, midi.KnobMsg{Channel: 1, Delta: +1})
	}
	if d.Snapshot()[1].Knob != 69 {
		t.Fatalf("knob = %d, want 69", d.Snapshot()[1].Knob)
	}
}

func TestKnobClampHigh(t *testing.T) {
	d := New(newFakePW())
	for i := 0; i < 200; i++ {
		send(d, midi.KnobMsg{Channel: 0, Delta: +1})
	}
	if d.Snapshot()[0].Knob != 127 {
		t.Fatalf("knob should clamp at 127, got %d", d.Snapshot()[0].Knob)
	}
}

func TestKnobClampLow(t *testing.T) {
	d := New(newFakePW())
	for i := 0; i < 200; i++ {
		send(d, midi.KnobMsg{Channel: 0, Delta: -1})
	}
	if d.Snapshot()[0].Knob != 0 {
		t.Fatalf("knob should clamp at 0, got %d", d.Snapshot()[0].Knob)
	}
}

// — button tests —

func TestMuteTogglesBoundStream(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(2, 99, "Spotify")

	send(d, midi.ButtonMsg{Channel: 2, Kind: midi.ButtonMute, Pressed: true})
	if !pw.mutes[99] {
		t.Fatal("expected muted after first press")
	}

	send(d, midi.ButtonMsg{Channel: 2, Kind: midi.ButtonMute, Pressed: true})
	if pw.mutes[99] {
		t.Fatal("expected unmuted after second press")
	}
}

func TestMuteReleaseIgnored(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 10, "test")

	send(d, midi.ButtonMsg{Channel: 0, Kind: midi.ButtonMute, Pressed: false})
	if _, called := pw.mutes[10]; called {
		t.Fatal("button release must not call SetMute")
	}
}

func TestMuteUnbound(t *testing.T) {
	pw := newFakePW()
	d := New(pw)

	send(d, midi.ButtonMsg{Channel: 0, Kind: midi.ButtonMute, Pressed: true})
	if len(pw.mutes) != 0 {
		t.Fatal("unbound mute should not call SetMute")
	}
	if !d.Snapshot()[0].Mute {
		t.Fatal("mute state should still toggle when unbound")
	}
}

func TestSoloToggle(t *testing.T) {
	d := New(newFakePW())

	send(d, midi.ButtonMsg{Channel: 3, Kind: midi.ButtonSolo, Pressed: true})
	if !d.Snapshot()[3].Solo {
		t.Fatal("expected Solo=true after press")
	}
	send(d, midi.ButtonMsg{Channel: 3, Kind: midi.ButtonSolo, Pressed: true})
	if d.Snapshot()[3].Solo {
		t.Fatal("expected Solo=false after second press")
	}
}

func TestRecToggle(t *testing.T) {
	d := New(newFakePW())
	send(d, midi.ButtonMsg{Channel: 5, Kind: midi.ButtonRec, Pressed: true})
	if !d.Snapshot()[5].Rec {
		t.Fatal("expected Rec=true")
	}
}

func TestStopToggle(t *testing.T) {
	d := New(newFakePW())
	send(d, midi.ButtonMsg{Channel: 7, Kind: midi.ButtonStop, Pressed: true})
	if !d.Snapshot()[7].Stop {
		t.Fatal("expected Stop=true")
	}
}

// — bind/unbind —

func TestUnbind(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 42, "Firefox")
	d.Unbind(0)

	send(d, midi.FaderMsg{Channel: 0, Value: 64})
	if len(pw.volumes) != 0 {
		t.Fatal("unbound after Unbind should not call SetVolume")
	}
}
