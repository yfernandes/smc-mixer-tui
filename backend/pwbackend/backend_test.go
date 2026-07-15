package pwbackend

import (
	"context"
	"testing"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/backend"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

type fakePW struct {
	volume float64
	muted  bool
	setID  uint32
}

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
