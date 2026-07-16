package pwbackend

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/pipewire"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

const crossfadeWriteInterval = 20 * time.Millisecond

type crossfaderManager struct {
	mu          sync.Mutex
	cfg         *config.Config
	pw          crossfaderClient
	active      map[string]*crossfaderState
	sinksLogged map[string]bool
	closed      bool
}

type crossfaderClient interface {
	SetupCrossfader(context.Context, string, uint32, string, string, string) (*pipewire.CrossfaderRouting, error)
	CrossfaderHealthy(context.Context, *pipewire.CrossfaderRouting) (bool, error)
	AttachCrossfaderStream(context.Context, *pipewire.CrossfaderRouting, uint32, string) error
	SetCrossfaderGains(context.Context, *pipewire.CrossfaderRouting, float64, float64) error
	TeardownCrossfader(context.Context, *pipewire.CrossfaderRouting)
	RetargetCrossfaderOutput(context.Context, uint32, string, string) (uint32, error)
	SetMute(context.Context, uint32, bool) error
}

type crossfaderState struct {
	routing              *pipewire.CrossfaderRouting
	streamID             uint32
	streamName           string
	nameA, nameB         string
	sinkANode, sinkBNode string
	knob                 config.KnobConfig
	value                float64
	pending              chan float64
	cancel               context.CancelFunc
	attached             map[uint32]struct{}
}

type routeInfo struct {
	DeviceKey                          string
	StreamID                           uint32
	StreamName                         string
	NameA, NameB                       string
	SinkANode, SinkBNode               string
	NullSinkName, GainAName, GainBName string
	Value                              float64
}

func newCrossfaderManager(cfg *config.Config, pw crossfaderClient) *crossfaderManager {
	return &crossfaderManager{cfg: cfg, pw: pw, active: make(map[string]*crossfaderState), sinksLogged: make(map[string]bool)}
}

// Sync reconciles crossfader graphs from stable configured rule targets. Once
// created, a graph stays keyed by the rule/device and its void sink rather than
// an ephemeral stream node ID. The WirePlumber rule can therefore attach a
// replacement stream to the same smc_<tag>_void graph without module churn.
func (m *crossfaderManager) Sync(ctx context.Context, resolved map[string]streams.EnrichedStream, ss []streams.EnrichedStream) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	for key, state := range m.active {
		healthy, err := m.pw.CrossfaderHealthy(ctx, state.routing)
		if err != nil {
			log.Printf("crossfader %s: health check: %v", key, err)
			continue
		}
		if !healthy {
			// PipeWire/WirePlumber restarts invalidate every module and node ID
			// while leaving the daemon's in-memory state intact. Drop that stale
			// state so the normal creation pass below rebuilds the graph.
			log.Printf("crossfader %s: graph disappeared, rebuilding", key)
			m.teardownLocked(ctx, key)
			continue
		}
		stream, ok := resolved[key]
		if !ok {
			// Keep the device-owned graph alive while a rule is temporarily
			// unresolved. This is the churn fix: a browser tab disappearing no
			// longer destroys seven modules only to recreate them two seconds later.
			continue
		}
		candidates := crossfaderStreams(m.cfg, key, stream, ss)
		live := make(map[uint32]struct{})
		attachedNew := false
		for _, candidate := range candidates {
			live[candidate.ID] = struct{}{}
			if _, attached := state.attached[candidate.ID]; attached {
				continue
			}
			if err := m.pw.AttachCrossfaderStream(ctx, state.routing, candidate.ID, candidate.NodeName); err != nil {
				log.Printf("crossfader %s: attach matching stream %d: %v", key, candidate.ID, err)
				continue
			}
			state.attached[candidate.ID] = struct{}{}
			attachedNew = true
			if candidate.Active || stream.ID == candidate.ID {
				state.streamID, state.streamName = candidate.ID, candidate.NodeName
			}
		}
		for id := range state.attached {
			if _, ok := live[id]; !ok {
				delete(state.attached, id)
			}
		}
		if attachedNew && len(candidates) > 1 {
			preferred := preferredCrossfaderStream(stream, candidates)
			if err := m.pw.AttachCrossfaderStream(ctx, state.routing, preferred.ID, preferred.NodeName); err != nil {
				log.Printf("crossfader %s: reassert preferred stream %d: %v", key, preferred.ID, err)
			} else {
				state.streamID, state.streamName = preferred.ID, preferred.NodeName
			}
		}
		a, b, _, _ := resolveCrossfaderSinks(m.cfg, state.knob, ss)
		if a == "" || b == "" {
			log.Printf("crossfader %s: sinks gone, tearing down", key)
			m.teardownLocked(ctx, key)
		}
	}
	for key, stream := range resolved {
		if _, ok := m.active[key]; ok {
			continue
		}
		knob, ok := m.cfg.KnobForDevice(key)
		if !ok || !knob.IsSend() {
			continue
		}
		a, b, nameA, nameB := resolveCrossfaderSinks(m.cfg, knob, ss)
		if a == "" || b == "" {
			if !m.sinksLogged[key] {
				log.Printf("crossfader %s: sinks not found (A=%q B=%q), will retry", key, knob.BusA, knob.BusB)
				m.sinksLogged[key] = true
			}
			continue
		}
		m.sinksLogged[key] = false
		routing, err := m.pw.SetupCrossfader(ctx, key, stream.ID, stream.NodeName, a, b)
		if err != nil {
			log.Printf("crossfader %s setup: %v", key, err)
			continue
		}
		if err := m.pw.SetCrossfaderGains(ctx, routing, 1, 1); err != nil {
			m.pw.TeardownCrossfader(ctx, routing)
			log.Printf("crossfader %s initial gains: %v", key, err)
			continue
		}
		workerCtx, cancel := context.WithCancel(ctx)
		state := &crossfaderState{routing: routing, streamID: stream.ID, streamName: stream.NodeName, nameA: nameA, nameB: nameB, sinkANode: a, sinkBNode: b, knob: knob, value: .5, pending: make(chan float64, 1), cancel: cancel, attached: map[uint32]struct{}{stream.ID: {}}}
		m.active[key] = state
		candidates := crossfaderStreams(m.cfg, key, stream, ss)
		for _, candidate := range candidates {
			if _, attached := state.attached[candidate.ID]; attached {
				continue
			}
			if err := m.pw.AttachCrossfaderStream(ctx, routing, candidate.ID, candidate.NodeName); err != nil {
				log.Printf("crossfader %s: attach matching stream %d: %v", key, candidate.ID, err)
				continue
			}
			state.attached[candidate.ID] = struct{}{}
			if candidate.Active {
				state.streamID, state.streamName = candidate.ID, candidate.NodeName
			}
		}
		if len(candidates) > 1 {
			preferred := preferredCrossfaderStream(stream, candidates)
			if err := m.pw.AttachCrossfaderStream(ctx, routing, preferred.ID, preferred.NodeName); err != nil {
				log.Printf("crossfader %s: reassert preferred stream %d: %v", key, preferred.ID, err)
			} else {
				state.streamID, state.streamName = preferred.ID, preferred.NodeName
			}
		}
		go m.runWorker(workerCtx, key, state)
		log.Printf("crossfader %s: %s ↔ %s", key, nameA, nameB)
	}
}

func matchingCrossfaderStreams(cfg *config.Config, key string, ss []streams.EnrichedStream) []streams.EnrichedStream {
	device, ok := cfg.Devices[key]
	if !ok {
		return nil
	}
	rule := &ruleState{device: device}
	out := make([]streams.EnrichedStream, 0, 1)
	for _, stream := range ss {
		if matches(rule, stream) {
			out = append(out, stream)
		}
	}
	return out
}

func crossfaderStreams(cfg *config.Config, key string, primary streams.EnrichedStream, ss []streams.EnrichedStream) []streams.EnrichedStream {
	out := matchingCrossfaderStreams(cfg, key, ss)
	for _, stream := range out {
		if stream.ID == primary.ID {
			return out
		}
	}
	return append(out, primary)
}

func preferredCrossfaderStream(primary streams.EnrichedStream, candidates []streams.EnrichedStream) streams.EnrichedStream {
	for _, stream := range candidates {
		if stream.Active {
			return stream
		}
	}
	return primary
}

func (m *crossfaderManager) runWorker(ctx context.Context, key string, state *crossfaderState) {
	ticker := time.NewTicker(crossfadeWriteInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			select {
			case value := <-state.pending:
				a, b := crossfadeGains(value)
				if err := m.pw.SetCrossfaderGains(ctx, state.routing, a, b); err != nil {
					log.Printf("crossfader %s gains: %v", key, err)
				}
			default:
			}
		}
	}
}

func (m *crossfaderManager) Set(_ context.Context, key string, value float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.active[key]
	if !ok {
		return fmt.Errorf("crossfader %s: no active routing", key)
	}
	value = math.Max(0, math.Min(1, value))
	state.value = value
	select {
	case <-state.pending:
	default:
	}
	state.pending <- value
	return nil
}

func (m *crossfaderManager) Get(key string) (float64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.active[key]
	if !ok {
		return 0, false
	}
	return state.value, true
}

func (m *crossfaderManager) Ext(key string) (map[string]string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.active[key]
	if !ok {
		return nil, false
	}
	return map[string]string{"cross_sink_a_name": state.nameA, "cross_sink_b_name": state.nameB}, true
}

func (m *crossfaderManager) Snapshot() []routeInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]routeInfo, 0, len(m.active))
	for key, state := range m.active {
		out = append(out, routeInfo{DeviceKey: key, StreamID: state.streamID, StreamName: state.streamName, NameA: state.nameA, NameB: state.nameB, SinkANode: state.sinkANode, SinkBNode: state.sinkBNode, NullSinkName: state.routing.NullSinkName, GainAName: state.routing.GainAName, GainBName: state.routing.GainBName, Value: state.value})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceKey < out[j].DeviceKey })
	return out
}

func (m *crossfaderManager) Retarget(ctx context.Context, key, branch, sinkNodeName, sinkDisplayName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.active[key]
	if !ok {
		return fmt.Errorf("crossfader %s: no active routing", key)
	}
	var monitor string
	var module *uint32
	var node, name *string
	switch branch {
	case "A":
		monitor, module, node, name = state.routing.GainAName+".monitor", &state.routing.Loop2AModule, &state.sinkANode, &state.nameA
	case "B":
		monitor, module, node, name = state.routing.GainBName+".monitor", &state.routing.Loop2BModule, &state.sinkBNode, &state.nameB
	default:
		return fmt.Errorf("crossfader %s: unknown branch %q", key, branch)
	}
	if state.streamID != 0 {
		_ = m.pw.SetMute(ctx, state.streamID, true)
		time.Sleep(40 * time.Millisecond)
	}
	newModule, err := m.pw.RetargetCrossfaderOutput(ctx, *module, monitor, sinkNodeName)
	if state.streamID != 0 {
		time.Sleep(150 * time.Millisecond)
		_ = m.pw.SetMute(ctx, state.streamID, false)
	}
	if err != nil {
		return err
	}
	*module, *node, *name = newModule, sinkNodeName, sinkDisplayName
	return nil
}

func (m *crossfaderManager) Close(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for key := range m.active {
		m.teardownLocked(ctx, key)
	}
}

func (m *crossfaderManager) teardownLocked(ctx context.Context, key string) {
	state := m.active[key]
	state.cancel()
	m.pw.TeardownCrossfader(ctx, state.routing)
	delete(m.active, key)
}

// crossfadeGains preserves the established center plateau while accepting the
// router's normalized 0..1 value instead of the dispatcher's 0..127 knob.
func crossfadeGains(value float64) (float64, float64) {
	return math.Min(1, 2*(1-value)), math.Min(1, 2*value)
}

func resolveCrossfaderSinks(cfg *config.Config, knob config.KnobConfig, ss []streams.EnrichedStream) (nodeA, nodeB, nameA, nameB string) {
	descA := strings.ToLower(cfg.ResolveOutput(knob.BusA))
	descB := strings.ToLower(cfg.ResolveOutput(knob.BusB))
	for _, stream := range ss {
		if stream.Kind != audio.KindSink {
			continue
		}
		name := strings.ToLower(stream.Name)
		if nodeA == "" && descA != "" && strings.Contains(name, descA) {
			nodeA, nameA = stream.NodeName, stream.Name
		}
		if nodeB == "" && descB != "" && strings.Contains(name, descB) {
			nodeB, nameB = stream.NodeName, stream.Name
		}
	}
	return
}
