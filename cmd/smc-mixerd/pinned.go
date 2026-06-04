package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/yfernandes/smc-mixer-tui/config"
	"gopkg.in/yaml.v3"
)

// pinnedState tracks which main-page fader slots are user-pinned.
// keys maps slot index → device key (same key used in config.Devices).
type pinnedState struct {
	mu   sync.Mutex
	keys map[int]string
	path string
}

func newPinnedState(path string) *pinnedState {
	return &pinnedState{keys: make(map[int]string), path: path}
}

func pinnedStatePath() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			base = filepath.Join(home, ".local", "share")
		}
	}
	return filepath.Join(base, "smc-mixer", "pinned.yaml")
}

// load reads pinned.yaml and merges its entries into cfg.pages.main.faders.
func (ps *pinnedState) load(cfg *config.Config) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	f, err := os.Open(ps.path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		log.Printf("load pinned: %v", err)
		return
	}
	defer f.Close()

	var raw map[int]string
	if err := yaml.NewDecoder(f).Decode(&raw); err != nil {
		log.Printf("load pinned: decode: %v", err)
		return
	}
	for ch, key := range raw {
		if ch < 0 || ch > 7 {
			continue
		}
		ps.keys[ch] = key
		cfg.PinFader(ch, key)
	}
}

// toggle pins or unpins slot ch for deviceKey. Returns true if now pinned.
// Also updates cfg to reflect the new state and persists to disk.
func (ps *pinnedState) toggle(cfg *config.Config, ch int, deviceKey string) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if existing, ok := ps.keys[ch]; ok && existing == deviceKey {
		delete(ps.keys, ch)
		cfg.UnpinFader(ch, deviceKey)
		ps.save()
		return false
	}

	ps.keys[ch] = deviceKey
	cfg.PinFader(ch, deviceKey)
	ps.save()
	return true
}

// isPinned reports whether slot ch is currently pinned, and which device key it holds.
func (ps *pinnedState) isPinned(ch int) (string, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	key, ok := ps.keys[ch]
	return key, ok
}

// snapshot returns a copy of the current pinned keys map.
func (ps *pinnedState) snapshot() map[int]string {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	out := make(map[int]string, len(ps.keys))
	for k, v := range ps.keys {
		out[k] = v
	}
	return out
}

// save writes the current pinned state to disk. Must be called with ps.mu held.
func (ps *pinnedState) save() {
	if err := os.MkdirAll(filepath.Dir(ps.path), 0o755); err != nil {
		log.Printf("save pinned: mkdir: %v", err)
		return
	}
	f, err := os.Create(ps.path)
	if err != nil {
		log.Printf("save pinned: create: %v", err)
		return
	}
	defer f.Close()
	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	if err := enc.Encode(ps.keys); err != nil {
		log.Printf("save pinned: encode: %v", err)
	}
}
