package main

import (
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/ui"
)

// computeStripConfigs derives per-channel split info from the main page config.
// A strip is split when its fader and knob slots reference different device keys.
func computeStripConfigs(cfg *config.Config) [8]ui.StripConfig {
	var cfgs [8]ui.StripConfig
	mainPage, ok := mainPageConfig(cfg)
	if !ok {
		return cfgs
	}
	for ch := range 8 {
		cfgs[ch] = stripConfigForSlot(cfg, mainPage, ch)
	}
	return cfgs
}

func mainPageConfig(cfg *config.Config) (config.PageConfig, bool) {
	if cfg.Pages == nil {
		return config.PageConfig{}, false
	}
	page, ok := cfg.Pages["main"]
	return page, ok
}

func stripConfigForSlot(cfg *config.Config, page config.PageConfig, ch int) ui.StripConfig {
	faderKey := slotKey(page.Faders, ch)
	knobKey := slotKey(page.Knobs, ch)

	// Split whenever knob has a config device and the fader is either unset
	// (dynamic) or targets a different device. Same-device channels stay unified.
	if knobKey == "" || faderKey == knobKey {
		return ui.StripConfig{}
	}

	strip := ui.StripConfig{IsSplit: true}
	if dev := cfg.DeviceFor(knobKey); dev != nil {
		strip.KnobLabel = dev.Label
		strip.KnobType = string(dev.Type)
	}
	if faderKey != "" {
		if dev := cfg.DeviceFor(faderKey); dev != nil {
			strip.FaderLabel = dev.Label
			strip.FaderType = string(dev.Type)
		}
	}
	return strip
}

func slotKey(slots map[int]*string, ch int) string {
	if slots == nil {
		return ""
	}
	key := slots[ch]
	if key == nil {
		return ""
	}
	return *key
}
