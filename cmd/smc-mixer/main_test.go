package main

import (
	"testing"

	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/ui"
)

func strPtr(s string) *string { return &s }

func makeConfig(faders, knobs map[int]*string, devices map[string]config.DeviceConfig) *config.Config {
	return &config.Config{
		Devices: devices,
		Pages: map[string]config.PageConfig{
			"main": {Faders: faders, Knobs: knobs},
		},
	}
}

func TestComputeStripConfigs_Unified(t *testing.T) {
	cfg := makeConfig(
		map[int]*string{0: strPtr("spotify")},
		map[int]*string{0: strPtr("spotify")},
		map[string]config.DeviceConfig{
			"spotify": {Label: "Spotify", Type: "playback"},
		},
	)
	cfgs := computeStripConfigs(cfg)
	if cfgs[0].IsSplit {
		t.Fatal("same fader/knob key should produce unified strip")
	}
}

func TestComputeStripConfigs_SplitStaticFader(t *testing.T) {
	cfg := makeConfig(
		map[int]*string{7: strPtr("speakers")},
		map[int]*string{7: strPtr("fifine")},
		map[string]config.DeviceConfig{
			"speakers": {Label: "Speakers", Type: "output"},
			"fifine":   {Label: "Fifine Mic", Type: "input"},
		},
	)
	cfgs := computeStripConfigs(cfg)
	if !cfgs[7].IsSplit {
		t.Fatal("different fader/knob keys should produce split strip")
	}
	if cfgs[7].KnobLabel != "Fifine Mic" || cfgs[7].KnobType != "input" {
		t.Fatalf("knob zone: got label=%q type=%q, want Fifine Mic/input", cfgs[7].KnobLabel, cfgs[7].KnobType)
	}
	if cfgs[7].FaderLabel != "Speakers" || cfgs[7].FaderType != "output" {
		t.Fatalf("fader zone: got label=%q type=%q, want Speakers/output", cfgs[7].FaderLabel, cfgs[7].FaderType)
	}
}

func TestComputeStripConfigs_SplitDynamicFader(t *testing.T) {
	// fader is nil (dynamic), knob is set → still split
	cfg := makeConfig(
		map[int]*string{0: nil},
		map[int]*string{0: strPtr("fifine")},
		map[string]config.DeviceConfig{
			"fifine": {Label: "Fifine Mic", Type: "input"},
		},
	)
	cfgs := computeStripConfigs(cfg)
	if !cfgs[0].IsSplit {
		t.Fatal("nil fader + set knob should produce split strip")
	}
	if cfgs[0].FaderLabel != "" || cfgs[0].FaderType != "" {
		t.Fatalf("dynamic fader should have empty FaderLabel/FaderType, got %q/%q", cfgs[0].FaderLabel, cfgs[0].FaderType)
	}
}

func TestComputeStripConfigs_NoKnobNoSplit(t *testing.T) {
	// knob is nil → unified regardless of fader
	cfg := makeConfig(
		map[int]*string{0: strPtr("spotify")},
		map[int]*string{0: nil},
		map[string]config.DeviceConfig{
			"spotify": {Label: "Spotify", Type: "playback"},
		},
	)
	cfgs := computeStripConfigs(cfg)
	if cfgs[0].IsSplit {
		t.Fatal("nil knob should never produce split strip")
	}
}

func TestComputeStripConfigs_NoPages(t *testing.T) {
	cfg := &config.Config{}
	cfgs := computeStripConfigs(cfg)
	for i, c := range cfgs {
		if c != (ui.StripConfig{}) {
			t.Fatalf("cfgs[%d] should be zero with no pages config, got %+v", i, c)
		}
	}
}
