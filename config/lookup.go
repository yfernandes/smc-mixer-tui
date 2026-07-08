package config

import "sort"

// DeviceFor looks up a device by key. Returns nil if not found.
func (c *Config) DeviceFor(key string) *DeviceConfig {
	if c.Devices == nil {
		return nil
	}
	d, ok := c.Devices[key]
	if !ok {
		return nil
	}
	return &d
}

// ChannelFor returns the DeviceConfig for fader position ch in the main page, or nil.
func (c *Config) ChannelFor(ch int) *DeviceConfig {
	return c.DeviceFor(c.deviceKeyForPageLocked("main", ch, slotFader))
}

// ChannelForPage returns the DeviceConfig for position ch on the named page.
// For "main", faders are used; for other pages, channels are used. Returns nil for nil slots.
func (c *Config) ChannelForPage(page string, ch int) *DeviceConfig {
	return c.DeviceFor(c.DeviceKeyForPage(page, ch))
}

// MatchStringForPage returns the match string for position ch on the named page.
func (c *Config) MatchStringForPage(page string, ch int) string {
	dev := c.ChannelForPage(page, ch)
	if dev == nil {
		return ""
	}
	return dev.Match
}

// MatchStringFor returns the match string for fader position ch in the main page.
func (c *Config) MatchStringFor(ch int) string {
	dev := c.ChannelFor(ch)
	if dev == nil {
		return ""
	}
	return dev.Match
}

// KnobDeviceFor returns the DeviceConfig for knob position ch in the main page, or nil.
func (c *Config) KnobDeviceFor(ch int) *DeviceConfig {
	return c.DeviceFor(c.deviceKeyForPageLocked("main", ch, slotKnob))
}

// KnobFor returns the effective KnobConfig for knob position ch in the main page.
// The second return value reports whether any device is assigned at that position.
func (c *Config) KnobFor(ch int) (KnobConfig, bool) {
	dev := c.KnobDeviceFor(ch)
	if dev == nil {
		return KnobConfig{}, false
	}
	return c.effectiveKnob(dev), true
}

// KnobForPage returns the effective KnobConfig for knob position ch on the named page.
// For "main" it uses the page's independent knob slot map.
// For other pages it derives knob behavior from the channel device with defaults inheritance.
func (c *Config) KnobForPage(page string, ch int) (KnobConfig, bool) {
	if page == "main" {
		return c.KnobFor(ch)
	}
	dev := c.ChannelForPage(page, ch)
	if dev == nil {
		return KnobConfig{}, false
	}
	return c.effectiveKnob(dev), true
}

func (c *Config) effectiveKnob(dev *DeviceConfig) KnobConfig {
	if dev.Knob != nil {
		return *dev.Knob
	}
	switch dev.Type {
	case BindInput:
		return c.Defaults.InputKnob
	case BindPlayback:
		return c.Defaults.PlaybackKnob
	case BindOutput:
		return c.Defaults.OutputKnob
	}
	return KnobConfig{}
}

// KnobForDevice returns the effective KnobConfig for a named device, independent of page context.
func (c *Config) KnobForDevice(deviceKey string) (KnobConfig, bool) {
	dev := c.DeviceFor(deviceKey)
	if dev == nil {
		return KnobConfig{}, false
	}
	return c.effectiveKnob(dev), true
}

// EffectiveSyncMode returns the sync mode for a device, falling back to the global default.
// When both are unset, SyncModeZero is returned.
func (c *Config) EffectiveSyncMode(dev *DeviceConfig) SyncMode {
	if dev != nil && dev.SyncMode != SyncModeDefault {
		return dev.SyncMode
	}
	if c.Defaults.SyncMode != SyncModeDefault {
		return c.Defaults.SyncMode
	}
	return SyncModeZero
}

// EffectivePickupToleranceCC returns the soft pickup tolerance in CC units (1–127).
// If not configured, the default of 2 CC units is returned.
func (c *Config) EffectivePickupToleranceCC() int {
	cc := c.Defaults.PickupToleranceCC
	if cc <= 0 || cc > 127 {
		return 2
	}
	return cc
}

// ResolveOutput resolves a device key to its match string (the PipeWire device description).
// If the key is not found in devices, the key itself is returned.
func (c *Config) ResolveOutput(key string) string {
	if dev := c.DeviceFor(key); dev != nil {
		return dev.Match
	}
	return key
}

// PageNames returns every configured page name, always including "main" even
// if absent from the config file (the daemon treats it as the default page).
func (c *Config) PageNames() []string {
	c.pagesMu.RLock()
	defer c.pagesMu.RUnlock()
	names := make([]string, 0, len(c.Pages)+1)
	seenMain := false
	for name := range c.Pages {
		names = append(names, name)
		if name == "main" {
			seenMain = true
		}
	}
	if !seenMain {
		names = append(names, "main")
	}
	sort.Strings(names)
	return names
}

// DeviceKeyForPage returns the config device key for slot ch on the given page.
// For "main" it uses the fader map; for other pages it uses the channel map.
// Returns "" if no device is assigned at that position.
func (c *Config) DeviceKeyForPage(page string, ch int) string {
	slot := slotChannel
	if page == "main" {
		slot = slotFader
	}
	return c.deviceKeyForPageLocked(page, ch, slot)
}

type pageSlot int

const (
	slotFader pageSlot = iota
	slotKnob
	slotChannel
)

func (c *Config) deviceKeyForPageLocked(page string, ch int, slot pageSlot) string {
	c.pagesMu.RLock()
	defer c.pagesMu.RUnlock()
	if c.Pages == nil {
		return ""
	}
	p, ok := c.Pages[page]
	if !ok {
		return ""
	}
	return slotKeyFor(p, ch, slot)
}

func slotKeyFor(page PageConfig, ch int, slot pageSlot) string {
	var key *string
	switch slot {
	case slotFader:
		key = page.Faders[ch]
	case slotKnob:
		key = page.Knobs[ch]
	case slotChannel:
		key = page.Channels[ch]
	}
	if key == nil {
		return ""
	}
	return *key
}
