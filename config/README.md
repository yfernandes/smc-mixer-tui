# Package: `config`

## Purpose

Loads, validates, and serializes the application's YAML configurations. It maps input, playback, and output hardware slots and handles pinning changes.

## Exported API

```go
package config

type Config struct {
	MIDI     MIDIConfig              `yaml:"midi"`
	Defaults DefaultsConfig          `yaml:"defaults"`
	Devices  map[string]DeviceConfig `yaml:"devices"`
	Pages    map[string]PageConfig   `yaml:"pages"`
	// pagesMu protects Pages; Devices is read-only after Load
}

type MIDIConfig struct {
	Device string `yaml:"device"` // e.g. "/dev/midi1"; "" triggers auto-detect
}

type DefaultsConfig struct {
	InputKnob    KnobConfig `yaml:"input-knob"`
	PlaybackKnob KnobConfig `yaml:"playback-knob"`
	OutputKnob   KnobConfig `yaml:"output-knob"`
}

type KnobConfig struct {
	Type KnobType `yaml:"type"`            // "gain", "send", or "none"
	BusA string   `yaml:"bus-a,omitempty"` // device key for send bus A
	BusB string   `yaml:"bus-b,omitempty"` // device key for send bus B
}

type KnobType string

const (
	KnobGain KnobType = "gain"
	KnobSend KnobType = "send"
	KnobNone KnobType = "none"
)

func (k KnobConfig) IsSend() bool

type DeviceConfig struct {
	Label      string          `yaml:"label"`
	Type       BindType        `yaml:"type"`                  // "input", "playback", or "output"
	Match      string          `yaml:"match,omitempty"`       // case-insensitive substring on Name/BindKey
	MatchRegex string          `yaml:"match-regex,omitempty"` // regex applied to stream Name/BindKey
	MatchTitle string          `yaml:"match-title,omitempty"` // case-insensitive substring on window title
	Knob       *KnobConfig     `yaml:"knob,omitempty"`        // per-device override; nil = use default for type
	SyncMode   SyncMode        `yaml:"sync_mode,omitempty"`
}

type BindType string

const (
	BindInput    BindType = "input"
	BindPlayback BindType = "playback"
	BindOutput   BindType = "output"
)

func (d DeviceConfig) IsOutput() bool

func (d DeviceConfig) AudioKind() (audio.NodeKind, bool)

type PageConfig struct {
	Button   string          `yaml:"button"`
	Faders   map[int]*string `yaml:"faders,omitempty"`
	Knobs    map[int]*string `yaml:"knobs,omitempty"`
	Channels map[int]*string `yaml:"channels,omitempty"`
}

func DefaultPath() string

func Load(path string) (*Config, error)

func Save(path string, cfg *Config) error

func (c *Config) Validate() error

func (c *Config) PinFader(ch int, key string)

func (c *Config) UnpinFader(ch int, key string)

func (c *Config) DeviceFor(key string) *DeviceConfig

func (c *Config) ChannelFor(ch int) *DeviceConfig

func (c *Config) ChannelForPage(page string, ch int) *DeviceConfig

func (c *Config) MatchStringForPage(page string, ch int) string

func (c *Config) MatchStringFor(ch int) string

func (c *Config) KnobDeviceFor(ch int) *DeviceConfig

func (c *Config) KnobFor(ch int) (KnobConfig, bool)

func (c *Config) KnobForPage(page string, ch int) (KnobConfig, bool)

func (c *Config) KnobForDevice(deviceKey string) (KnobConfig, bool)

func (c *Config) ResolveOutput(key string) string

func (c *Config) DeviceKeyForPage(page string, ch int) string
```

## Inbound Dependencies

- `ui`
- `cmd/smc-mixerd`
- `cmd/smc-mixer`

## Outbound Dependencies

- `audio`

## Seams

- **`Load` / `Save` / `Validate`**: Serves as the persistence layer boundary.
- **Lookups (`ChannelForPage`, `KnobForPage`)**: Translates layout coordinate selections (pages and channel indexes) into mapping properties.

## Side Effects

- `Load` reads files from the local filesystem.
- `Save` writes configuration files and generates directories via `os.MkdirAll`.

## Package-level Invariants & Concurrency Assumptions

- Contains a `pagesMu sync.RWMutex` to protect updates to the `Pages` map (mutated during `PinFader` / `UnpinFader` when streams are pinned). `Devices` mapping is expected to remain read-only after loading.
