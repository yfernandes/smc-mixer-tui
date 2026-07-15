package pwbackend

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yfernandes/smc-mixer-tui/backend"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/pipewire"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

const Name = "pipewire"

type pipewireClient interface {
	SetVolume(context.Context, uint32, float64) error
	SetMute(context.Context, uint32, bool) error
	GetVolume(context.Context, uint32) (float64, bool, error)
}

type enricher interface {
	Enrich(context.Context) ([]streams.EnrichedStream, error)
}

type playPauser interface {
	PlayPause(context.Context, string) error
}

type ruleState struct {
	device         config.DeviceConfig
	current        *streams.EnrichedStream
	boundPID       uint32
	boundMediaName string
}

// Backend exposes live PipeWire nodes and config match rules as router targets.
type Backend struct {
	mu       sync.RWMutex
	pw       pipewireClient
	enricher enricher
	mpris    playPauser
	rules    map[string]*ruleState
	streams  []streams.EnrichedStream
	playing  map[uint32]bool
	interval time.Duration
}

func New(pw *pipewire.Client, e *streams.Enricher, devices map[string]config.DeviceConfig) *Backend {
	return newBackend(pw, e, streams.NewController(), devices)
}

func newBackend(pw pipewireClient, e enricher, mpris playPauser, devices map[string]config.DeviceConfig) *Backend {
	rules := make(map[string]*ruleState, len(devices))
	for key, device := range devices {
		rules[key] = &ruleState{device: device}
	}
	return &Backend{pw: pw, enricher: e, mpris: mpris, rules: rules, playing: make(map[uint32]bool), interval: 2 * time.Second}
}

func (b *Backend) Name() string { return Name }

func (b *Backend) Targets(ctx context.Context) ([]backend.TargetInfo, error) {
	ss, err := b.enricher.Enrich(ctx)
	if err != nil {
		return nil, err
	}
	b.updateStreams(ctx, ss)
	return b.targetInfos(), nil
}

func (b *Backend) Set(ctx context.Context, target backend.TargetID, param string, value backend.Value) error {
	s, ok := b.resolve(target)
	if !ok {
		return fmt.Errorf("pipewire: target %q is unresolved", target)
	}
	switch param {
	case "volume":
		return b.pw.SetVolume(ctx, s.ID, value.F)
	case "mute":
		return b.pw.SetMute(ctx, s.ID, value.B)
	case "playpause":
		if s.MPRISPlayer == "" {
			return fmt.Errorf("pipewire: target %q has no MPRIS player", target)
		}
		return b.mpris.PlayPause(ctx, s.MPRISPlayer)
	default:
		return fmt.Errorf("pipewire: unknown param %q", param)
	}
}

func (b *Backend) Get(ctx context.Context, target backend.TargetID, param string) (backend.Value, bool, error) {
	s, ok := b.resolve(target)
	if !ok {
		return backend.Value{}, false, nil
	}
	switch param {
	case "volume":
		vol, _, err := b.pw.GetVolume(ctx, s.ID)
		return backend.Value{F: vol}, err == nil, err
	case "mute":
		_, muted, err := b.pw.GetVolume(ctx, s.ID)
		return backend.Value{B: muted}, err == nil, err
	default:
		return backend.Value{}, false, nil
	}
}

func (b *Backend) Watch(ctx context.Context, ch chan<- []backend.TargetInfo) {
	defer close(ch)
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()
	volumeTicker := time.NewTicker(50 * time.Millisecond)
	defer volumeTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ss, err := b.enricher.Enrich(ctx); err == nil {
				b.updateStreams(ctx, ss)
			}
		case <-volumeTicker.C:
		}
		select {
		case ch <- b.targetInfos():
		default:
		}
	}
}

func (b *Backend) updateStreams(ctx context.Context, ss []streams.EnrichedStream) {
	playing := make(map[uint32]bool, len(ss))
	for _, s := range ss {
		if s.MPRISPlayer != "" {
			playing[s.ID] = streams.IsPlaying(ctx, s.MPRISPlayer)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.streams = append(b.streams[:0], ss...)
	b.playing = playing
	for _, rule := range b.rules {
		rule.current = resolveRule(rule, ss)
		if rule.current != nil {
			rule.boundPID = rule.current.PID
			rule.boundMediaName = rule.current.MediaName
		}
	}
}

func (b *Backend) resolve(target backend.TargetID) (streams.EnrichedStream, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	value := strings.TrimPrefix(string(target), Name+":")
	if strings.HasPrefix(value, "node/") {
		id, err := strconv.ParseUint(strings.TrimPrefix(value, "node/"), 10, 32)
		if err != nil {
			return streams.EnrichedStream{}, false
		}
		for _, s := range b.streams {
			if s.ID == uint32(id) {
				return s, true
			}
		}
		return streams.EnrichedStream{}, false
	}
	if strings.HasPrefix(value, "rule/") {
		rule := b.rules[strings.TrimPrefix(value, "rule/")]
		if rule != nil && rule.current != nil {
			return *rule.current, true
		}
	}
	return streams.EnrichedStream{}, false
}

func (b *Backend) targetInfos() []backend.TargetInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]backend.TargetInfo, 0, len(b.streams)+len(b.rules))
	for _, s := range b.streams {
		out = append(out, targetInfo(backend.TargetID(fmt.Sprintf("pipewire:node/%d", s.ID)), s.Name, s, b.playing[s.ID]))
	}
	for key, rule := range b.rules {
		info := backend.TargetInfo{ID: backend.TargetID("pipewire:rule/" + key), Label: rule.device.Label}
		if rule.current != nil {
			info = targetInfo(info.ID, rule.device.Label, *rule.current, b.playing[rule.current.ID])
		}
		out = append(out, info)
	}
	return out
}

func targetInfo(id backend.TargetID, label string, s streams.EnrichedStream, playing bool) backend.TargetInfo {
	params := []backend.ParamSpec{
		{ID: "volume", Kind: backend.ParamContinuous, Readable: true, Push: true},
		{ID: "mute", Kind: backend.ParamToggle, Readable: true, Push: true},
	}
	if s.MPRISPlayer != "" {
		params = append(params, backend.ParamSpec{ID: "playpause", Kind: backend.ParamTrigger})
	}
	ext, _ := json.Marshal(map[string]any{
		"node_id": s.ID, "pid": s.PID, "kind": s.Kind, "mpris_name": s.MPRISPlayer,
		"media_name": s.MediaName, "window_title": s.WinTitle,
		"playback_status": playing,
	})
	return backend.TargetInfo{ID: id, Label: label, Group: fmt.Sprint(s.Kind), Params: params, Ext: ext}
}
