package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"gopkg.in/yaml.v3"
)

// Config is the full on-disk representation of smc-mixer settings.
// Always use *Config; the embedded mutex makes value copies invalid.
type Config struct {
	MIDI     MIDIConfig              `yaml:"midi"`
	Defaults DefaultsConfig          `yaml:"defaults"`
	Devices  map[string]DeviceConfig `yaml:"devices"`
	Pages    map[string]PageConfig   `yaml:"pages"`
	pagesMu  sync.RWMutex            // protects Pages; Devices is read-only after Load
}

// MIDIConfig holds hardware settings.
type MIDIConfig struct {
	Device string `yaml:"device"` // e.g. "/dev/midi1"; "" triggers auto-detect
}

// DefaultsConfig sets the default knob behaviour per device type.
type DefaultsConfig struct {
	InputKnob    KnobConfig `yaml:"input-knob"`
	PlaybackKnob KnobConfig `yaml:"playback-knob"`
	OutputKnob   KnobConfig `yaml:"output-knob"`
}

// KnobConfig describes how a hardware knob is used.
type KnobConfig struct {
	Type KnobType `yaml:"type"`            // "gain", "send", or "none"
	BusA string   `yaml:"bus-a,omitempty"` // device key for send bus A
	BusB string   `yaml:"bus-b,omitempty"` // device key for send bus B
}

// KnobType describes how a channel knob behaves.
type KnobType string

const (
	KnobGain KnobType = "gain"
	KnobSend KnobType = "send"
	KnobNone KnobType = "none"
)

func (k KnobConfig) IsSend() bool { return k.Type == KnobSend }

// DeviceConfig describes a named audio device that can be assigned to a channel slot.
type DeviceConfig struct {
	Label      string          `yaml:"label"`
	Type       BindType        `yaml:"type"`                  // "input", "playback", or "output"
	Match      string          `yaml:"match,omitempty"`       // case-insensitive substring on Name/BindKey
	MatchRegex string          `yaml:"match-regex,omitempty"` // regex applied to stream Name/BindKey
	MatchTitle string          `yaml:"match-title,omitempty"` // case-insensitive substring on window title
	Knob       *KnobConfig     `yaml:"knob,omitempty"`        // per-device override; nil = use default for type
	Advanced   *AdvancedConfig `yaml:"advanced,omitempty"`    // reserved; not yet implemented
}

// AdvancedConfig holds future per-device advanced behaviors. Not yet implemented.
type AdvancedConfig struct {
	Fader      *ControlConfig `yaml:"fader,omitempty"`
	Knob       *ControlConfig `yaml:"knob,omitempty"`
	MuteButton *ControlConfig `yaml:"mute-button,omitempty"`
	SoloButton *ControlConfig `yaml:"solo-button,omitempty"`
	StopButton *ControlConfig `yaml:"stop-button,omitempty"`
}

// ControlConfig holds the shape for a control action or effect. Not yet implemented.
type ControlConfig struct {
	Type   string `yaml:"type,omitempty"`
	Effect string `yaml:"effect,omitempty"`
	Action string `yaml:"action,omitempty"`
}

// BindType describes which kind of audio node a device represents.
type BindType string

const (
	BindInput    BindType = "input"
	BindPlayback BindType = "playback"
	BindOutput   BindType = "output"
)

func (d DeviceConfig) IsOutput() bool { return d.Type == BindOutput }

func (d DeviceConfig) AudioKind() (audio.NodeKind, bool) {
	switch d.Type {
	case BindInput:
		return audio.KindMic, true
	case BindPlayback:
		return audio.KindSource, true
	case BindOutput:
		return audio.KindSink, true
	default:
		return 0, false
	}
}

// PageConfig describes one page of the mixer. The main page has independent
// fader and knob slot maps; other pages use a single channels map.
type PageConfig struct {
	Button   string          `yaml:"button"`
	Faders   map[int]*string `yaml:"faders,omitempty"`
	Knobs    map[int]*string `yaml:"knobs,omitempty"`
	Channels map[int]*string `yaml:"channels,omitempty"`
}

// Validate checks the config for semantic errors.
func (c *Config) Validate() error {
	for key, dev := range c.Devices {
		if err := validateDeviceConfig(dev, key); err != nil {
			return err
		}
		if dev.Knob != nil {
			if err := c.validateKnobConfig(*dev.Knob, "device "+key+" knob"); err != nil {
				return err
			}
		}
	}
	for label, knob := range map[string]KnobConfig{
		"defaults.input-knob":    c.Defaults.InputKnob,
		"defaults.playback-knob": c.Defaults.PlaybackKnob,
		"defaults.output-knob":   c.Defaults.OutputKnob,
	} {
		if knob.Type != "" {
			if err := c.validateKnobConfig(knob, label); err != nil {
				return err
			}
		}
	}
	for pageName, page := range c.Pages {
		for pos, key := range page.Faders {
			if key != nil && *key != "" {
				if _, ok := c.Devices[*key]; !ok {
					return fmt.Errorf("page %s fader %d: unknown device %q", pageName, pos, *key)
				}
			}
		}
		for pos, key := range page.Knobs {
			if key != nil && *key != "" {
				if _, ok := c.Devices[*key]; !ok {
					return fmt.Errorf("page %s knob %d: unknown device %q", pageName, pos, *key)
				}
			}
		}
		for pos, key := range page.Channels {
			if key != nil && *key != "" {
				if _, ok := c.Devices[*key]; !ok {
					return fmt.Errorf("page %s channel %d: unknown device %q", pageName, pos, *key)
				}
			}
		}
	}
	return nil
}

func validateDeviceConfig(d DeviceConfig, key string) error {
	switch d.Type {
	case BindInput, BindPlayback, BindOutput:
	default:
		return fmt.Errorf("device %s: unknown type %q", key, d.Type)
	}
	if d.MatchRegex != "" {
		if _, err := regexp.Compile("(?i)" + d.MatchRegex); err != nil {
			return fmt.Errorf("device %s: invalid match-regex %q: %w", key, d.MatchRegex, err)
		}
	}
	return nil
}

func (c *Config) validateKnobConfig(k KnobConfig, loc string) error {
	switch k.Type {
	case KnobGain, KnobSend, KnobNone, "":
	default:
		return fmt.Errorf("%s: unknown knob type %q", loc, k.Type)
	}
	if k.IsSend() {
		if k.BusA != "" {
			if c.DeviceFor(k.BusA) == nil {
				return fmt.Errorf("%s: bus-a device %q not found in devices", loc, k.BusA)
			}
		}
		if k.BusB != "" {
			if c.DeviceFor(k.BusB) == nil {
				return fmt.Errorf("%s: bus-b device %q not found in devices", loc, k.BusB)
			}
		}
	}
	return nil
}

// DefaultPath returns the canonical config file location:
// $XDG_CONFIG_HOME/smc-mixer/config.yaml, falling back to ~/.config/…
func DefaultPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "config.yaml"
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "smc-mixer", "config.yaml")
}

// Load reads the YAML file at path.
// If the file does not exist, an empty Config is returned (not an error).
func Load(path string) (*Config, error) {
	cfg := &Config{}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := yaml.NewDecoder(f).Decode(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save writes cfg to path as YAML, creating parent directories as needed.
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	return enc.Encode(cfg)
}

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
	c.pagesMu.RLock()
	key := c.faderKeyFor(ch)
	c.pagesMu.RUnlock()
	return c.DeviceFor(key)
}

// faderKeyFor returns the device key for fader ch on the main page (caller must hold pagesMu).
func (c *Config) faderKeyFor(ch int) string {
	if c.Pages == nil {
		return ""
	}
	page, ok := c.Pages["main"]
	if !ok {
		return ""
	}
	key := page.Faders[ch]
	if key == nil {
		return ""
	}
	return *key
}

// knobKeyFor returns the device key for knob ch on the main page (caller must hold pagesMu).
func (c *Config) knobKeyFor(ch int) string {
	if c.Pages == nil {
		return ""
	}
	page, ok := c.Pages["main"]
	if !ok {
		return ""
	}
	key := page.Knobs[ch]
	if key == nil {
		return ""
	}
	return *key
}

// ChannelForPage returns the DeviceConfig for position ch on the named page.
// For "main", faders are used; for other pages, channels are used. Returns nil for nil slots.
func (c *Config) ChannelForPage(page string, ch int) *DeviceConfig {
	c.pagesMu.RLock()
	if c.Pages == nil {
		c.pagesMu.RUnlock()
		return nil
	}
	p, ok := c.Pages[page]
	if !ok {
		c.pagesMu.RUnlock()
		return nil
	}
	var key *string
	if page == "main" {
		key = p.Faders[ch]
	} else {
		key = p.Channels[ch]
	}
	c.pagesMu.RUnlock()
	if key == nil {
		return nil
	}
	return c.DeviceFor(*key)
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
	c.pagesMu.RLock()
	key := c.knobKeyFor(ch)
	c.pagesMu.RUnlock()
	return c.DeviceFor(key)
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

// ResolveOutput resolves a device key to its match string (the PipeWire device description).
// If the key is not found in devices, the key itself is returned.
func (c *Config) ResolveOutput(key string) string {
	if dev := c.DeviceFor(key); dev != nil {
		return dev.Match
	}
	return key
}

// DeviceKeyForPage returns the config device key for slot ch on the given page.
// For "main" it uses the fader map; for other pages it uses the channel map.
// Returns "" if no device is assigned at that position.
func (c *Config) DeviceKeyForPage(page string, ch int) string {
	c.pagesMu.RLock()
	defer c.pagesMu.RUnlock()
	if c.Pages == nil {
		return ""
	}
	p, ok := c.Pages[page]
	if !ok {
		return ""
	}
	var key *string
	if page == "main" {
		key = p.Faders[ch]
	} else {
		key = p.Channels[ch]
	}
	if key == nil {
		return ""
	}
	return *key
}

// PinFader sets pages.main.faders[ch] = key, creating the page and map as needed.
func (c *Config) PinFader(ch int, key string) {
	c.pagesMu.Lock()
	defer c.pagesMu.Unlock()
	if c.Pages == nil {
		c.Pages = make(map[string]PageConfig)
	}
	page := c.Pages["main"]
	if page.Faders == nil {
		page.Faders = make(map[int]*string)
	}
	k := key
	page.Faders[ch] = &k
	c.Pages["main"] = page
}

// UnpinFader removes pages.main.faders[ch] if it currently matches key.
func (c *Config) UnpinFader(ch int, key string) {
	c.pagesMu.Lock()
	defer c.pagesMu.Unlock()
	if c.Pages == nil {
		return
	}
	page, ok := c.Pages["main"]
	if !ok {
		return
	}
	if existing := page.Faders[ch]; existing != nil && *existing == key {
		delete(page.Faders, ch)
		c.Pages["main"] = page
	}
}
