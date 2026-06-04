package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/yfernandes/smc-mixer-tui/audio"
)

func sp(s string) *string { return &s }

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
		Devices: map[string]DeviceConfig{
			"firefox":  {Label: "Firefox", Type: BindPlayback, Match: "firefox"},
			"spotify":  {Label: "Spotify", Type: BindPlayback, Match: "spotify"},
			"speakers": {Label: "Speakers", Type: BindOutput, Match: "Built-in Audio"},
		},
		Pages: map[string]PageConfig{
			"main": {
				Button: "none",
				Faders: map[int]*string{0: sp("firefox"), 3: sp("spotify"), 7: sp("speakers")},
				Knobs:  map[int]*string{},
			},
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
		dev := loaded.ChannelFor(ch)
		if dev == nil {
			t.Errorf("ch %d: no device found", ch)
			continue
		}
		if dev.Label != wantLabel {
			t.Errorf("ch %d: label = %q, want %q", ch, dev.Label, wantLabel)
		}
	}

	if loaded.ChannelFor(1) != nil {
		t.Errorf("ch 1: expected nil, got device")
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

func TestChannelFor_mainPageFader(t *testing.T) {
	cfg := &Config{
		Devices: map[string]DeviceConfig{
			"spotify": {Label: "Spotify", Type: BindPlayback, Match: "spotify"},
		},
		Pages: map[string]PageConfig{
			"main": {Faders: map[int]*string{1: sp("spotify")}},
		},
	}
	dev := cfg.ChannelFor(1)
	if dev == nil {
		t.Fatal("expected device at fader 1")
	}
	if dev.Label != "Spotify" {
		t.Errorf("label = %q, want Spotify", dev.Label)
	}
}

func TestChannelFor_nullFaderReturnsNil(t *testing.T) {
	cfg := &Config{
		Pages: map[string]PageConfig{
			"main": {Faders: map[int]*string{0: nil}},
		},
	}
	if cfg.ChannelFor(0) != nil {
		t.Error("nil fader slot should return nil device")
	}
}

func TestMatchStringFor_directMatch(t *testing.T) {
	cfg := &Config{
		Devices: map[string]DeviceConfig{
			"sp": {Type: BindPlayback, Match: "spotify"},
		},
		Pages: map[string]PageConfig{
			"main": {Faders: map[int]*string{1: sp("sp")}},
		},
	}
	if got := cfg.MatchStringFor(1); got != "spotify" {
		t.Errorf("got %q, want %q", got, "spotify")
	}
}

func TestMatchStringFor_outputDevice(t *testing.T) {
	cfg := &Config{
		Devices: map[string]DeviceConfig{
			"headphones": {Type: BindOutput, Match: "WH-1000XM4"},
		},
		Pages: map[string]PageConfig{
			"main": {Faders: map[int]*string{6: sp("headphones")}},
		},
	}
	if got := cfg.MatchStringFor(6); got != "WH-1000XM4" {
		t.Errorf("got %q, want WH-1000XM4", got)
	}
}

func TestMatchStringFor_missingFader(t *testing.T) {
	cfg := &Config{}
	if got := cfg.MatchStringFor(4); got != "" {
		t.Errorf("nil pages: expected empty, got %q", got)
	}
}

func TestKnobFor_perDeviceOverride(t *testing.T) {
	override := &KnobConfig{Type: KnobGain}
	cfg := &Config{
		Defaults: DefaultsConfig{PlaybackKnob: KnobConfig{Type: KnobSend}},
		Devices: map[string]DeviceConfig{
			"dev": {Type: BindPlayback, Knob: override},
		},
		Pages: map[string]PageConfig{
			"main": {Knobs: map[int]*string{2: sp("dev")}},
		},
	}
	knob, ok := cfg.KnobFor(2)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if knob.Type != KnobGain {
		t.Errorf("knob type = %q, want gain", knob.Type)
	}
}

func TestKnobFor_defaultForType(t *testing.T) {
	cfg := &Config{
		Defaults: DefaultsConfig{
			PlaybackKnob: KnobConfig{Type: KnobSend, BusA: "speakers", BusB: "headphones"},
		},
		Devices: map[string]DeviceConfig{
			"sp": {Type: BindPlayback},
		},
		Pages: map[string]PageConfig{
			"main": {Knobs: map[int]*string{1: sp("sp")}},
		},
	}
	knob, ok := cfg.KnobFor(1)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if knob.Type != KnobSend || knob.BusA != "speakers" || knob.BusB != "headphones" {
		t.Errorf("unexpected knob: %+v", knob)
	}
}

func TestKnobFor_missingKnobSlot(t *testing.T) {
	cfg := &Config{}
	_, ok := cfg.KnobFor(5)
	if ok {
		t.Error("expected ok=false for unconfigured knob slot")
	}
}

func TestKnobConfigIsSend(t *testing.T) {
	if !(KnobConfig{Type: KnobSend}).IsSend() {
		t.Fatal("send knob should report true")
	}
	if (KnobConfig{Type: KnobGain}).IsSend() {
		t.Fatal("gain knob should report false")
	}
}

func TestDeviceConfigAudioKind(t *testing.T) {
	cases := []struct {
		name string
		dev  DeviceConfig
		want audio.NodeKind
	}{
		{"input", DeviceConfig{Type: BindInput}, audio.KindMic},
		{"playback", DeviceConfig{Type: BindPlayback}, audio.KindSource},
		{"output", DeviceConfig{Type: BindOutput}, audio.KindSink},
	}

	for _, c := range cases {
		got, ok := c.dev.AudioKind()
		if !ok {
			t.Fatalf("%s: expected audio kind", c.name)
		}
		if got != c.want {
			t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDeviceConfigAudioKindUnknown(t *testing.T) {
	if _, ok := (DeviceConfig{Type: "custom"}).AudioKind(); ok {
		t.Fatal("unknown device type should not report an audio kind")
	}
}

func TestKnobDeviceFor_returnsKnobSlotDevice(t *testing.T) {
	cfg := &Config{
		Devices: map[string]DeviceConfig{
			"fifine": {Label: "Fifine", Type: BindInput, Match: "fifine"},
		},
		Pages: map[string]PageConfig{
			"main": {Knobs: map[int]*string{0: sp("fifine")}},
		},
	}
	dev := cfg.KnobDeviceFor(0)
	if dev == nil || dev.Label != "Fifine" {
		t.Fatalf("KnobDeviceFor(0) = %v, want Fifine device", dev)
	}
	if cfg.KnobDeviceFor(1) != nil {
		t.Error("KnobDeviceFor unassigned slot should return nil")
	}
}

func TestKnobForPage_mainDelegatesToKnobSlot(t *testing.T) {
	cfg := &Config{
		Devices: map[string]DeviceConfig{
			"fifine": {Label: "Fifine", Type: BindInput},
		},
		Defaults: DefaultsConfig{InputKnob: KnobConfig{Type: KnobGain}},
		Pages: map[string]PageConfig{
			"main": {Knobs: map[int]*string{0: sp("fifine")}},
		},
	}
	knob, ok := cfg.KnobForPage("main", 0)
	if !ok {
		t.Fatal("expected ok=true for main page knob slot")
	}
	if knob.Type != KnobGain {
		t.Errorf("knob type = %q, want gain", knob.Type)
	}
}

func TestKnobForPage_nonMainUsesChannelDevice(t *testing.T) {
	cfg := &Config{
		Devices: map[string]DeviceConfig{
			"spotify": {Type: BindPlayback},
		},
		Defaults: DefaultsConfig{
			PlaybackKnob: KnobConfig{Type: KnobSend, BusA: "spk", BusB: "hp"},
		},
		Pages: map[string]PageConfig{
			"applications": {Channels: map[int]*string{0: sp("spotify")}},
		},
	}
	knob, ok := cfg.KnobForPage("applications", 0)
	if !ok {
		t.Fatal("expected ok=true for applications page channel")
	}
	if knob.Type != KnobSend {
		t.Errorf("knob type = %q, want send", knob.Type)
	}
}

func TestKnobForPage_nonMainPerDeviceOverride(t *testing.T) {
	override := &KnobConfig{Type: KnobGain}
	cfg := &Config{
		Devices: map[string]DeviceConfig{
			"mic": {Type: BindInput, Knob: override},
		},
		Defaults: DefaultsConfig{InputKnob: KnobConfig{Type: KnobNone}},
		Pages: map[string]PageConfig{
			"inputs": {Channels: map[int]*string{0: sp("mic")}},
		},
	}
	knob, ok := cfg.KnobForPage("inputs", 0)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if knob.Type != KnobGain {
		t.Errorf("knob type = %q, want gain (per-device override)", knob.Type)
	}
}

func TestKnobForPage_missingSlotReturnsFalse(t *testing.T) {
	cfg := &Config{}
	_, ok := cfg.KnobForPage("applications", 3)
	if ok {
		t.Error("empty page slot should return ok=false")
	}
}

func TestResolveOutput(t *testing.T) {
	cfg := &Config{
		Devices: map[string]DeviceConfig{
			"speakers": {Type: BindOutput, Match: "Ryzen HD Audio"},
		},
	}
	if got := cfg.ResolveOutput("speakers"); got != "Ryzen HD Audio" {
		t.Errorf("got %q, want Ryzen HD Audio", got)
	}
	if got := cfg.ResolveOutput("unknown"); got != "unknown" {
		t.Errorf("unknown key should pass through, got %q", got)
	}
}

func TestValidateAcceptsWellFormedConfig(t *testing.T) {
	cfg := &Config{
		Defaults: DefaultsConfig{
			PlaybackKnob: KnobConfig{Type: KnobSend, BusA: "speakers", BusB: "headphones"},
			InputKnob:    KnobConfig{Type: KnobGain},
		},
		Devices: map[string]DeviceConfig{
			"spotify":    {Type: BindPlayback, Match: "spotify"},
			"firefox":    {Type: BindPlayback, MatchRegex: "firefox.*"},
			"speakers":   {Type: BindOutput, Match: "Ryzen HD Audio Analog Stereo"},
			"headphones": {Type: BindOutput, Match: "WH-1000XM4"},
		},
		Pages: map[string]PageConfig{
			"main": {
				Faders: map[int]*string{0: sp("spotify"), 3: sp("firefox"), 7: sp("speakers")},
				Knobs:  map[int]*string{6: sp("headphones"), 7: sp("speakers")},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("well-formed config should pass validation: %v", err)
	}
}

func TestValidateRejectsUnknownDeviceType(t *testing.T) {
	cfg := &Config{
		Devices: map[string]DeviceConfig{
			"dev": {Type: "recording"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("unknown device type should fail validation")
	}
}

func TestValidateRejectsInvalidRegex(t *testing.T) {
	cfg := &Config{
		Devices: map[string]DeviceConfig{
			"dev": {Type: BindPlayback, MatchRegex: "[unclosed"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("invalid match-regex should fail validation")
	}
}

func TestValidateRejectsUnknownKnobType(t *testing.T) {
	cfg := &Config{
		Devices: map[string]DeviceConfig{
			"dev": {
				Type: BindPlayback,
				Knob: &KnobConfig{Type: "equalizer"},
			},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("unknown per-device knob type should fail validation")
	}
}

func TestValidateRejectsDanglingBusAInDevice(t *testing.T) {
	cfg := &Config{
		Devices: map[string]DeviceConfig{
			"sp":       {Type: BindPlayback, Knob: &KnobConfig{Type: KnobSend, BusA: "speakers", BusB: "ghost"}},
			"speakers": {Type: BindOutput, Match: "Ryzen HD Audio"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("send knob with unknown bus-b should fail validation")
	}
}

func TestValidateRejectsDanglingBusInDefaults(t *testing.T) {
	cfg := &Config{
		Devices: map[string]DeviceConfig{
			"speakers": {Type: BindOutput, Match: "Ryzen HD Audio"},
		},
		Defaults: DefaultsConfig{
			PlaybackKnob: KnobConfig{Type: KnobSend, BusA: "speakers", BusB: "headphones"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("default send knob with missing bus-b device should fail validation")
	}
}

func TestValidateRejectsUnknownDefaultKnobType(t *testing.T) {
	cfg := &Config{
		Defaults: DefaultsConfig{
			InputKnob: KnobConfig{Type: "boost"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("unknown default knob type should fail validation")
	}
}

func TestValidateRejectsUnknownPageFaderDevice(t *testing.T) {
	cfg := &Config{
		Devices: map[string]DeviceConfig{},
		Pages: map[string]PageConfig{
			"main": {Faders: map[int]*string{0: sp("ghost")}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("page fader referencing unknown device should fail validation")
	}
}

func TestValidateRejectsUnknownPageKnobDevice(t *testing.T) {
	cfg := &Config{
		Devices: map[string]DeviceConfig{},
		Pages: map[string]PageConfig{
			"main": {Knobs: map[int]*string{0: sp("ghost")}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("page knob referencing unknown device should fail validation")
	}
}

func TestValidateRejectsUnknownPageChannelDevice(t *testing.T) {
	cfg := &Config{
		Devices: map[string]DeviceConfig{},
		Pages: map[string]PageConfig{
			"apps": {Channels: map[int]*string{0: sp("ghost")}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("page channel referencing unknown device should fail validation")
	}
}

func TestValidateAcceptsNullSlots(t *testing.T) {
	cfg := &Config{
		Pages: map[string]PageConfig{
			"main": {
				Faders:   map[int]*string{0: nil, 1: nil},
				Knobs:    map[int]*string{0: nil},
				Channels: map[int]*string{},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("null slots should pass validation: %v", err)
	}
}

// TestPinFaderConcurrentWithReaders verifies fix 4: PinFader and UnpinFader
// protect cfg.Pages with pagesMu so concurrent readers don't race with writes.
func TestPinFaderConcurrentWithReaders(t *testing.T) {
	cfg := &Config{
		Devices: map[string]DeviceConfig{
			"dev0": {Type: BindPlayback, Match: "spotify"},
		},
		Pages: map[string]PageConfig{
			"main": {Faders: map[int]*string{0: sp("dev0")}},
		},
	}

	const iterations = 500
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: rapidly pin and unpin slot 0.
	go func() {
		defer wg.Done()
		for i := range iterations {
			if i%2 == 0 {
				cfg.PinFader(0, "dev0")
			} else {
				cfg.UnpinFader(0, "dev0")
			}
		}
	}()

	// Reader: concurrently read Pages via public methods.
	go func() {
		defer wg.Done()
		for range iterations {
			cfg.ChannelForPage("main", 0)
			cfg.DeviceKeyForPage("main", 0)
		}
	}()

	wg.Wait()
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
		Devices: map[string]DeviceConfig{
			"vlc": {Label: "VLC", Type: BindPlayback, Match: "vlc"},
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
