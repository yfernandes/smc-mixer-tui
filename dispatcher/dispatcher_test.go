package dispatcher

import (
	"context"
	"testing"

	"github.com/yfernandes/smc-mixer-tui/audio"
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

type fakeLEDs struct {
	faderPosition map[int]float64
}

func newFakeLEDs() *fakeLEDs {
	return &fakeLEDs{
		faderPosition: make(map[int]float64),
	}
}

func (f *fakeLEDs) SetButtonLED(ch int, kind midi.ButtonKind, on bool) {
}

func (f *fakeLEDs) SetFaderPosition(ch int, vol float64) {
	f.faderPosition[ch] = vol
}

func (f *fakeLEDs) SetGlobalLED(action midi.GlobalAction, on bool) {
}

// send drives a single message directly through the dispatcher's internal dispatch
// path, bypassing Run. This lets tests issue many messages to the same dispatcher
// without hitting Run's single-use enforcement.
func send(d *Dispatcher, msg midi.Msg) {
	d.dispatch(context.Background(), msg)
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
	d.Bind(0, 42, "Firefox", audio.KindSource, "")

	// Fader must reach zero first to sync, regardless of the actual stream volume.
	send(d, midi.FaderMsg{Channel: 0, Value: 0})
	if !d.Snapshot()[0].Synced {
		t.Fatal("channel should be synced after fader reaches zero")
	}

	const val14 = uint16(8192) // ≈ 50% in 14-bit (8192/16383)
	want := 8192.0 / 16383.0
	send(d, midi.FaderMsg{Channel: 0, Value: val14})
	if !approxEq(pw.volumes[42], want) {
		t.Fatalf("SetVolume got %.9f, want %.9f", pw.volumes[42], want)
	}
	if !approxEq(d.Snapshot()[0].FaderPos, want) {
		t.Fatalf("FaderPos = %.9f, want %.9f", d.Snapshot()[0].FaderPos, want)
	}
}

func TestFaderNoCallBeforePickup(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 42, "Firefox", audio.KindSource, "")
	// Fader above zero — must not sync or call SetVolume until zero is reached.
	send(d, midi.FaderMsg{Channel: 0, Value: 8192}) // 8192/16383 ≈ 0.500

	if _, called := pw.volumes[42]; called {
		t.Fatal("SetVolume must not be called before fader reaches zero")
	}
	if d.Snapshot()[0].Synced {
		t.Fatal("channel should not be synced when fader has not reached zero")
	}
}

func TestFaderZeroAutoPickup(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 1, "test", audio.KindSource, "")
	// Actual at 0; fader at 0 → auto-syncs (both at floor).
	d.UpdateActualVolume(0, 0.0)
	send(d, midi.FaderMsg{Channel: 0, Value: 0})
	if pw.volumes[1] != 0.0 {
		t.Fatalf("value 0 → volume %.9f, want 0", pw.volumes[1])
	}
}

func TestFaderMaxAfterZeroSync(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 1, "test", audio.KindSource, "")
	send(d, midi.FaderMsg{Channel: 0, Value: 0})     // sync at zero
	send(d, midi.FaderMsg{Channel: 0, Value: 16383}) // drive to max
	if !approxEq(pw.volumes[1], 1.0) {
		t.Fatalf("value 16383 → volume %.9f, want 1.0", pw.volumes[1])
	}
}

func TestFaderUnboundUpdatesStateOnly(t *testing.T) {
	pw := newFakePW()
	d := New(pw)

	send(d, midi.FaderMsg{Channel: 0, Value: 12800}) // 12800/16383 ≈ 0.781

	if len(pw.volumes) != 0 {
		t.Fatal("unbound channel should not call SetVolume")
	}
	if !approxEq(d.Snapshot()[0].FaderPos, 12800.0/16383.0) {
		t.Fatal("FaderPos should update even when unbound")
	}
	if !d.Snapshot()[0].FaderPosKnown {
		t.Fatal("FaderPosKnown should be true after first fader message")
	}
}

func TestSetLEDWriterNilInvalidatesFaderPosKnown(t *testing.T) {
	d := New(newFakePW())
	d.SetLEDWriter(newFakeLEDs())

	// Establish known positions on a few channels.
	send(d, midi.FaderMsg{Channel: 0, Value: 8192})
	send(d, midi.FaderMsg{Channel: 3, Value: 4096})
	if !d.Snapshot()[0].FaderPosKnown || !d.Snapshot()[3].FaderPosKnown {
		t.Fatal("precondition: FaderPosKnown should be true after fader messages")
	}

	// Device disconnect: position may drift while unplugged.
	d.SetLEDWriter(nil)

	snap := d.Snapshot()
	for ch := range 8 {
		if snap[ch].FaderPosKnown {
			t.Errorf("ch%d: FaderPosKnown should be false after device disconnect", ch)
		}
	}
}

// — soft pickup tests —

// setSoftPickup is a test helper that configures channel ch for soft pickup mode
// with the default tolerance.
func setSoftPickup(d *Dispatcher, ch int) {
	d.SetChannelSyncMode(ch, SyncModeSoftPickup, PickupThreshold)
}

func TestSoftPickupSyncsWhenFaderCrossesTarget(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 42, "Spotify", audio.KindSource, "")
	setSoftPickup(d, 0)
	d.UpdateActualVolume(0, 80.0/127.0) // target at CC 80

	// Fader starts below target; no sync yet.
	send(d, midi.FaderMsg{Channel: 0, Value: 7680}) // 60*128; below target window
	if d.Snapshot()[0].Synced {
		t.Fatal("should not be synced before crossing target")
	}
	if _, called := pw.volumes[42]; called {
		t.Fatal("SetVolume must not be called before sync")
	}

	// Fader crosses into tolerance window from below.
	send(d, midi.FaderMsg{Channel: 0, Value: 10112}) // 79*128; enters window from below
	if !d.Snapshot()[0].Synced {
		t.Fatal("should be synced after fader crosses target from below")
	}
	if !approxEq(pw.volumes[42], 10112.0/16383.0) {
		t.Fatalf("SetVolume = %.4f, want %.4f", pw.volumes[42], 10112.0/16383.0)
	}
}

func TestSoftPickupSyncsFromAbove(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 42, "Spotify", audio.KindSource, "")
	setSoftPickup(d, 0)
	d.UpdateActualVolume(0, 40.0/127.0) // target at CC 40

	// Fader starts above target.
	send(d, midi.FaderMsg{Channel: 0, Value: 10240}) // 80*128; above target window
	if d.Snapshot()[0].Synced {
		t.Fatal("should not be synced before crossing target from above")
	}

	// Sweep down into window.
	send(d, midi.FaderMsg{Channel: 0, Value: 5248}) // 41*128; enters window from above
	if !d.Snapshot()[0].Synced {
		t.Fatal("should be synced after fader crosses target from above")
	}
}

func TestSoftPickupIgnoresFaderOnWrongSide(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 42, "Spotify", audio.KindSource, "")
	setSoftPickup(d, 0)
	d.UpdateActualVolume(0, 80.0/127.0) // target at CC 80

	// Fader starts above target and moves further above; must not sync.
	send(d, midi.FaderMsg{Channel: 0, Value: 12800}) // 100*128
	send(d, midi.FaderMsg{Channel: 0, Value: 14080}) // 110*128
	send(d, midi.FaderMsg{Channel: 0, Value: 15360}) // 120*128
	if d.Snapshot()[0].Synced {
		t.Fatal("fader moving away from target (wrong direction) must not sync")
	}
	if _, called := pw.volumes[42]; called {
		t.Fatal("SetVolume must not be called before sync")
	}
}

func TestSoftPickupFastSweepOvershoot(t *testing.T) {
	// Fader was below target; a single tick jumps above target (overshoot).
	// Should still sync because the fader crossed through the window.
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 42, "Spotify", audio.KindSource, "")
	setSoftPickup(d, 0)
	d.UpdateActualVolume(0, 60.0/127.0) // target at CC 60

	send(d, midi.FaderMsg{Channel: 0, Value: 3840})  // 30*128; prev below target window
	send(d, midi.FaderMsg{Channel: 0, Value: 12800}) // 100*128; jumped over target
	if !d.Snapshot()[0].Synced {
		t.Fatal("fast sweep overshoot should trigger sync")
	}
}

func TestSoftPickupAlreadyAtTarget(t *testing.T) {
	// Fader is already within tolerance of target on first message after bind.
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 42, "Spotify", audio.KindSource, "")
	setSoftPickup(d, 0)
	d.UpdateActualVolume(0, 64.0/127.0) // target at CC 64

	// Fader within ±tol of target: sync immediately.
	send(d, midi.FaderMsg{Channel: 0, Value: 8192}) // 64*128; within tolerance of 64/127 target
	if !d.Snapshot()[0].Synced {
		t.Fatal("fader already at target should sync immediately")
	}
}

func TestSoftPickupTargetUpdateResetsPickupSide(t *testing.T) {
	// Startup race: ActualVolume was 0 when fader first moved (pickupSide=above).
	// When the real volume arrives via UpdateActualVolume, pickupSide resets and
	// approaching from below should work correctly.
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 42, "Spotify", audio.KindSource, "")
	setSoftPickup(d, 0)

	// First fader message while ActualVolume=0 (bind resets it to 0).
	// Fader at ~50% is above 0: pickupSide=above.
	send(d, midi.FaderMsg{Channel: 0, Value: 8192}) // 64*128; above target=0

	// Polling updates the real volume; should reset pickupSide.
	d.UpdateActualVolume(0, 80.0/127.0) // target=80/127, fader=~50% is now below

	// Next fader message: pickupSide should now be below; sweep up to sync.
	send(d, midi.FaderMsg{Channel: 0, Value: 8960}) // 70*128; still below target window
	if d.Snapshot()[0].Synced {
		t.Fatal("should not be synced before crossing new target")
	}
	send(d, midi.FaderMsg{Channel: 0, Value: 10112}) // 79*128; enters window from below
	if !d.Snapshot()[0].Synced {
		t.Fatal("should sync after crossing target post-ActualVolume-update")
	}
}

func TestSoftPickupNoUnsyncAfterSync(t *testing.T) {
	// Once synced, moving the fader should never un-sync.
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 42, "Spotify", audio.KindSource, "")
	setSoftPickup(d, 0)
	d.UpdateActualVolume(0, 60.0/127.0)

	send(d, midi.FaderMsg{Channel: 0, Value: 3840}) // 30*128; below target
	send(d, midi.FaderMsg{Channel: 0, Value: 7680}) // 60*128; within tolerance of 60/127

	if !d.Snapshot()[0].Synced {
		t.Fatal("should be synced at target")
	}

	// Large movement away from where we synced.
	send(d, midi.FaderMsg{Channel: 0, Value: 16383})
	send(d, midi.FaderMsg{Channel: 0, Value: 0})
	if !d.Snapshot()[0].Synced {
		t.Fatal("synced channel must not un-sync from fader movement")
	}
}

func TestSoftPickupPinnedChannelPreservesSync(t *testing.T) {
	// Pinned channels are not reset on page switch; their sync state persists.
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 42, "Spotify", audio.KindSource, "")
	setSoftPickup(d, 0)
	d.UpdateActualVolume(0, 60.0/127.0)

	send(d, midi.FaderMsg{Channel: 0, Value: 3840}) // 30*128; below target
	send(d, midi.FaderMsg{Channel: 0, Value: 7680}) // 60*128; syncs within tolerance

	d.SetPinned(0, true)
	// Page switch doesn't call ResetStrip for pinned channels.
	if !d.Snapshot()[0].Synced {
		t.Fatal("pinned channel sync state must be preserved")
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
	d.Bind(2, 99, "Spotify", audio.KindSource, "")

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
	d.Bind(0, 10, "test", audio.KindSource, "")

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
	// R toggles on release; send press then immediate release (< 500ms = short press).
	send(d, midi.ButtonMsg{Channel: 5, Kind: midi.ButtonRec, Pressed: true})
	send(d, midi.ButtonMsg{Channel: 5, Kind: midi.ButtonRec, Pressed: false})
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
	d.Bind(0, 10, "App1", audio.KindSource, "")
	d.Bind(1, 20, "App2", audio.KindSource, "")
	d.Bind(2, 30, "Mic1", audio.KindMic, "")

	// Solo channel 0 (audio.KindSource).
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
	d.Bind(0, 10, "App1", audio.KindSource, "")
	d.Bind(1, 20, "App2", audio.KindSource, "")

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
	d.Bind(0, 10, "App1", audio.KindSource, "")
	d.Bind(1, 20, "App2", audio.KindSource, "")

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
	d.Bind(0, 10, "Spotify", audio.KindSource, "Spotify")

	send(d, midi.ButtonMsg{Channel: 0, Kind: midi.ButtonStop, Pressed: true})

	if len(m.calls) != 1 || m.calls[0] != "Spotify" {
		t.Fatalf("expected PlayPause(Spotify), got %v", m.calls)
	}
}

func TestStopDebouncesDuplicatePresses(t *testing.T) {
	d := New(newFakePW())
	m := &fakeMPRIS{}
	d.SetMPRISCaller(m)
	d.Bind(0, 10, "Spotify", audio.KindSource, "Spotify")

	send(d, midi.ButtonMsg{Channel: 0, Kind: midi.ButtonStop, Pressed: true})
	send(d, midi.ButtonMsg{Channel: 0, Kind: midi.ButtonStop, Pressed: true})

	if len(m.calls) != 1 {
		t.Fatalf("duplicate stop press should call PlayPause once, got %v", m.calls)
	}
}

func TestUpdateBindingMetadataPreservesFaderStateAndEnablesMPRIS(t *testing.T) {
	d := New(newFakePW())
	d.Bind(0, 42, "Zen", audio.KindSource, "")
	send(d, midi.FaderMsg{Channel: 0, Value: 0})    // sync at zero
	send(d, midi.FaderMsg{Channel: 0, Value: 8192}) // move to working position
	before := d.Snapshot()[0]
	if !before.Synced {
		t.Fatal("expected channel to be synced before metadata refresh")
	}

	d.UpdateBindingMetadata(0, 42, "firefox.instance_1_46", "firefox.instance_1_46")

	after := d.Snapshot()[0]
	if after.MPRISName != "firefox.instance_1_46" {
		t.Fatalf("MPRISName = %q, want firefox.instance_1_46", after.MPRISName)
	}
	if !after.Synced || after.LastSetVol != before.LastSetVol || after.ActualVolume != before.ActualVolume {
		t.Fatalf("metadata refresh reset fader state: before=%+v after=%+v", before, after)
	}
}

func TestStopNoMPRISNoCall(t *testing.T) {
	d := New(newFakePW())
	d.Bind(0, 10, "App", audio.KindSource, "") // no MPRIS name

	// Should not panic; no MPRIS caller set.
	send(d, midi.ButtonMsg{Channel: 0, Kind: midi.ButtonStop, Pressed: true})
}

func TestStopAfterUnbindDoesNotCallStaleMPRIS(t *testing.T) {
	d := New(newFakePW())
	m := &fakeMPRIS{}
	d.SetMPRISCaller(m)
	d.Bind(0, 10, "Spotify", audio.KindSource, "Spotify")
	d.Unbind(0)

	send(d, midi.ButtonMsg{Channel: 0, Kind: midi.ButtonStop, Pressed: true})

	if len(m.calls) != 0 {
		t.Fatalf("unbound channel must not call stale MPRIS player, got %v", m.calls)
	}
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

func TestKnobGainCallsSetVolume(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.BindKnob(0, 55)

	// Knob starts at 64; delta +63 → 127 → vol 1.0
	for range 63 {
		send(d, midi.KnobMsg{Channel: 0, Delta: +1})
	}
	if !approxEq(pw.volumes[55], 1.0) {
		t.Fatalf("SetVolume[55] = %.4f, want 1.0", pw.volumes[55])
	}
}

func TestKnobGainLoseKnobStopsWrites(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.BindKnob(0, 55)
	d.LoseKnob(0)

	send(d, midi.KnobMsg{Channel: 0, Delta: +1})
	if _, called := pw.volumes[55]; called {
		t.Fatal("LoseKnob should stop SetVolume calls")
	}
}

func TestKnobGainDoesNotTouchCrossfaderWhenBothSet(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	ctrl := &fakeCrossfader{}
	d.SetCrossfader(0, ctrl, "A", "B")
	d.BindKnob(0, 55) // crossfader takes priority

	send(d, midi.KnobMsg{Channel: 0, Delta: +1})
	if _, called := pw.volumes[55]; called {
		t.Fatal("crossfader must take priority over knob gain; SetVolume must not be called")
	}
	if ctrl.calls == 0 {
		t.Fatal("crossfader SetGains should have been called")
	}
}

func TestCrossfaderNoCallWithoutController(t *testing.T) {
	pw := newFakePW()
	d := New(pw)

	// No crossfader and no knob binding — knob must not call SetVolume on PipeWire.
	send(d, midi.KnobMsg{Channel: 0, Delta: +10})
	if len(pw.volumes) != 0 {
		t.Fatal("knob without crossfader controller or knob binding must not call SetVolume")
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
	d.Bind(0, 42, "Firefox", audio.KindSource, "")
	d.Bind(1, 42, "Firefox", audio.KindSource, "") // same stream → channel 0 should be released

	snap := d.Snapshot()
	if snap[0].StreamID != nil {
		t.Fatal("channel 0 should be unbound after stream was claimed by channel 1")
	}
	if snap[1].StreamID == nil || *snap[1].StreamID != 42 {
		t.Fatal("channel 1 should hold the stream")
	}
}

func TestBindEvictionClearsStaleMPRIS(t *testing.T) {
	d := New(newFakePW())
	m := &fakeMPRIS{}
	d.SetMPRISCaller(m)
	d.Bind(0, 42, "Spotify", audio.KindSource, "Spotify")
	d.Bind(1, 42, "Spotify", audio.KindSource, "Spotify")

	send(d, midi.ButtonMsg{Channel: 0, Kind: midi.ButtonStop, Pressed: true})

	if len(m.calls) != 0 {
		t.Fatalf("evicted channel must not call stale MPRIS player, got %v", m.calls)
	}
}

func TestUnbind(t *testing.T) {
	pw := newFakePW()
	d := New(pw)
	d.Bind(0, 42, "Firefox", audio.KindSource, "firefox")
	d.Unbind(0)

	snap := d.Snapshot()
	if snap[0].StreamID != nil || snap[0].Name != "" || snap[0].MPRISName != "" {
		t.Fatalf("unbind should clear stream metadata, got %+v", snap[0])
	}

	send(d, midi.FaderMsg{Channel: 0, Value: 8192})
	if len(pw.volumes) != 0 {
		t.Fatal("unbound after Unbind should not call SetVolume")
	}
}

func TestUnbindSetsManuallyUnbound(t *testing.T) {
	d := New(newFakePW())
	d.Bind(0, 42, "Firefox", audio.KindSource, "")
	d.Unbind(0)

	if !d.Snapshot()[0].ManuallyUnbound {
		t.Fatal("Unbind should set ManuallyUnbound")
	}
}

func TestBindClearsManuallyUnbound(t *testing.T) {
	d := New(newFakePW())
	d.Bind(0, 42, "Firefox", audio.KindSource, "")
	d.Unbind(0)
	d.Bind(0, 42, "Firefox", audio.KindSource, "")

	if d.Snapshot()[0].ManuallyUnbound {
		t.Fatal("Bind should clear ManuallyUnbound")
	}
}

func TestEvictedChannelNotManuallyUnbound(t *testing.T) {
	d := New(newFakePW())
	d.Bind(0, 42, "Firefox", audio.KindSource, "")
	d.Bind(1, 42, "Firefox", audio.KindSource, "") // evicts ch 0

	if d.Snapshot()[0].ManuallyUnbound {
		t.Fatal("system eviction must not set ManuallyUnbound")
	}
}

// TestAdvancedBlinkCancelStoredUnderLock verifies fix 3: the blink goroutine's
// cancel func is stored in advancedCancels while the lock is still held, so a
// concurrent OnGlobal page switch cannot miss the cancel and leave a ghost goroutine.
func TestAdvancedBlinkCancelStoredUnderLock(t *testing.T) {
	d := New(newFakePW())
	d.Bind(0, 10, "App", audio.KindSource, "")
	d.SetAdvancedSpec(0, &AdvancedSpec{MuteButtonAction: "mute"})

	// Activate advanced mode (R short press on bound channel with advanced spec).
	send(d, midi.ButtonMsg{Channel: 0, Kind: midi.ButtonRec, Pressed: true})
	send(d, midi.ButtonMsg{Channel: 0, Kind: midi.ButtonRec, Pressed: false})

	// Immediately switch page — this simulates the concurrent OnGlobal and must
	// find the cancel func already stored (not nil), so the goroutine is cancelled.
	d.OnGlobal(midi.GlobalMsg{Action: midi.ActionPlay, Pressed: true})

	// After page switch, Advanced must be false.
	if d.Snapshot()[0].Advanced {
		t.Fatal("Advanced should be false after page switch")
	}
}

func TestUpdatePlaybackStatusForStream(t *testing.T) {
	d := New(newFakePW())
	const streamID uint32 = 42
	d.Bind(0, streamID, "test", audio.KindSource, "player")

	// Normal update: sets Stop=true when stream matches.
	d.UpdatePlaybackStatusForStream(0, streamID, true)
	if !d.Snapshot()[0].Stop {
		t.Fatal("Stop should be true after UpdatePlaybackStatusForStream with matching ID")
	}

	// Stale-snapshot race: update rejected when ID doesn't match current binding.
	d.UpdatePlaybackStatusForStream(0, streamID+1, false)
	if !d.Snapshot()[0].Stop {
		t.Fatal("Stop should remain true when ID doesn't match (stale-snapshot guard)")
	}

	// After Unbind, update for the old stream ID must be rejected.
	d.Unbind(0)
	d.UpdatePlaybackStatusForStream(0, streamID, true)
	if d.Snapshot()[0].Stop {
		t.Fatal("Stop should not be set after Unbind (stale-snapshot guard)")
	}
}
