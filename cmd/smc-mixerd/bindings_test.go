package main

import (
	"context"
	"testing"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/midi"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

func sp(s string) *string { return &s }

func testConfig(bindType config.BindType, match string) *config.Config {
	return &config.Config{
		Devices: map[string]config.DeviceConfig{
			"dev0": {Type: bindType, Match: match},
		},
		Pages: map[string]config.PageConfig{
			"main": {Faders: map[int]*string{0: sp("dev0")}},
		},
	}
}

func TestPlanBindingsMatchesConfiguredPlayback(t *testing.T) {
	cfg := testConfig(config.BindPlayback, "spotify")
	ss := []streams.EnrichedStream{
		stream(10, "Firefox", "firefox", audio.KindSource),
		stream(20, "Spotify", "spotify", audio.KindSource),
	}

	actions := planBindings(cfg, "main", [8]dispatcher.Channel{}, ss)

	if len(actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(actions))
	}
	if actions[0].ch != 0 || actions[0].id != 20 || actions[0].name != "Spotify" {
		t.Fatalf("unexpected action: %+v", actions[0])
	}
}

func TestPlanBindingsResolvesOutputMatch(t *testing.T) {
	cfg := &config.Config{
		Devices: map[string]config.DeviceConfig{
			"speakers": {Type: config.BindOutput, Match: "Ryzen HD Audio"},
		},
		Pages: map[string]config.PageConfig{
			"main": {Faders: map[int]*string{0: sp("speakers")}},
		},
	}
	ss := []streams.EnrichedStream{
		stream(30, "Ryzen HD Audio Analog Stereo", "alsa_output", audio.KindSink),
	}

	actions := planBindings(cfg, "main", [8]dispatcher.Channel{}, ss)

	if len(actions) != 1 || actions[0].id != 30 {
		t.Fatalf("expected output device to bind sink 30, got %+v", actions)
	}
}

func TestPlanBindingsLeavesLiveBindingsAlone(t *testing.T) {
	cfg := testConfig(config.BindPlayback, "spotify")
	snap := [8]dispatcher.Channel{}
	id := uint32(20)
	snap[0].StreamID = &id
	ss := []streams.EnrichedStream{
		stream(20, "Spotify", "spotify", audio.KindSource),
	}

	actions := planBindings(cfg, "main", snap, ss)

	// A syncSpec action is allowed (it refreshes config-derived metadata without rebinding).
	// No lose or actual bind actions should be emitted for a correctly-live binding.
	for _, a := range actions {
		if !a.syncSpec {
			t.Fatalf("live matching binding should only emit syncSpec actions, got %+v", actions)
		}
	}
}

func TestApplyBindingsRefreshesMPRISForLiveBinding(t *testing.T) {
	cfg := testConfig(config.BindPlayback, "spotify")
	disp := dispatcher.New(newNoopPW())

	applyBindings(context.Background(), cfg, disp, []streams.EnrichedStream{
		stream(20, "Spotify", "spotify", audio.KindSource),
	}, nil, nil)
	if got := disp.Snapshot()[0].MPRISName; got != "" {
		t.Fatalf("initial MPRISName = %q, want empty", got)
	}

	applyBindings(context.Background(), cfg, disp, []streams.EnrichedStream{
		mprisStream(20, "spotify", audio.KindSource),
	}, nil, nil)

	if got := disp.Snapshot()[0].MPRISName; got != "spotify" {
		t.Fatalf("refreshed MPRISName = %q, want spotify", got)
	}
}

func TestPlanBindingsRebindsDeadBinding(t *testing.T) {
	cfg := testConfig(config.BindPlayback, "spotify")
	snap := [8]dispatcher.Channel{}
	oldID := uint32(20)
	snap[0].StreamID = &oldID
	ss := []streams.EnrichedStream{
		stream(21, "Spotify", "spotify", audio.KindSource),
	}

	actions := planBindings(cfg, "main", snap, ss)

	if len(actions) != 1 || actions[0].id != 21 {
		t.Fatalf("dead binding should be rebound to stream 21, got %+v", actions)
	}
}

func TestPlanBindingsCapturesMPRISName(t *testing.T) {
	cfg := testConfig(config.BindPlayback, "spotify")
	ss := []streams.EnrichedStream{
		mprisStream(20, "spotify", audio.KindSource),
	}

	actions := planBindings(cfg, "main", [8]dispatcher.Channel{}, ss)

	if len(actions) != 1 || actions[0].mprisName != "spotify" {
		t.Fatalf("expected MPRIS name spotify, got %+v", actions)
	}
}

func TestStreamMatcherFiltersWrongKind(t *testing.T) {
	cfg := testConfig(config.BindPlayback, "spotify")
	ss := []streams.EnrichedStream{
		stream(20, "Spotify Microphone", "spotify", audio.KindMic),
	}

	actions := planBindings(cfg, "main", [8]dispatcher.Channel{}, ss)

	if len(actions) != 0 {
		t.Fatalf("wrong stream kind should not match, got %+v", actions)
	}
}

func TestStreamMatcherRegexChecksNameAndBindKey(t *testing.T) {
	cfg := &config.Config{
		Devices: map[string]config.DeviceConfig{
			"dev0": {Type: config.BindPlayback, MatchRegex: "firefox.*"},
		},
		Pages: map[string]config.PageConfig{
			"main": {Faders: map[int]*string{0: sp("dev0")}},
		},
	}
	ss := []streams.EnrichedStream{
		stream(20, "Browser", "firefox.instance_1", audio.KindSource),
	}

	actions := planBindings(cfg, "main", [8]dispatcher.Channel{}, ss)

	if len(actions) != 1 || actions[0].id != 20 {
		t.Fatalf("regex should match BindKey, got %+v", actions)
	}
}

func TestStreamMatcherRegexTakesPrecedenceOverMatch(t *testing.T) {
	cfg := &config.Config{
		Devices: map[string]config.DeviceConfig{
			"dev0": {Type: config.BindPlayback, Match: "spotify", MatchRegex: "firefox"},
		},
		Pages: map[string]config.PageConfig{
			"main": {Faders: map[int]*string{0: sp("dev0")}},
		},
	}
	ss := []streams.EnrichedStream{
		stream(20, "Spotify", "spotify", audio.KindSource),
	}

	actions := planBindings(cfg, "main", [8]dispatcher.Channel{}, ss)

	if len(actions) != 0 {
		t.Fatalf("regex should take precedence over substring match, got %+v", actions)
	}
}

func TestStreamMatcherInvalidRegexMatchesNothing(t *testing.T) {
	cfg := &config.Config{
		Devices: map[string]config.DeviceConfig{
			"dev0": {Type: config.BindPlayback, Match: "spotify", MatchRegex: "["},
		},
		Pages: map[string]config.PageConfig{
			"main": {Faders: map[int]*string{0: sp("dev0")}},
		},
	}
	ss := []streams.EnrichedStream{
		stream(20, "Spotify", "spotify", audio.KindSource),
	}

	actions := planBindings(cfg, "main", [8]dispatcher.Channel{}, ss)

	if len(actions) != 0 {
		t.Fatalf("invalid regex should match nothing, got %+v", actions)
	}
}

func TestPlanBindingsSkipsManuallyUnboundChannels(t *testing.T) {
	cfg := testConfig(config.BindPlayback, "spotify")
	snap := [8]dispatcher.Channel{}
	snap[0].ManuallyUnbound = true
	ss := []streams.EnrichedStream{
		stream(20, "Spotify", "spotify", audio.KindSource),
	}

	actions := planBindings(cfg, "main", snap, ss)

	if len(actions) != 0 {
		t.Fatalf("manually unbound channel should not be auto-rebound, got %+v", actions)
	}
}

func TestPlanBindingsPreservesPinnedMainPageLiveBinding(t *testing.T) {
	cfg := testConfig(config.BindPlayback, "firefox")
	id := uint32(20)
	snap := [8]dispatcher.Channel{}
	snap[0].StreamID = &id
	snap[0].Name = "Spotify"
	snap[0].Kind = audio.KindSource
	snap[0].Pinned = true
	ss := []streams.EnrichedStream{
		stream(20, "Spotify", "spotify", audio.KindSource),
		stream(30, "Firefox", "firefox", audio.KindSource),
	}

	actions := planBindings(cfg, "main", snap, ss)

	if len(actions) != 0 {
		t.Fatalf("pinned live main-page binding should be preserved, got %+v", actions)
	}
}

// Regression: switching from outputs page (ch1=headphones, KindSink) to main page
// (ch1=youtube-music, KindSource) must clear the sink binding even when
// youtube-music is not currently playing. The fix lives in clearPageAssignments,
// not in planBindings, so the test exercises both together as the page-switch
// handler does.
func TestPageSwitchClearsStaleCrossPageBinding(t *testing.T) {
	cfg := &config.Config{
		Devices: map[string]config.DeviceConfig{
			"youtube-music": {Type: config.BindPlayback, Match: "youtube"},
		},
		Pages: map[string]config.PageConfig{
			"main": {Faders: map[int]*string{1: sp("youtube-music")}},
		},
	}
	disp := dispatcher.New(newNoopPW())
	disp.Bind(1, 99, "WH-1000XM4 Analog Stereo", audio.KindSink, "")

	ss := []streams.EnrichedStream{
		stream(99, "WH-1000XM4 Analog Stereo", "alsa_output.usb", audio.KindSink),
	}

	clearPageAssignments(disp)
	applyBindings(context.Background(), cfg, disp, ss, map[int]string{}, nil)

	if got := disp.Snapshot()[1].StreamID; got != nil {
		t.Fatalf("ch1 should be unbound after page switch, got stream %d", *got)
	}
}

func TestClearPageAssignmentsClearsLiveBindings(t *testing.T) {
	disp := dispatcher.New(newNoopPW())
	disp.Bind(1, 99, "WH-1000XM4", audio.KindSink, "")

	clearPageAssignments(disp)

	if got := disp.Snapshot()[1].StreamID; got != nil {
		t.Fatalf("clearPageAssignments should clear ch1, got %d", *got)
	}
}

func TestClearPageAssignmentsPreservesPinned(t *testing.T) {
	disp := dispatcher.New(newNoopPW())
	disp.Bind(0, 10, "Spotify", audio.KindSource, "")
	disp.SetPinned(0, true)

	clearPageAssignments(disp)

	snap := disp.Snapshot()
	if snap[0].StreamID == nil || *snap[0].StreamID != 10 {
		t.Fatal("clearPageAssignments should preserve pinned bindings")
	}
}

func TestClearPageAssignmentsPreservesManuallyUnbound(t *testing.T) {
	disp := dispatcher.New(newNoopPW())
	disp.Unbind(0)

	clearPageAssignments(disp)

	if !disp.Snapshot()[0].ManuallyUnbound {
		t.Fatal("clearPageAssignments should preserve ManuallyUnbound flag")
	}
}

func TestPlanBindingsLosesLiveBindingWhenPageSlotIsEmpty(t *testing.T) {
	cfg := &config.Config{
		Devices: map[string]config.DeviceConfig{},
		Pages:   map[string]config.PageConfig{"main": {Faders: map[int]*string{}}},
	}
	id := uint32(20)
	snap := [8]dispatcher.Channel{}
	snap[0].StreamID = &id
	ss := []streams.EnrichedStream{
		stream(20, "Spotify", "spotify", audio.KindSource),
	}

	actions := planBindings(cfg, "main", snap, ss)

	if len(actions) != 1 || !actions[0].lose {
		t.Fatalf("empty configured slot should lose live binding, got %+v", actions)
	}
}

func TestStreamMatcherMatchTitle(t *testing.T) {
	cfg := &config.Config{
		Devices: map[string]config.DeviceConfig{
			"dev0": {Type: config.BindPlayback, MatchTitle: "YouTube Music"},
		},
		Pages: map[string]config.PageConfig{
			"main": {Faders: map[int]*string{0: sp("dev0")}},
		},
	}
	ss := []streams.EnrichedStream{
		{ID: 10, Name: "chrome-music.youtube.com__-Default", BindKey: "chrome-music.youtube.com__-Default", WinTitle: "YouTube Music", Kind: audio.KindSource},
		{ID: 20, Name: "Chromium", BindKey: "chromium", WinTitle: "Chromium", Kind: audio.KindSource},
	}

	actions := planBindings(cfg, "main", [8]dispatcher.Channel{}, ss)

	if len(actions) != 1 || actions[0].id != 10 {
		t.Fatalf("match-title should bind stream 10, got %+v", actions)
	}
}

func TestApplyKnobBindingsBindsMainPageGainKnob(t *testing.T) {
	cfg := &config.Config{
		Devices: map[string]config.DeviceConfig{
			"knob0": {
				Type:  config.BindPlayback,
				Match: "spotify",
				Knob:  &config.KnobConfig{Type: config.KnobGain},
			},
		},
		Pages: map[string]config.PageConfig{
			"main": {Knobs: map[int]*string{0: sp("knob0")}},
		},
	}
	disp := dispatcher.New(newNoopPW())

	applyKnobBindings(context.Background(), cfg, disp, "main", []streams.EnrichedStream{
		stream(20, "Spotify", "spotify", audio.KindSource),
	}, nil)

	got := disp.Snapshot()[0].KnobStreamID
	if got == nil || *got != 20 {
		t.Fatalf("KnobStreamID = %v, want 20", got)
	}
}

func TestApplyKnobBindingsClearsOutsideMainPage(t *testing.T) {
	cfg := testConfig(config.BindPlayback, "spotify")
	disp := dispatcher.New(newNoopPW())
	disp.BindKnob(0, 20)
	disp.OnGlobal(midi.GlobalMsg{Action: midi.ActionPlay, Pressed: true})

	applyKnobBindings(context.Background(), cfg, disp, disp.ActivePage(), []streams.EnrichedStream{
		stream(20, "Spotify", "spotify", audio.KindSource),
	}, nil)

	if got := disp.Snapshot()[0].KnobStreamID; got != nil {
		t.Fatalf("KnobStreamID = %v outside main page, want nil", *got)
	}
}

func TestApplyKnobBindingsBindsOutputKnobWithNoneDefault(t *testing.T) {
	cfg := &config.Config{
		Devices: map[string]config.DeviceConfig{
			"headphones": {
				Type:  config.BindOutput,
				Match: "WH-1000XM4",
				// No Knob override; defaults to output-knob which is KnobNone.
			},
		},
		Defaults: config.DefaultsConfig{
			OutputKnob: config.KnobConfig{Type: config.KnobNone},
		},
		Pages: map[string]config.PageConfig{
			"main": {Knobs: map[int]*string{6: sp("headphones")}},
		},
	}
	disp := dispatcher.New(newNoopPW())

	applyKnobBindings(context.Background(), cfg, disp, "main", []streams.EnrichedStream{
		stream(42, "WH-1000XM4 Analog Stereo", "alsa_output.usb", audio.KindSink),
	}, nil)

	got := disp.Snapshot()[6].KnobStreamID
	if got == nil || *got != 42 {
		t.Fatalf("KnobStreamID = %v, want 42; output device in knob slot should bind for gain", got)
	}
}

func TestKnobBindingCandidateSkipsSendKnob(t *testing.T) {
	cfg := &config.Config{
		Devices: map[string]config.DeviceConfig{
			"knob0": {
				Type:  config.BindPlayback,
				Match: "spotify",
				Knob:  &config.KnobConfig{Type: config.KnobSend},
			},
		},
		Pages: map[string]config.PageConfig{
			"main": {Knobs: map[int]*string{0: sp("knob0")}},
		},
	}

	got := knobBindingCandidate(cfg, "main", 0, []streams.EnrichedStream{
		stream(20, "Spotify", "spotify", audio.KindSource),
	})

	if got != nil {
		t.Fatalf("send knob should not bind as gain knob, got %+v", *got)
	}
}

func TestPlanBindingsPIDRebindWhenStreamDies(t *testing.T) {
	// Simulate a user-bound stream that has died (StreamID cleared) but BoundPID is set.
	// A new stream from the same process should be reattached.
	cfg := &config.Config{} // no config for ch0 — pure user-bound scenario
	snap := [8]dispatcher.Channel{}
	snap[0].BoundPID = 1234 // previous stream's PID; StreamID is nil (stream died)

	ss := []streams.EnrichedStream{
		{ID: 99, Name: "Firefox", BindKey: "firefox", Kind: audio.KindSource, PID: 1234},
	}

	actions := planBindings(cfg, "main", snap, ss)

	if len(actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(actions))
	}
	if !actions[0].userBound {
		t.Fatalf("PID-based rebind should have userBound=true, got %+v", actions[0])
	}
	if actions[0].id != 99 {
		t.Fatalf("PID-based rebind should bind stream 99, got %+v", actions[0])
	}
}

func TestPlanBindingsPIDRebindNotTriggeredWhenLive(t *testing.T) {
	// If the original stream is still alive, BoundPID must not cause a spurious rebind.
	cfg := &config.Config{}
	snap := [8]dispatcher.Channel{}
	id := uint32(50)
	snap[0].StreamID = &id
	snap[0].BoundPID = 1234
	snap[0].UserBound = true

	ss := []streams.EnrichedStream{
		{ID: 50, Name: "Firefox", BindKey: "firefox", Kind: audio.KindSource, PID: 1234},
	}

	actions := planBindings(cfg, "main", snap, ss)

	for _, a := range actions {
		if a.id != 0 && !a.syncSpec && !a.lose {
			t.Fatalf("live user-bound channel should not be rebound, got %+v", a)
		}
	}
}

func stream(id uint32, name, bindKey string, kind audio.NodeKind) streams.EnrichedStream {
	return streams.EnrichedStream{
		ID:      id,
		Name:    name,
		BindKey: bindKey,
		Kind:    kind,
	}
}

func mprisStream(id uint32, name string, kind audio.NodeKind) streams.EnrichedStream {
	s := stream(id, name, name, kind)
	s.Source = streams.SourceMPRIS
	s.MPRISPlayer = name
	return s
}

type noopPW struct{}

func newNoopPW() noopPW { return noopPW{} }

func (noopPW) SetVolume(context.Context, uint32, float64) error { return nil }

func (noopPW) SetMute(context.Context, uint32, bool) error { return nil }
