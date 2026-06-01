package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/BurntSushi/toml"
)

// Config is the full on-disk representation of smc-mixer settings.
//
// Example file:
//
//	[midi]
//	device = "/dev/midi1"   # omit for auto-detect
//
//	[channels]
//	0 = "Firefox"
//	2 = "Spotify"
//	5 = "discord"
type Config struct {
	MIDI     MIDIConfig        `toml:"midi"`
	Channels map[string]string `toml:"channels"` // key "0"–"7" → stream name substring
}

// MIDIConfig holds hardware settings.
type MIDIConfig struct {
	Device string `toml:"device"` // e.g. "/dev/midi1"; "" triggers auto-detect
}

// DefaultPath returns the canonical config file location:
// $XDG_CONFIG_HOME/smc-mixer/config.toml, falling back to ~/.config/…
func DefaultPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "config.toml"
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "smc-mixer", "config.toml")
}

// Load reads the TOML file at path.
// If the file does not exist, an empty Config is returned (not an error) so
// the daemon works out of the box without a config file.
func Load(path string) (*Config, error) {
	cfg := &Config{}
	_, err := toml.DecodeFile(path, cfg)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save writes cfg to path as TOML, creating parent directories as needed.
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// StreamFor returns the stream-name substring bound to channel ch (0–7),
// or "" if no binding is configured.
func (c *Config) StreamFor(ch int) string {
	if c.Channels == nil {
		return ""
	}
	return c.Channels[strconv.Itoa(ch)]
}

// SetStream binds channel ch to a stream-name substring, initialising the map
// if needed.
func (c *Config) SetStream(ch int, name string) {
	if c.Channels == nil {
		c.Channels = make(map[string]string)
	}
	if name == "" {
		delete(c.Channels, strconv.Itoa(ch))
	} else {
		c.Channels[strconv.Itoa(ch)] = name
	}
}
