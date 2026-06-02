package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"gopkg.in/yaml.v3"
)

// Config is the full on-disk representation of smc-mixer settings.
type Config struct {
	MIDI     MIDIConfig               `yaml:"midi"`
	Defaults DefaultsConfig           `yaml:"defaults"`
	Outputs  map[string]string        `yaml:"outputs"`  // alias → full device description
	Channels map[string]ChannelConfig `yaml:"channels"` // "0"–"7" → channel config
}

// MIDIConfig holds hardware settings.
type MIDIConfig struct {
	Device string `yaml:"device"` // e.g. "/dev/midi1"; "" triggers auto-detect
}

// DefaultsConfig sets the default knob behaviour per channel bind type.
type DefaultsConfig struct {
	InputKnob    KnobConfig `yaml:"input-knob"`
	PlaybackKnob KnobConfig `yaml:"playback-knob"`
	OutputKnob   KnobConfig `yaml:"output-knob"`
}

// KnobConfig describes how the hardware knob on a channel is used.
type KnobConfig struct {
	Type    KnobType `yaml:"type"`               // "gain", "crossfade", or "none"
	OutputA string   `yaml:"output-a,omitempty"` // output alias (from Outputs); crossfade only
	OutputB string   `yaml:"output-b,omitempty"`
}

// KnobType describes how a channel knob behaves.
type KnobType string

const (
	KnobGain      KnobType = "gain"
	KnobCrossfade KnobType = "crossfade"
	KnobNone      KnobType = "none"
)

func (k KnobConfig) IsCrossfade() bool {
	return k.Type == KnobCrossfade
}

// ChannelConfig describes one mixer channel strip.
type ChannelConfig struct {
	Label string      `yaml:"label"`
	Bind  BindConfig  `yaml:"bind"`
	Knob  *KnobConfig `yaml:"knob,omitempty"` // per-channel override; nil = use default for bind type
}

// BindConfig describes how a channel finds its PipeWire stream.
type BindConfig struct {
	Type       BindType `yaml:"type"`                  // "input", "playback", or "output"
	Match      string   `yaml:"match,omitempty"`       // case-insensitive substring
	MatchRegex string   `yaml:"match-regex,omitempty"` // regex applied to stream name/BindKey
}

// BindType describes which kind of audio node a channel can bind to.
type BindType string

const (
	BindInput    BindType = "input"
	BindPlayback BindType = "playback"
	BindOutput   BindType = "output"
)

func (b BindConfig) IsOutput() bool {
	return b.Type == BindOutput
}

func (b BindConfig) AudioKind() (audio.NodeKind, bool) {
	switch b.Type {
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

// ChannelFor returns the ChannelConfig for channel ch, or nil if not configured.
func (c *Config) ChannelFor(ch int) *ChannelConfig {
	if c.Channels == nil {
		return nil
	}
	cfg, ok := c.Channels[strconv.Itoa(ch)]
	if !ok {
		return nil
	}
	return &cfg
}

// MatchStringFor returns the effective match substring for channel ch.
// For output-type channels, the match value is treated as an output alias and
// resolved to the full device description via the Outputs map.
func (c *Config) MatchStringFor(ch int) string {
	chCfg := c.ChannelFor(ch)
	if chCfg == nil {
		return ""
	}
	match := chCfg.Bind.Match
	if chCfg.Bind.IsOutput() && match != "" {
		if desc, ok := c.Outputs[match]; ok {
			return desc
		}
	}
	return match
}

// KnobFor returns the effective KnobConfig for channel ch.
// If the channel has a per-channel knob override it takes precedence; otherwise
// the default for the channel's bind type is returned.
// The second return value reports whether any config exists for the channel.
func (c *Config) KnobFor(ch int) (KnobConfig, bool) {
	chCfg := c.ChannelFor(ch)
	if chCfg == nil {
		return KnobConfig{}, false
	}
	if chCfg.Knob != nil {
		return *chCfg.Knob, true
	}
	switch chCfg.Bind.Type {
	case BindInput:
		return c.Defaults.InputKnob, true
	case BindPlayback:
		return c.Defaults.PlaybackKnob, true
	case BindOutput:
		return c.Defaults.OutputKnob, true
	}
	return KnobConfig{}, true
}

// ResolveOutput resolves an output alias to its device description.
// If the alias is not found in the Outputs map, the alias itself is returned.
func (c *Config) ResolveOutput(alias string) string {
	if c.Outputs != nil {
		if desc, ok := c.Outputs[alias]; ok {
			return desc
		}
	}
	return alias
}
