package config

import "testing"

func TestDeviceKeyForPageUsesFadersOnMainPage(t *testing.T) {
	cfg := &Config{
		Pages: map[string]PageConfig{
			"main": {
				Faders:   map[int]*string{0: sp("fader")},
				Channels: map[int]*string{0: sp("channel")},
			},
		},
	}

	if got := cfg.DeviceKeyForPage("main", 0); got != "fader" {
		t.Fatalf("DeviceKeyForPage(main, 0) = %q, want fader", got)
	}
}

func TestDeviceKeyForPageUsesChannelsOffMainPage(t *testing.T) {
	cfg := &Config{
		Pages: map[string]PageConfig{
			"applications": {
				Faders:   map[int]*string{0: sp("fader")},
				Channels: map[int]*string{0: sp("channel")},
			},
		},
	}

	if got := cfg.DeviceKeyForPage("applications", 0); got != "channel" {
		t.Fatalf("DeviceKeyForPage(applications, 0) = %q, want channel", got)
	}
}

func TestDeviceKeyForPageNilSlotMapsReturnEmpty(t *testing.T) {
	cfg := &Config{
		Pages: map[string]PageConfig{
			"main":         {},
			"applications": {},
		},
	}

	if got := cfg.DeviceKeyForPage("main", 0); got != "" {
		t.Fatalf("main page nil faders returned %q, want empty", got)
	}
	if got := cfg.DeviceKeyForPage("applications", 0); got != "" {
		t.Fatalf("non-main page nil channels returned %q, want empty", got)
	}
}

func TestKnobDeviceForNilKnobMapReturnsNil(t *testing.T) {
	cfg := &Config{
		Devices: map[string]DeviceConfig{
			"dev": {Type: BindPlayback},
		},
		Pages: map[string]PageConfig{
			"main": {},
		},
	}

	if got := cfg.KnobDeviceFor(0); got != nil {
		t.Fatalf("KnobDeviceFor nil knob map = %+v, want nil", got)
	}
}
