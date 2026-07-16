package config

// PromoteCrossfadePages converts legacy non-main pages containing send knobs
// into equivalent in-memory router pages. The on-disk schema remains valid and
// unchanged through Phase 5, while the runtime gives the whole physical strip
// (including its crossfade knob) to one routing world at a time.
func (c *Config) PromoteCrossfadePages() {
	if c == nil {
		return
	}
	c.pagesMu.Lock()
	defer c.pagesMu.Unlock()
	for pageName, page := range c.Pages {
		if pageName == "main" || page.Button == "" || page.Button == "none" {
			continue
		}
		promote := false
		for _, key := range page.Channels {
			if key == nil {
				continue
			}
			if knob, ok := c.knobForDeviceUnlocked(*key); ok && knob.IsSend() {
				promote = true
				break
			}
		}
		if !promote {
			continue
		}
		assignments := make([]AssignmentConfig, 0, len(page.Channels))
		for strip := 0; strip < 8; strip++ {
			key := page.Channels[strip]
			if key == nil {
				continue
			}
			device := c.Devices[*key]
			params := map[string]string{"fader": "volume", "mute": "mute", "solo": "solo"}
			if device.Type == BindPlayback {
				params["stop"] = "playpause"
			}
			if knob, ok := c.knobForDeviceUnlocked(*key); ok && knob.IsSend() {
				params["knob"] = "crossfade"
			}
			assignments = append(assignments, AssignmentConfig{Label: device.Label, Target: "pipewire:rule/" + *key, Params: params})
		}
		c.Router.Pages = append(c.Router.Pages, RouterPageConfig{Name: pageName, Button: page.Button, Assignments: assignments})
		delete(c.Pages, pageName)
	}
}

func (c *Config) knobForDeviceUnlocked(key string) (KnobConfig, bool) {
	device, ok := c.Devices[key]
	if !ok {
		return KnobConfig{}, false
	}
	return c.effectiveKnob(&device), true
}
