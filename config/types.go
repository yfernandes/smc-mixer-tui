package config

import (
	"sync"

	"github.com/yfernandes/smc-mixer-tui/audio"
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
