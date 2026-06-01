package streams

import (
	"context"
	"errors"
	"testing"

	"github.com/yfernandes/smc-mixer-tui/pipewire"
)

// — fakes —

func fakePW(ss []pipewire.Stream) func(context.Context) ([]pipewire.Stream, error) {
	return func(context.Context) ([]pipewire.Stream, error) { return ss, nil }
}

func fakeHypr(ws []hyprWindow) func(context.Context) ([]hyprWindow, error) {
	return func(context.Context) ([]hyprWindow, error) { return ws, nil }
}

func fakeMPRIS(ps []mprisPlayer) func(context.Context) ([]mprisPlayer, error) {
	return func(context.Context) ([]mprisPlayer, error) { return ps, nil }
}

func errPW(msg string) func(context.Context) ([]pipewire.Stream, error) {
	return func(context.Context) ([]pipewire.Stream, error) { return nil, errors.New(msg) }
}

func noHypr(_ context.Context) ([]hyprWindow, error)  { return nil, nil }
func noMPRIS(_ context.Context) ([]mprisPlayer, error) { return nil, nil }

// — join logic —

func TestEnrichPipeWireOnly(t *testing.T) {
	e := &Enricher{
		pw:    fakePW([]pipewire.Stream{{ID: 10, Name: "mpv", PID: 100}}),
		hypr:  noHypr,
		mpris: noMPRIS,
	}
	ss, err := e.Enrich(context.Background())
	if err != nil || len(ss) != 1 {
		t.Fatalf("err=%v, len=%d", err, len(ss))
	}
	s := ss[0]
	if s.ID != 10 || s.Name != "mpv" || s.BindKey != "mpv" || s.Source != SourcePipeWire {
		t.Errorf("unexpected: %+v", s)
	}
}

func TestEnrichHyprlandOverridesPipeWire(t *testing.T) {
	e := &Enricher{
		pw:    fakePW([]pipewire.Stream{{ID: 10, Name: "mpv", PID: 100}}),
		hypr:  fakeHypr([]hyprWindow{{PID: 100, Class: "mpv-class"}}),
		mpris: noMPRIS,
	}
	ss, _ := e.Enrich(context.Background())
	if ss[0].Name != "mpv-class" || ss[0].Source != SourceHyprland {
		t.Errorf("Hyprland should override PipeWire: %+v", ss[0])
	}
}

func TestEnrichMPRISOverridesHyprland(t *testing.T) {
	e := &Enricher{
		pw:    fakePW([]pipewire.Stream{{ID: 10, Name: "mpv", PID: 100}}),
		hypr:  fakeHypr([]hyprWindow{{PID: 100, Class: "mpv-class"}}),
		mpris: fakeMPRIS([]mprisPlayer{{Name: "mpv-player", PID: 100, Track: "Song", Artist: "Band"}}),
	}
	ss, _ := e.Enrich(context.Background())
	s := ss[0]
	if s.Name != "mpv-player" || s.Source != SourceMPRIS || s.Track != "Song" || s.Artist != "Band" {
		t.Errorf("MPRIS should be top priority: %+v", s)
	}
}

func TestEnrichPIDMismatchNoUpgrade(t *testing.T) {
	e := &Enricher{
		pw:    fakePW([]pipewire.Stream{{ID: 10, Name: "vlc", PID: 200}}),
		hypr:  fakeHypr([]hyprWindow{{PID: 999, Class: "other-app"}}), // different PID
		mpris: noMPRIS,
	}
	ss, _ := e.Enrich(context.Background())
	if ss[0].Source != SourcePipeWire || ss[0].Name != "vlc" {
		t.Errorf("PID mismatch should keep PipeWire source: %+v", ss[0])
	}
}

func TestEnrichZeroPIDNeverMatches(t *testing.T) {
	e := &Enricher{
		pw:    fakePW([]pipewire.Stream{{ID: 10, Name: "vlc", PID: 0}}),
		hypr:  fakeHypr([]hyprWindow{{PID: 0, Class: "vlc"}}),
		mpris: noMPRIS,
	}
	ss, _ := e.Enrich(context.Background())
	if ss[0].Source != SourcePipeWire {
		t.Errorf("PID=0 should never match: %+v", ss[0])
	}
}

func TestEnrichMultipleStreams(t *testing.T) {
	e := &Enricher{
		pw: fakePW([]pipewire.Stream{
			{ID: 1, Name: "Firefox", PID: 10},
			{ID: 2, Name: "Spotify", PID: 20},
			{ID: 3, Name: "vlc", PID: 30},
		}),
		hypr: fakeHypr([]hyprWindow{
			{PID: 10, Class: "firefox"},
			{PID: 30, Class: "vlc"},
		}),
		mpris: fakeMPRIS([]mprisPlayer{
			{Name: "spotify", PID: 20, Track: "Midnight", Artist: "The Cure"},
		}),
	}
	ss, err := e.Enrich(context.Background())
	if err != nil || len(ss) != 3 {
		t.Fatalf("err=%v len=%d", err, len(ss))
	}

	byID := make(map[uint32]EnrichedStream)
	for _, s := range ss {
		byID[s.ID] = s
	}

	if byID[1].Source != SourceHyprland || byID[1].Name != "firefox" {
		t.Errorf("ID 1: %+v", byID[1])
	}
	if byID[2].Source != SourceMPRIS || byID[2].Name != "spotify" || byID[2].Track != "Midnight" {
		t.Errorf("ID 2: %+v", byID[2])
	}
	if byID[3].Source != SourceHyprland || byID[3].Name != "vlc" {
		t.Errorf("ID 3: %+v", byID[3])
	}
}

func TestEnrichHyprlandErrorIsNonFatal(t *testing.T) {
	e := &Enricher{
		pw:    fakePW([]pipewire.Stream{{ID: 1, Name: "app", PID: 1}}),
		hypr:  func(context.Context) ([]hyprWindow, error) { return nil, errors.New("no hyprland") },
		mpris: noMPRIS,
	}
	ss, err := e.Enrich(context.Background())
	if err != nil || len(ss) != 1 || ss[0].Source != SourcePipeWire {
		t.Errorf("Hyprland error should be non-fatal: err=%v ss=%+v", err, ss)
	}
}

func TestEnrichMPRISErrorIsNonFatal(t *testing.T) {
	e := &Enricher{
		pw:    fakePW([]pipewire.Stream{{ID: 1, Name: "app", PID: 1}}),
		hypr:  noHypr,
		mpris: func(context.Context) ([]mprisPlayer, error) { return nil, errors.New("dbus gone") },
	}
	ss, err := e.Enrich(context.Background())
	if err != nil || len(ss) != 1 || ss[0].Source != SourcePipeWire {
		t.Errorf("MPRIS error should be non-fatal: err=%v ss=%+v", err, ss)
	}
}

func TestEnrichPipeWireErrorPropagates(t *testing.T) {
	e := &Enricher{
		pw:    errPW("pw-dump failed"),
		hypr:  noHypr,
		mpris: noMPRIS,
	}
	_, err := e.Enrich(context.Background())
	if err == nil {
		t.Fatal("PipeWire error must propagate")
	}
}

func TestEnrichBindKeyEqualName(t *testing.T) {
	e := &Enricher{
		pw:    fakePW([]pipewire.Stream{{ID: 5, Name: "discord", PID: 50}}),
		hypr:  noHypr,
		mpris: noMPRIS,
	}
	ss, _ := e.Enrich(context.Background())
	if ss[0].BindKey != ss[0].Name {
		t.Errorf("BindKey should equal Name when no upgrade: %+v", ss[0])
	}
}
