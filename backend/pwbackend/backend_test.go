package pwbackend

import (
	"context"
	"testing"
	"time"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/backend"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/pipewire"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

type fakePW struct {
	volume float64
	muted  bool
	setID  uint32
	list   []pipewire.Stream
}

func TestWatchSuppressesUnchangedTargetSnapshots(t *testing.T) {
	stream := streams.EnrichedStream{ID: 9, Name: "Music", BindKey: "music", Kind: audio.KindSource}
	b := newBackend(&fakePW{}, fakeEnricher{streams: []streams.EnrichedStream{stream}}, &fakeMPRIS{}, map[string]config.DeviceConfig{
		"music": {Type: config.BindPlayback, Match: "music"},
	})
	if _, err := b.Targets(context.Background()); err != nil {
		t.Fatal(err)
	}
	b.interval = 5 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	ch := make(chan []backend.TargetInfo, 4)
	b.Watch(ctx, ch)
	if got := len(ch); got != 0 {
		t.Fatalf("unchanged watcher publications = %d, want 0", got)
	}
}

func (f *fakePW) ListStreams(context.Context) ([]pipewire.Stream, error) { return f.list, nil }

func (f *fakePW) SetVolume(_ context.Context, id uint32, value float64) error {
	f.setID, f.volume = id, value
	return nil
}
func (f *fakePW) SetMute(_ context.Context, id uint32, value bool) error {
	f.setID, f.muted = id, value
	return nil
}
func (f *fakePW) GetVolume(context.Context, uint32) (float64, bool, error) {
	return f.volume, f.muted, nil
}

type fakeEnricher struct{ streams []streams.EnrichedStream }

func (f fakeEnricher) Enrich(context.Context) ([]streams.EnrichedStream, error) {
	return f.streams, nil
}

type fakeMPRIS struct{ player string }

func (f *fakeMPRIS) PlayPause(_ context.Context, player string) error { f.player = player; return nil }

func TestRuleResolutionPrefersActiveAndReconnectsSameTab(t *testing.T) {
	pw := &fakePW{volume: .63}
	e := fakeEnricher{streams: []streams.EnrichedStream{
		{ID: 10, PID: 42, Name: "Firefox", BindKey: "firefox", MediaName: "other", Kind: audio.KindSource},
		{ID: 11, PID: 42, Name: "Firefox", BindKey: "firefox", MediaName: "wanted", Kind: audio.KindSource, Active: true},
	}}
	b := newBackend(pw, e, &fakeMPRIS{}, map[string]config.DeviceConfig{
		"browser": {Label: "Browser", Type: config.BindPlayback, Match: "firefox"},
	})
	if _, err := b.Targets(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.Set(context.Background(), "pipewire:rule/browser", "volume", backend.Value{F: .7}); err != nil {
		t.Fatal(err)
	}
	if pw.setID != 11 {
		t.Fatalf("initial active node = %d, want 11", pw.setID)
	}

	e.streams = []streams.EnrichedStream{
		{ID: 12, PID: 42, Name: "Firefox", BindKey: "firefox", MediaName: "other", Kind: audio.KindSource},
		{ID: 13, PID: 42, Name: "Firefox", BindKey: "firefox", MediaName: "wanted", Kind: audio.KindSource},
	}
	b.enricher = e
	if _, err := b.Targets(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.Set(context.Background(), "pipewire:rule/browser", "mute", backend.Value{B: true}); err != nil {
		t.Fatal(err)
	}
	if pw.setID != 13 {
		t.Fatalf("reconnected node = %d, want same-tab node 13", pw.setID)
	}
}

func TestRuleResolutionSwitchesOffIdleCurrentToNewActiveStream(t *testing.T) {
	pw := &fakePW{volume: .5}
	e := fakeEnricher{streams: []streams.EnrichedStream{
		{ID: 11, PID: 42, Name: "Zen", BindKey: "firefox", MediaName: "tab-a", Kind: audio.KindSource, Active: true},
	}}
	b := newBackend(pw, e, &fakeMPRIS{}, map[string]config.DeviceConfig{
		"browser": {Label: "Browser", Type: config.BindPlayback, Match: "zen"},
	})
	if _, err := b.Targets(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.Set(context.Background(), "pipewire:rule/browser", "volume", backend.Value{F: .5}); err != nil {
		t.Fatal(err)
	}
	if pw.setID != 11 {
		t.Fatalf("initial node = %d, want 11", pw.setID)
	}

	// Tab A goes idle (track ended) but its node lingers; a different tab (a new
	// PID/media identity) starts producing audio. Resolution must follow the
	// live audio instead of staying stuck on the now-silent node 11.
	e.streams = []streams.EnrichedStream{
		{ID: 11, PID: 42, Name: "Zen", BindKey: "firefox", MediaName: "tab-a", Kind: audio.KindSource, Active: false},
		{ID: 21, PID: 99, Name: "Zen", BindKey: "firefox", MediaName: "tab-b", Kind: audio.KindSource, Active: true},
	}
	b.enricher = e
	if _, err := b.Targets(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.Set(context.Background(), "pipewire:rule/browser", "mute", backend.Value{B: true}); err != nil {
		t.Fatal(err)
	}
	if pw.setID != 21 {
		t.Fatalf("resolved node = %d, want newly-active node 21 (not stuck on idle 11)", pw.setID)
	}
}

func TestReadableStateSeedsFromResolvedTarget(t *testing.T) {
	pw := &fakePW{volume: .72, muted: true}
	b := newBackend(pw, fakeEnricher{streams: []streams.EnrichedStream{{ID: 9, Name: "Music", BindKey: "music", Kind: audio.KindSource}}}, &fakeMPRIS{}, map[string]config.DeviceConfig{
		"music": {Type: config.BindPlayback, Match: "music"},
	})
	if _, err := b.Targets(context.Background()); err != nil {
		t.Fatal(err)
	}
	v, known, err := b.Get(context.Background(), "pipewire:rule/music", "volume")
	if err != nil || !known || v.F != .72 {
		t.Fatalf("Get volume = %#v, %v, %v", v, known, err)
	}
	m, known, err := b.Get(context.Background(), "pipewire:rule/music", "mute")
	if err != nil || !known || !m.B {
		t.Fatalf("Get mute = %#v, %v, %v", m, known, err)
	}
}

func TestCrossfadeGainsPreserveCenterPlateau(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		wantA float64
		wantB float64
	}{{"left", 0, 1, 0}, {"center", .5, 1, 1}, {"right", 1, 0, 1}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := crossfadeGains(tt.value)
			if a != tt.wantA || b != tt.wantB {
				t.Fatalf("crossfadeGains(%v) = (%v, %v), want (%v, %v)", tt.value, a, b, tt.wantA, tt.wantB)
			}
		})
	}
}

func TestRoutingSnapshotReadsBlacklistedGainNodesFromPipeWire(t *testing.T) {
	pw := &fakePW{volume: 1, list: []pipewire.Stream{
		{ID: 30, NodeName: "smc_music_gain_a"},
		{ID: 31, NodeName: "smc_music_gain_b"},
		{ID: 40, NodeName: "sink.a"},
		{ID: 41, NodeName: "sink.b"},
	}}
	b := newBackend(pw, fakeEnricher{}, &fakeMPRIS{}, nil)
	b.streams = []streams.EnrichedStream{{ID: 10, Name: "Music", NodeName: "music", Kind: audio.KindSource}}
	b.cross = &crossfaderManager{active: map[string]*crossfaderState{"music": {
		routing:  &pipewire.CrossfaderRouting{NullSinkName: "smc_music_void", GainAName: "smc_music_gain_a", GainBName: "smc_music_gain_b"},
		streamID: 10, streamName: "music", nameA: "A", nameB: "B", sinkANode: "sink.a", sinkBNode: "sink.b", value: .5,
	}}}
	snapshot := b.routingSnapshot(context.Background())
	if len(snapshot.Routes) != 1 {
		t.Fatalf("routes = %+v", snapshot.Routes)
	}
	for _, branch := range snapshot.Routes[0].Branches {
		if !branch.Steps[0].LiveKnown {
			t.Fatalf("gain %s is missing from live snapshot: %+v", branch.Label, branch.Steps[0])
		}
	}
}
