package streams

import (
	"context"
	"errors"
	"os"
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

func TestEnrichMPRISDirectOverridesHyprland(t *testing.T) {
	e := &Enricher{
		pw:    fakePW([]pipewire.Stream{{ID: 10, Name: "mpv", PID: 100}}),
		hypr:  fakeHypr([]hyprWindow{{PID: 100, Class: "mpv-class"}}),
		mpris: fakeMPRIS([]mprisPlayer{{Name: "mpv-player", PID: 100, Track: "Song", Artist: "Band"}}),
	}
	ss, _ := e.Enrich(context.Background())
	s := ss[0]
	// Direct match: MPRIS name wins for display identity
	if s.Name != "mpv-player" || s.Source != SourceMPRIS || s.Track != "Song" || s.Artist != "Band" {
		t.Errorf("direct MPRIS match should override Hyprland identity: %+v", s)
	}
	if s.MPRISPlayer != "mpv-player" {
		t.Errorf("MPRISPlayer = %q, want mpv-player", s.MPRISPlayer)
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

func TestEnrichHyprlandPipedTitleSplitsNameAndSubtitle(t *testing.T) {
	e := &Enricher{
		pw:    fakePW([]pipewire.Stream{{ID: 10, Name: "Chromium", PID: 100}}),
		hypr:  fakeHypr([]hyprWindow{{PID: 100, Class: "chrome-music.youtube.com__-Default", Title: "Welcome to the Family | YouTube Music"}}),
		mpris: noMPRIS,
	}
	ss, _ := e.Enrich(context.Background())
	s := ss[0]
	if s.Name != "YouTube Music" {
		t.Errorf("Name = %q, want %q", s.Name, "YouTube Music")
	}
	if s.MediaName != "Welcome to the Family" {
		t.Errorf("MediaName = %q, want %q", s.MediaName, "Welcome to the Family")
	}
	if s.BindKey != "chrome-music.youtube.com__-Default" {
		t.Errorf("BindKey = %q, want class", s.BindKey)
	}
}

func TestEnrichHyprlandTitleNoSeparatorUsesTitle(t *testing.T) {
	e := &Enricher{
		pw:    fakePW([]pipewire.Stream{{ID: 10, Name: "mpv", PID: 100}}),
		hypr:  fakeHypr([]hyprWindow{{PID: 100, Class: "mpv", Title: "movie.mkv"}}),
		mpris: noMPRIS,
	}
	ss, _ := e.Enrich(context.Background())
	if ss[0].Name != "movie.mkv" || ss[0].MediaName != "" {
		t.Errorf("unexpected: %+v", ss[0])
	}
}

// TestEnrichMPRISAncestorMatch verifies that a PipeWire stream owned by a
// child process is matched to the MPRIS player registered by its parent.
// This is the Chromium case: the main browser process (MPRIS owner) spawns
// a utility audio subprocess (PW stream), so the PIDs differ by one level.
// Crucially, an ancestry match must NOT override the display identity —
// only MPRISPlayer and track metadata are set, so config rules that matched
// the Hyprland-derived name (e.g. "YouTube Music") continue to work.
func TestEnrichMPRISAncestorMatch(t *testing.T) {
	ppid := uint32(os.Getppid())
	e := &Enricher{
		pw:   fakePW([]pipewire.Stream{{ID: 10, Name: "Chromium", PID: uint32(os.Getpid())}}),
		hypr: noHypr,
		mpris: fakeMPRIS([]mprisPlayer{
			{Name: "chromium.instance1296365", PID: ppid, Track: "Hey There Delilah", Artist: "Plain White T's"},
		}),
	}
	ss, err := e.Enrich(context.Background())
	if err != nil || len(ss) != 1 {
		t.Fatalf("err=%v len=%d", err, len(ss))
	}
	s := ss[0]
	// Ancestry match: display identity is preserved (Name stays as PipeWire gave it)
	if s.Source != SourcePipeWire {
		t.Errorf("Source = %v, want SourcePipeWire (ancestry match must not override)", s.Source)
	}
	if s.Name != "Chromium" {
		t.Errorf("Name = %q, want original PW name (ancestry match must not override)", s.Name)
	}
	// But MPRIS control name and track metadata must be populated
	if s.MPRISPlayer != "chromium.instance1296365" {
		t.Errorf("MPRISPlayer = %q, want chromium instance name", s.MPRISPlayer)
	}
	if s.Track != "Hey There Delilah" {
		t.Errorf("Track = %q, want track title", s.Track)
	}
	if s.Artist != "Plain White T's" {
		t.Errorf("Artist = %q, want artist", s.Artist)
	}
}

func TestEnrichAppNamePreservedAfterMPRIS(t *testing.T) {
	e := &Enricher{
		pw:    fakePW([]pipewire.Stream{{ID: 10, Name: "spotify", PID: 100}}),
		hypr:  noHypr,
		mpris: fakeMPRIS([]mprisPlayer{{Name: "spotify", PID: 100, Track: "Invincible", Artist: "TOOL"}}),
	}
	ss, _ := e.Enrich(context.Background())
	s := ss[0]
	if s.AppName != "spotify" {
		t.Errorf("AppName = %q, want original PW name %q", s.AppName, "spotify")
	}
	// Name is overridden by MPRIS (same value here, but Artist/Track confirm enrichment worked)
	if s.Artist != "TOOL" || s.Track != "Invincible" {
		t.Errorf("MPRIS metadata not applied: %+v", s)
	}
}

func TestEnrichAppNamePreservedAfterHyprland(t *testing.T) {
	e := &Enricher{
		pw:    fakePW([]pipewire.Stream{{ID: 10, Name: "Zen", PID: 100}}),
		hypr:  fakeHypr([]hyprWindow{{PID: 100, Class: "zen", Title: "Claude | Anthropic"}}),
		mpris: noMPRIS,
	}
	ss, _ := e.Enrich(context.Background())
	s := ss[0]
	if s.AppName != "Zen" {
		t.Errorf("AppName = %q, want original PW name %q", s.AppName, "Zen")
	}
	if s.Name == "Zen" {
		t.Errorf("Name should have been overridden by Hyprland enrichment, got %q", s.Name)
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
