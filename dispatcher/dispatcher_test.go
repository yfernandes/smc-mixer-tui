package dispatcher

import (
	"context"
	"testing"

	"github.com/yfernandes/smc-mixer-tui/midi"
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

// fakeMPRIS records PlayPause calls.
type fakeMPRIS struct {
	calls []string
}

func (f *fakeMPRIS) PlayPause(_ context.Context, playerName string) error {
	f.calls = append(f.calls, playerName)
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

func TestFaderSetsVolumeAfterPickup(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 42, "Firefox", KindSource, "")

	want := 64.0 / 127.0
	// Simulate poll: actual volume is at 64/127.
	d.UpdateActualVolume(0, want)
	// Fader reaches the same position → picks up → calls SetVolume.
	send(d, midi.FaderMsg{Channel: 0, Value: 64})

	if !approxEq(pw.volumes[42], want) {
		t.Fatalf("SetVolume got %.9f, want %.9f", pw.volumes[42], want)
	}
	if !approxEq(d.Snapshot()[0].FaderPos, want) {
		t.Fatalf("FaderPos = %.9f, want %.9f", d.Snapshot()[0].FaderPos, want)
	}
	if !d.Snapshot()[0].Synced {
		t.Fatal("channel should be synced after pickup")
	}
}

func TestFaderNoCallBeforePickup(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 42, "Firefox", KindSource, "")
	// Actual volume at 80%; fader sent at 50% — too far, should NOT call SetVolume.
	d.UpdateActualVolume(0, 0.80)
	send(d, midi.FaderMsg{Channel: 0, Value: 64}) // 64/127 ≈ 0.504

	if _, called := pw.volumes[42]; called {
		t.Fatal("SetVolume must not be called before fader picks up actual volume")
	}
	if d.Snapshot()[0].Synced {
		t.Fatal("channel should not be synced when fader is far from actual volume")
	}
}

func TestFaderDesyncsOnExternalChange(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 42, "Firefox", KindSource, "")

	// Pick up at 0.5.
	d.UpdateActualVolume(0, 0.5)
	send(d, midi.FaderMsg{Channel: 0, Value: 64}) // ≈0.504, within threshold
	if !d.Snapshot()[0].Synced {
		t.Fatal("should be synced after pickup")
	}

	// External change: keyboard shortcut lowers volume significantly.
	d.UpdateActualVolume(0, 0.2)
	if d.Snapshot()[0].Synced {
		t.Fatal("external volume change should desync the channel")
	}
}

func TestFaderZeroAutoPickup(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 1, "test", KindSource, "")
	// Actual at 0; fader at 0 → auto-syncs (both at floor).
	d.UpdateActualVolume(0, 0.0)
	send(d, midi.FaderMsg{Channel: 0, Value: 0})
	if pw.volumes[1] != 0.0 {
		t.Fatalf("value 0 → volume %.9f, want 0", pw.volumes[1])
	}
}

func TestFaderMaxPickup(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 1, "test", KindSource, "")
	d.UpdateActualVolume(0, 1.0)
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
	if !approxEq(d.Snapshot()[0].FaderPos, 100.0/127.0) {
		t.Fatal("FaderPos should update even when unbound")
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
	d.Bind(2, 99, "Spotify", KindSource, "")

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
	d.Bind(0, 10, "test", KindSource, "")

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

func TestSoloMutesOtherSameKindChannels(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 10, "App1", KindSource, "")
	d.Bind(1, 20, "App2", KindSource, "")
	d.Bind(2, 30, "Mic1", KindMic, "")

	// Solo channel 0 (KindSource).
	send(d, midi.ButtonMsg{Channel: 0, Kind: midi.ButtonSolo, Pressed: true})

	// Channel 1 (same kind) must be muted; channel 2 (different kind) must not be touched.
	if !pw.mutes[20] {
		t.Fatal("channel 1 (same kind) must be muted when channel 0 is soloed")
	}
	if _, touched := pw.mutes[30]; touched {
		t.Fatal("channel 2 (different kind) must not be muted by solo")
	}
	if pw.mutes[10] {
		t.Fatal("soloed channel itself must not be muted")
	}
	if !d.Snapshot()[1].SoloMuted {
		t.Fatal("channel 1 SoloMuted should be true")
	}
}

func TestSoloUnsoloRestoresMute(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 10, "App1", KindSource, "")
	d.Bind(1, 20, "App2", KindSource, "")

	// Solo then unsolo channel 0.
	send(d, midi.ButtonMsg{Channel: 0, Kind: midi.ButtonSolo, Pressed: true})
	send(d, midi.ButtonMsg{Channel: 0, Kind: midi.ButtonSolo, Pressed: true})

	// Both channels should be unmuted (SoloMuted cleared).
	if pw.mutes[10] || pw.mutes[20] {
		t.Fatal("both channels must be unmuted after unsolo")
	}
	if d.Snapshot()[1].SoloMuted {
		t.Fatal("channel 1 SoloMuted should be cleared after unsolo")
	}
}

func TestSoloPreservesUserMute(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 10, "App1", KindSource, "")
	d.Bind(1, 20, "App2", KindSource, "")

	// Manually mute channel 1, then solo channel 0, then unsolo channel 0.
	send(d, midi.ButtonMsg{Channel: 1, Kind: midi.ButtonMute, Pressed: true})
	send(d, midi.ButtonMsg{Channel: 0, Kind: midi.ButtonSolo, Pressed: true})
	send(d, midi.ButtonMsg{Channel: 0, Kind: midi.ButtonSolo, Pressed: true})

	// Channel 1 is still user-muted.
	if !d.Snapshot()[1].Mute {
		t.Fatal("user mute on channel 1 must persist after solo cycle")
	}
	if !pw.mutes[20] {
		t.Fatal("channel 1 must remain muted (user mute) after solo cycle")
	}
}

func TestStopCallsMPRIS(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	m := &fakeMPRIS{}
	d.SetMPRISCaller(m)
	d.Bind(0, 10, "Spotify", KindSource, "Spotify")

	send(d, midi.ButtonMsg{Channel: 0, Kind: midi.ButtonStop, Pressed: true})

	if len(m.calls) != 1 || m.calls[0] != "Spotify" {
		t.Fatalf("expected PlayPause(Spotify), got %v", m.calls)
	}
}

func TestStopNoMPRISNoCall(t *testing.T) {
	d := New(newFakePW())
	d.Bind(0, 10, "App", KindSource, "") // no MPRIS name

	// Should not panic; no MPRIS caller set.
	send(d, midi.ButtonMsg{Channel: 0, Kind: midi.ButtonStop, Pressed: true})
}

// — crossfader tests —

// fakeCrossfader records SetGains calls for testing.
type fakeCrossfader struct {
	lastVolA, lastVolB float64
	calls              int
}

func (f *fakeCrossfader) SetGains(_ context.Context, volA, volB float64) error {
	f.lastVolA = volA
	f.lastVolB = volB
	f.calls++
	return nil
}

func TestCrossfaderKnobHardLeft(t *testing.T) {
	d := New(newFakePW())
	ctrl := &fakeCrossfader{}
	d.SetCrossfader(0, ctrl, "Speakers", "Headphones")

	// Knob to 0 → volA=1.0, volB=0.0
	for range 64 {
		send(d, midi.KnobMsg{Channel: 0, Delta: -1})
	}
	if !approxEq(ctrl.lastVolA, 1.0) {
		t.Fatalf("volA = %.4f, want 1.0 at knob=0", ctrl.lastVolA)
	}
	if !approxEq(ctrl.lastVolB, 0.0) {
		t.Fatalf("volB = %.4f, want 0.0 at knob=0", ctrl.lastVolB)
	}
}

func TestCrossfaderKnobHardRight(t *testing.T) {
	d := New(newFakePW())
	ctrl := &fakeCrossfader{}
	d.SetCrossfader(0, ctrl, "Speakers", "Headphones")

	// Knob to 127 → volA=0.0, volB=1.0
	for range 64 {
		send(d, midi.KnobMsg{Channel: 0, Delta: +1})
	}
	if !approxEq(ctrl.lastVolA, 0.0) {
		t.Fatalf("volA = %.4f, want 0.0 at knob=127", ctrl.lastVolA)
	}
	if !approxEq(ctrl.lastVolB, 1.0) {
		t.Fatalf("volB = %.4f, want 1.0 at knob=127", ctrl.lastVolB)
	}
}

func TestCrossfaderKnobCenterBothFull(t *testing.T) {
	d := New(newFakePW())
	ctrl := &fakeCrossfader{}
	d.SetCrossfader(0, ctrl, "Speakers", "Headphones")

	// Knob stays at center (64) → both ≈1.0
	send(d, midi.KnobMsg{Channel: 0, Delta: 0})
	if ctrl.lastVolA < 0.98 {
		t.Fatalf("volA = %.4f at center, want ≈1.0", ctrl.lastVolA)
	}
	if ctrl.lastVolB < 0.98 {
		t.Fatalf("volB = %.4f at center, want ≈1.0", ctrl.lastVolB)
	}
}

func TestCrossfaderNoCallWithoutController(t *testing.T) {
	pw := newFakePW()
	d := New(pw)

	// No crossfader configured — knob must not call SetVolume on PipeWire.
	send(d, midi.KnobMsg{Channel: 0, Delta: +10})
	if len(pw.volumes) != 0 {
		t.Fatal("knob without crossfader controller must not call SetVolume")
	}
}

func TestCrossfaderNoGlobalSinkTouch(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	ctrl := &fakeCrossfader{}
	d.SetCrossfader(0, ctrl, "Speakers", "Headphones")

	send(d, midi.KnobMsg{Channel: 0, Delta: +10})

	// The fake PipeWire must NOT receive any SetVolume calls — gains go via ctrl.
	if len(pw.volumes) != 0 {
		t.Fatal("crossfader must not touch global sink volumes via SetVolume")
	}
	if ctrl.calls == 0 {
		t.Fatal("crossfader controller SetGains was not called")
	}
}

// — bind/unbind —

func TestBindEvictsDuplicateStream(t *testing.T) {
	d := New(newFakePW())
	d.Bind(0, 42, "Firefox", KindSource, "")
	d.Bind(1, 42, "Firefox", KindSource, "") // same stream → channel 0 should be released

	snap := d.Snapshot()
	if snap[0].StreamID != nil {
		t.Fatal("channel 0 should be unbound after stream was claimed by channel 1")
	}
	if snap[1].StreamID == nil || *snap[1].StreamID != 42 {
		t.Fatal("channel 1 should hold the stream")
	}
}

func TestUnbind(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 42, "Firefox", KindSource, "")
	d.Unbind(0)

	send(d, midi.FaderMsg{Channel: 0, Value: 64})
	if len(pw.volumes) != 0 {
		t.Fatal("unbound after Unbind should not call SetVolume")
	}
}
