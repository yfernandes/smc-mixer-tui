package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissing(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
}

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "smc-mixer", "config.toml")

	orig := &Config{
		MIDI: MIDIConfig{Device: "/dev/midi2"},
	}
	orig.SetStream(0, "Firefox")
	orig.SetStream(3, "Spotify")
	orig.SetStream(7, "discord")

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
	for ch, want := range map[int]string{0: "Firefox", 3: "Spotify", 7: "discord"} {
		if got := loaded.StreamFor(ch); got != want {
			t.Errorf("ch %d: stream = %q, want %q", ch, got, want)
		}
	}
	// unset channel
	if got := loaded.StreamFor(1); got != "" {
		t.Errorf("ch 1: expected empty, got %q", got)
	}
}

func TestSaveCreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c", "config.toml")
	if err := Save(path, &Config{}); err != nil {
		t.Fatalf("Save should create parent dirs: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist after Save: %v", err)
	}
}

func TestSetStreamClearsEmpty(t *testing.T) {
	cfg := &Config{}
	cfg.SetStream(0, "Firefox")
	cfg.SetStream(0, "") // clear it
	if got := cfg.StreamFor(0); got != "" {
		t.Errorf("cleared binding should return empty, got %q", got)
	}
}

func TestStreamForNilMap(t *testing.T) {
	cfg := &Config{}
	if got := cfg.StreamFor(4); got != "" {
		t.Errorf("nil map: expected empty, got %q", got)
	}
}

func TestDefaultPath(t *testing.T) {
	// Just verify it returns a non-empty path ending in config.toml
	p := DefaultPath()
	if p == "" {
		t.Fatal("DefaultPath returned empty string")
	}
	if filepath.Base(p) != "config.toml" {
		t.Errorf("DefaultPath = %q, want file named config.toml", p)
	}
}

func TestDefaultPathXDG(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	got := DefaultPath()
	want := filepath.Join(tmp, "smc-mixer", "config.toml")
	if got != want {
		t.Errorf("DefaultPath = %q, want %q", got, want)
	}
}

func TestTomlContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := &Config{MIDI: MIDIConfig{Device: "/dev/midi1"}}
	cfg.SetStream(2, "VLC")

	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	contents := string(data)
	if !contains(contents, "/dev/midi1") {
		t.Errorf("expected device in toml output:\n%s", contents)
	}
	if !contains(contents, "VLC") {
		t.Errorf("expected stream name in toml output:\n%s", contents)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsLoop(s, sub))
}

func containsLoop(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
