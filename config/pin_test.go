package config

import "testing"

func TestPinFaderCreatesMainPageAndFaderMap(t *testing.T) {
	cfg := &Config{}

	cfg.PinFader(2, "spotify")

	if cfg.Pages == nil {
		t.Fatal("PinFader should create Pages")
	}
	got := cfg.Pages["main"].Faders[2]
	if got == nil || *got != "spotify" {
		t.Fatalf("pinned fader = %v, want spotify", got)
	}
}

func TestPinFaderPreservesExistingMainPageKnobs(t *testing.T) {
	cfg := &Config{
		Pages: map[string]PageConfig{
			"main": {Knobs: map[int]*string{1: sp("mic")}},
		},
	}

	cfg.PinFader(0, "spotify")

	if got := cfg.Pages["main"].Knobs[1]; got == nil || *got != "mic" {
		t.Fatalf("existing knob slot = %v, want mic", got)
	}
}

func TestUnpinFaderKeepsDifferentKey(t *testing.T) {
	cfg := &Config{
		Pages: map[string]PageConfig{
			"main": {Faders: map[int]*string{0: sp("spotify")}},
		},
	}

	cfg.UnpinFader(0, "firefox")

	if got := cfg.Pages["main"].Faders[0]; got == nil || *got != "spotify" {
		t.Fatalf("UnpinFader removed mismatched key, got %v", got)
	}
}

func TestUnpinFaderRemovesMatchingKey(t *testing.T) {
	cfg := &Config{
		Pages: map[string]PageConfig{
			"main": {Faders: map[int]*string{0: sp("spotify")}},
		},
	}

	cfg.UnpinFader(0, "spotify")

	if got := cfg.Pages["main"].Faders[0]; got != nil {
		t.Fatalf("UnpinFader left matching key %q", *got)
	}
}
