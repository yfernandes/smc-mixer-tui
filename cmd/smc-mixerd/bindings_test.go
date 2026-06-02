package main

import (
	"testing"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

func TestPlanBindingsMatchesConfiguredPlayback(t *testing.T) {
	cfg := testConfig(config.BindPlayback, "spotify")
	ss := []streams.EnrichedStream{
		stream(10, "Firefox", "firefox", audio.KindSource),
		stream(20, "Spotify", "spotify", audio.KindSource),
	}

	actions := planBindings(cfg, [8]dispatcher.Channel{}, ss)

	if len(actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(actions))
	}
	if actions[0].ch != 0 || actions[0].id != 20 || actions[0].name != "Spotify" {
		t.Fatalf("unexpected action: %+v", actions[0])
	}
}

func TestPlanBindingsResolvesOutputAliases(t *testing.T) {
	cfg := &config.Config{
		Outputs: map[string]string{"speakers": "Ryzen HD Audio"},
		Channels: map[string]config.ChannelConfig{
			"0": {Bind: config.BindConfig{Type: config.BindOutput, Match: "speakers"}},
		},
	}
	ss := []streams.EnrichedStream{
		stream(30, "Ryzen HD Audio Analog Stereo", "alsa_output", audio.KindSink),
	}

	actions := planBindings(cfg, [8]dispatcher.Channel{}, ss)

	if len(actions) != 1 || actions[0].id != 30 {
		t.Fatalf("expected output alias to bind sink 30, got %+v", actions)
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

	actions := planBindings(cfg, snap, ss)

	if len(actions) != 0 {
		t.Fatalf("live binding should not be rebound, got %+v", actions)
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

	actions := planBindings(cfg, snap, ss)

	if len(actions) != 1 || actions[0].id != 21 {
		t.Fatalf("dead binding should be rebound to stream 21, got %+v", actions)
	}
}

func TestPlanBindingsCapturesMPRISName(t *testing.T) {
	cfg := testConfig(config.BindPlayback, "spotify")
	ss := []streams.EnrichedStream{
		mprisStream(20, "spotify", audio.KindSource),
	}

	actions := planBindings(cfg, [8]dispatcher.Channel{}, ss)

	if len(actions) != 1 || actions[0].mprisName != "spotify" {
		t.Fatalf("expected MPRIS name spotify, got %+v", actions)
	}
}

func TestStreamMatcherFiltersWrongKind(t *testing.T) {
	cfg := testConfig(config.BindPlayback, "spotify")
	ss := []streams.EnrichedStream{
		stream(20, "Spotify Microphone", "spotify", audio.KindMic),
	}

	actions := planBindings(cfg, [8]dispatcher.Channel{}, ss)

	if len(actions) != 0 {
		t.Fatalf("wrong stream kind should not match, got %+v", actions)
	}
}

func TestStreamMatcherRegexChecksNameAndBindKey(t *testing.T) {
	cfg := &config.Config{
		Channels: map[string]config.ChannelConfig{
			"0": {Bind: config.BindConfig{Type: config.BindPlayback, MatchRegex: "firefox.*"}},
		},
	}
	ss := []streams.EnrichedStream{
		stream(20, "Browser", "firefox.instance_1", audio.KindSource),
	}

	actions := planBindings(cfg, [8]dispatcher.Channel{}, ss)

	if len(actions) != 1 || actions[0].id != 20 {
		t.Fatalf("regex should match BindKey, got %+v", actions)
	}
}

func TestStreamMatcherRegexTakesPrecedenceOverMatch(t *testing.T) {
	cfg := &config.Config{
		Channels: map[string]config.ChannelConfig{
			"0": {Bind: config.BindConfig{Type: config.BindPlayback, Match: "spotify", MatchRegex: "firefox"}},
		},
	}
	ss := []streams.EnrichedStream{
		stream(20, "Spotify", "spotify", audio.KindSource),
	}

	actions := planBindings(cfg, [8]dispatcher.Channel{}, ss)

	if len(actions) != 0 {
		t.Fatalf("regex should take precedence over substring match, got %+v", actions)
	}
}

func TestStreamMatcherInvalidRegexMatchesNothing(t *testing.T) {
	cfg := &config.Config{
		Channels: map[string]config.ChannelConfig{
			"0": {Bind: config.BindConfig{Type: config.BindPlayback, Match: "spotify", MatchRegex: "["}},
		},
	}
	ss := []streams.EnrichedStream{
		stream(20, "Spotify", "spotify", audio.KindSource),
	}

	actions := planBindings(cfg, [8]dispatcher.Channel{}, ss)

	if len(actions) != 0 {
		t.Fatalf("invalid regex should match nothing, got %+v", actions)
	}
}

func testConfig(bindType config.BindType, match string) *config.Config {
	return &config.Config{
		Channels: map[string]config.ChannelConfig{
			"0": {Bind: config.BindConfig{Type: bindType, Match: match}},
		},
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
	return s
}
