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

	applyBindings(cfg, disp, []streams.EnrichedStream{
		stream(20, "Spotify", "spotify", audio.KindSource),
	}, nil)
	if got := disp.Snapshot()[0].MPRISName; got != "" {
		t.Fatalf("initial MPRISName = %q, want empty", got)
	}

	applyBindings(cfg, disp, []streams.EnrichedStream{
		mprisStream(20, "spotify", audio.KindSource),
	}, nil)

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

	applyKnobBindings(cfg, disp, "main", []streams.EnrichedStream{
		stream(20, "Spotify", "spotify", audio.KindSource),
	})

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

	applyKnobBindings(cfg, disp, disp.ActivePage(), []streams.EnrichedStream{
		stream(20, "Spotify", "spotify", audio.KindSource),
	})

	if got := disp.Snapshot()[0].KnobStreamID; got != nil {
		t.Fatalf("KnobStreamID = %v outside main page, want nil", *got)
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
