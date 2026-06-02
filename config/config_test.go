package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissing(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
}

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "smc-mixer", "config.yaml")

	orig := &Config{
		MIDI: MIDIConfig{Device: "/dev/midi2"},
		Outputs: map[string]string{
			"speakers":   "Built-in Audio",
			"headphones": "WH-1000XM5",
		},
		Channels: map[string]ChannelConfig{
			"0": {Label: "Firefox", Bind: BindConfig{Type: "playback", Match: "firefox"}},
			"3": {Label: "Spotify", Bind: BindConfig{Type: "playback", Match: "spotify"}},
			"7": {Label: "Speakers", Bind: BindConfig{Type: "output", Match: "speakers"}},
		},
	}

	if err := Save(path, orig); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.MIDI.Device != "/dev/midi2" {
		t.Errorf("device = %q, want /dev/midi2", loaded.MIDI.Device)
	}

	for ch, wantLabel := range map[int]string{0: "Firefox", 3: "Spotify", 7: "Speakers"} {
		chCfg := loaded.ChannelFor(ch)
		if chCfg == nil {
			t.Errorf("ch %d: no config found", ch)
			continue
		}
		if chCfg.Label != wantLabel {
			t.Errorf("ch %d: label = %q, want %q", ch, chCfg.Label, wantLabel)
		}
	}

	if loaded.ChannelFor(1) != nil {
		t.Errorf("ch 1: expected nil, got config")
	}
}

func TestSaveCreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c", "config.yaml")
	if err := Save(path, &Config{}); err != nil {
		t.Fatalf("Save should create parent dirs: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist after Save: %v", err)
	}
}

func TestMatchStringFor_directMatch(t *testing.T) {
	cfg := &Config{
		Channels: map[string]ChannelConfig{
			"1": {Bind: BindConfig{Type: "playback", Match: "spotify"}},
		},
	}
	if got := cfg.MatchStringFor(1); got != "spotify" {
		t.Errorf("got %q, want %q", got, "spotify")
	}
}

func TestMatchStringFor_outputAlias(t *testing.T) {
	cfg := &Config{
		Outputs: map[string]string{"headphones": "WH-1000XM4"},
		Channels: map[string]ChannelConfig{
			"6": {Bind: BindConfig{Type: "output", Match: "headphones"}},
		},
	}
	if got := cfg.MatchStringFor(6); got != "WH-1000XM4" {
		t.Errorf("got %q, want WH-1000XM4", got)
	}
}

func TestMatchStringFor_missingChannel(t *testing.T) {
	cfg := &Config{}
	if got := cfg.MatchStringFor(4); got != "" {
		t.Errorf("nil map: expected empty, got %q", got)
	}
}

func TestKnobFor_perChannelOverride(t *testing.T) {
	override := &KnobConfig{Type: "gain"}
	cfg := &Config{
		Defaults: DefaultsConfig{PlaybackKnob: KnobConfig{Type: "crossfade"}},
		Channels: map[string]ChannelConfig{
			"2": {Bind: BindConfig{Type: "playback"}, Knob: override},
		},
	}
	knob, ok := cfg.KnobFor(2)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if knob.Type != "gain" {
		t.Errorf("knob type = %q, want gain", knob.Type)
	}
}

func TestKnobFor_defaultForType(t *testing.T) {
	cfg := &Config{
		Defaults: DefaultsConfig{
			PlaybackKnob: KnobConfig{Type: "crossfade", OutputA: "speakers", OutputB: "headphones"},
		},
		Channels: map[string]ChannelConfig{
			"1": {Bind: BindConfig{Type: "playback"}},
		},
	}
	knob, ok := cfg.KnobFor(1)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if knob.Type != "crossfade" || knob.OutputA != "speakers" || knob.OutputB != "headphones" {
		t.Errorf("unexpected knob: %+v", knob)
	}
}

func TestKnobFor_missingChannel(t *testing.T) {
	cfg := &Config{}
	_, ok := cfg.KnobFor(5)
	if ok {
		t.Error("expected ok=false for unconfigured channel")
	}
}

func TestResolveOutput(t *testing.T) {
	cfg := &Config{
		Outputs: map[string]string{"speakers": "Ryzen HD Audio"},
	}
	if got := cfg.ResolveOutput("speakers"); got != "Ryzen HD Audio" {
		t.Errorf("got %q, want Ryzen HD Audio", got)
	}
	if got := cfg.ResolveOutput("unknown"); got != "unknown" {
		t.Errorf("unknown alias should pass through, got %q", got)
	}
}

func TestDefaultPath(t *testing.T) {
	p := DefaultPath()
	if p == "" {
		t.Fatal("DefaultPath returned empty string")
	}
	if filepath.Base(p) != "config.yaml" {
		t.Errorf("DefaultPath = %q, want file named config.yaml", p)
	}
}

func TestDefaultPathXDG(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	got := DefaultPath()
	want := filepath.Join(tmp, "smc-mixer", "config.yaml")
	if got != want {
		t.Errorf("DefaultPath = %q, want %q", got, want)
	}
}

func TestYamlContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &Config{
		MIDI: MIDIConfig{Device: "/dev/midi1"},
		Channels: map[string]ChannelConfig{
			"2": {Label: "VLC", Bind: BindConfig{Type: "playback", Match: "vlc"}},
		},
	}

	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	contents := string(data)
	if !contains(contents, "/dev/midi1") {
		t.Errorf("expected device in yaml output:\n%s", contents)
	}
	if !contains(contents, "VLC") {
		t.Errorf("expected label in yaml output:\n%s", contents)
	}
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
