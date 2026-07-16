package pwbackend

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
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
	ListStreams(context.Context) ([]pipewire.Stream, error)
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
	cross    *crossfaderManager
	cfg      *config.Config
}

func New(pw *pipewire.Client, e *streams.Enricher, cfg *config.Config) *Backend {
	b := newBackend(pw, e, streams.NewController(), cfg.Devices)
	b.cfg = cfg
	b.cross = newCrossfaderManager(cfg, pw)
	return b
}

func newBackend(pw pipewireClient, e enricher, mpris playPauser, devices map[string]config.DeviceConfig) *Backend {
	rules := make(map[string]*ruleState, len(devices))
	for key, device := range devices {
		rules[key] = &ruleState{device: device}
	}
	return &Backend{pw: pw, enricher: e, mpris: mpris, rules: rules, playing: make(map[uint32]bool), interval: 500 * time.Millisecond}
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
	if param == "crossfade" {
		key, ok := ruleKey(target)
		if !ok || b.cross == nil {
			return fmt.Errorf("pipewire: target %q has no crossfader", target)
		}
		return b.cross.Set(ctx, key, value.F)
	}
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
	if param == "crossfade" {
		key, ok := ruleKey(target)
		if !ok || b.cross == nil {
			return backend.Value{}, false, nil
		}
		value, known := b.cross.Get(key)
		return backend.Value{F: value}, known, nil
	}
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
	last := b.targetInfos()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ss, err := b.enricher.Enrich(ctx); err == nil {
				b.updateStreams(ctx, ss)
				next := b.targetInfos()
				if reflect.DeepEqual(last, next) {
					continue
				}
				last = next
				select {
				case ch <- next:
				default:
				}
			}
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
	b.streams = append(b.streams[:0], ss...)
	b.playing = playing
	for _, rule := range b.rules {
		rule.current = resolveRule(rule, ss)
		if rule.current != nil {
			rule.boundPID = rule.current.PID
			rule.boundMediaName = rule.current.MediaName
		}
	}
	b.mu.Unlock()
	if b.cross != nil {
		b.cross.Sync(ctx, b.ruleSnapshot(), ss)
	}
}

func (b *Backend) ruleSnapshot() map[string]streams.EnrichedStream {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make(map[string]streams.EnrichedStream, len(b.rules))
	for key, rule := range b.rules {
		if rule.current != nil {
			out[key] = *rule.current
		}
	}
	return out
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
		if b.cfg != nil {
			knob, ok := b.cfg.KnobForDevice(key)
			if !ok || !knob.IsSend() {
				out = append(out, info)
				continue
			}
			info.Params = append(info.Params, backend.ParamSpec{ID: "crossfade", Kind: backend.ParamComposite, Readable: true, Push: true})
			if b.cross != nil {
				if ext, ok := b.cross.Ext(key); ok {
					info.Ext = mergeExt(info.Ext, ext)
				}
			}
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func ruleKey(target backend.TargetID) (string, bool) {
	const prefix = Name + ":rule/"
	value := string(target)
	return strings.TrimPrefix(value, prefix), strings.HasPrefix(value, prefix)
}

func mergeExt(raw json.RawMessage, values map[string]string) json.RawMessage {
	merged := make(map[string]any)
	_ = json.Unmarshal(raw, &merged)
	for key, value := range values {
		merged[key] = value
	}
	out, _ := json.Marshal(merged)
	return out
}

func (b *Backend) Close(ctx context.Context) {
	if b.cross != nil {
		b.cross.Close(ctx)
	}
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
