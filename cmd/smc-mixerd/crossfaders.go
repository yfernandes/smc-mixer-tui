package main

import (
	"context"
	"log"
	"strings"
	"sync"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/pipewire"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

type crossfaderManager struct {
	mu          sync.Mutex
	cfg         *config.Config
	pw          *pipewire.Client
	disp        *dispatcher.Dispatcher
	active      map[string]*crossfaderState
	sinksLogged map[string]bool // true after "sinks not found" has been logged; cleared when sinks are found
	closed      bool            // set by Close; blocks Sync from re-creating routing after teardown

	lastSSMu sync.RWMutex
	lastSS   []streams.EnrichedStream // most recent stream list; used by SyncIfAble
}

type crossfaderState struct {
	routing    *pipewire.CrossfaderRouting
	ctrl       *channelCrossfader
	streamID   uint32
	nameA      string
	nameB      string
	attachedCh int              // dispatcher channel currently controlled by this routing; -1 if none
	knob       config.KnobConfig // effective knob config; used in Pass 1 to re-validate sinks without re-deriving from cfg
}

type channelCrossfader struct {
	pw      *pipewire.Client
	routing *pipewire.CrossfaderRouting
}

func newCrossfaderManager(cfg *config.Config, pw *pipewire.Client, disp *dispatcher.Dispatcher) *crossfaderManager {
	return &crossfaderManager{
		cfg:         cfg,
		pw:          pw,
		disp:        disp,
		active:      make(map[string]*crossfaderState),
		sinksLogged: make(map[string]bool),
	}
}

func (m *crossfaderManager) Sync(ctx context.Context, snap [8]dispatcher.Channel, ss []streams.EnrichedStream) {
	m.lastSSMu.Lock()
	m.lastSS = ss
	m.lastSSMu.Unlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}

	// Pass 1: maintain active routings — tear down any that lost their stream or sinks.
	for deviceKey, state := range m.active {
		if streamNodeNameFor(state.streamID, ss) == "" {
			log.Printf("crossfader %s: stream %d not in ss (len=%d), tearing down", deviceKey, state.streamID, len(ss))
			m.teardownDevice(ctx, deviceKey)
			continue
		}
		sinkA, sinkB, _, _ := resolveCrossfaderSinks(m.cfg, state.knob, ss)
		if sinkA == "" || sinkB == "" {
			log.Printf("crossfader %s: sinks gone (A=%q B=%q), tearing down", deviceKey, state.knob.BusA, state.knob.BusB)
			m.teardownDevice(ctx, deviceKey)
			continue
		}
		// Routing is valid; update which channel strip controls the knob.
		m.attachCurrentChannel(deviceKey, state, snap)
	}

	// Pass 2: set up routing for channel-bound streams that don't have one yet.
	// Driven by snap so that both auto-bound (config-matched) and manually-bound
	// streams receive a send matrix when defaults.playback-knob (or per-device
	// knob) is type: send.
	activeStreamIDs := make(map[uint32]bool, len(m.active))
	for _, state := range m.active {
		activeStreamIDs[state.streamID] = true
	}
	for _, channel := range snap {
		if channel.StreamID == nil {
			continue
		}
		streamID := *channel.StreamID
		if activeStreamIDs[streamID] {
			continue
		}
		s := streamByID(streamID, ss)
		if s == nil {
			continue
		}
		deviceKey, knob := m.deviceKeyAndKnob(*s)
		if !knob.IsSend() {
			continue
		}
		if _, exists := m.active[deviceKey]; exists {
			continue
		}
		sinkA, sinkB, _, _ := resolveCrossfaderSinks(m.cfg, knob, ss)
		if sinkA == "" || sinkB == "" {
			if !m.sinksLogged[deviceKey] {
				log.Printf("crossfader %s: sinks not found (A=%q B=%q), will retry when available", deviceKey, knob.BusA, knob.BusB)
				m.sinksLogged[deviceKey] = true
			}
			continue
		}
		m.sinksLogged[deviceKey] = false
		m.setupDevice(ctx, deviceKey, streamID, knob, snap, ss)
	}
}

// SyncIfAble runs a full Sync using the most recently cached stream list.
// Safe to call concurrently; returns immediately if no cached list is available yet.
func (m *crossfaderManager) SyncIfAble(ctx context.Context, snap [8]dispatcher.Channel) {
	m.lastSSMu.RLock()
	ss := m.lastSS
	m.lastSSMu.RUnlock()
	if ss == nil {
		return
	}
	m.Sync(ctx, snap, ss)
}

// Reattach updates the dispatcher knob attachment for all active routings based on snap.
// It only calls SetCrossfader — no PipeWire teardown or setup — so it is safe to call
// synchronously on every bind/unbind without incurring PipeWire latency.
func (m *crossfaderManager) Reattach(snap [8]dispatcher.Channel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for deviceKey, state := range m.active {
		m.attachCurrentChannel(deviceKey, state, snap)
	}
}

func (m *crossfaderManager) Close(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Mark closed before teardown so any concurrent Sync call that acquires m.mu
	// afterwards does not re-create routing we are about to destroy.
	m.closed = true
	for deviceKey := range m.active {
		m.teardownDevice(ctx, deviceKey)
	}
}

// attachCurrentChannel finds which channel in snap currently holds state.streamID and
// updates the dispatcher attachment, migrating from the previously attached channel if needed.
func (m *crossfaderManager) attachCurrentChannel(_ string, state *crossfaderState, snap [8]dispatcher.Channel) {
	newCh := -1
	for ch, c := range snap {
		if c.StreamID != nil && *c.StreamID == state.streamID {
			newCh = ch
			break
		}
	}
	if newCh == state.attachedCh {
		return
	}
	if state.attachedCh >= 0 {
		m.disp.SetCrossfader(state.attachedCh, nil, "", "")
	}
	if newCh >= 0 {
		m.disp.SetCrossfader(newCh, state.ctrl, state.nameA, state.nameB)
	}
	state.attachedCh = newCh
}

func (m *crossfaderManager) teardownDevice(ctx context.Context, deviceKey string) {
	state := m.active[deviceKey]
	log.Printf("crossfader %s: teardown (attachedCh=%d stream=%d)", deviceKey, state.attachedCh, state.streamID)
	if state.attachedCh >= 0 {
		m.disp.SetCrossfader(state.attachedCh, nil, "", "")
	}
	m.pw.TeardownCrossfader(ctx, state.routing)
	delete(m.active, deviceKey)
}

func (m *crossfaderManager) setupDevice(ctx context.Context, deviceKey string, streamID uint32, knob config.KnobConfig, snap [8]dispatcher.Channel, ss []streams.EnrichedStream) {
	sinkANodeName, sinkBNodeName, nameA, nameB := resolveCrossfaderSinks(m.cfg, knob, ss)
	if sinkANodeName == "" || sinkBNodeName == "" {
		log.Printf("crossfader %s: sinks not found (A=%q B=%q)", deviceKey, knob.BusA, knob.BusB)
		return
	}
	streamNodeName := streamNodeNameFor(streamID, ss)
	routing, err := m.pw.SetupCrossfader(ctx, deviceKey, streamID, streamNodeName, sinkANodeName, sinkBNodeName)
	if err != nil {
		log.Printf("crossfader %s setup: %v", deviceKey, err)
		return
	}
	ctrl := &channelCrossfader{pw: m.pw, routing: routing}
	state := &crossfaderState{
		routing:    routing,
		ctrl:       ctrl,
		streamID:   streamID,
		nameA:      nameA,
		nameB:      nameB,
		attachedCh: -1,
		knob:       knob,
	}
	m.active[deviceKey] = state
	m.attachCurrentChannel(deviceKey, state, snap)
	log.Printf("crossfader %s: %s ↔ %s", deviceKey, nameA, nameB)
}

func (c *channelCrossfader) SetGains(ctx context.Context, volA, volB float64) error {
	return c.pw.SetCrossfaderGains(ctx, c.routing, volA, volB)
}

// deviceKeyAndKnob returns the stable routing key and effective KnobConfig for a stream.
// For streams matching a configured device, the device key and its effective knob are used.
// For unconfigured streams, the stream's NodeName is used as the key and the default knob
// for the stream's audio kind is returned (e.g. defaults.playback-knob for KindSource).
func (m *crossfaderManager) deviceKeyAndKnob(s streams.EnrichedStream) (string, config.KnobConfig) {
	for key := range m.cfg.Devices {
		dev := m.cfg.DeviceFor(key)
		if dev != nil && newStreamMatcher(dev).matches(s) {
			if knob, ok := m.cfg.KnobForDevice(key); ok {
				return key, knob
			}
		}
	}
	key := s.NodeName
	if key == "" {
		key = s.Name
	}
	return key, defaultKnobForStream(m.cfg, s)
}

// defaultKnobForStream returns the configured default KnobConfig for a stream's audio kind.
func defaultKnobForStream(cfg *config.Config, s streams.EnrichedStream) config.KnobConfig {
	switch s.Kind {
	case audio.KindMic:
		return cfg.Defaults.InputKnob
	case audio.KindSource:
		return cfg.Defaults.PlaybackKnob
	case audio.KindSink:
		return cfg.Defaults.OutputKnob
	}
	return config.KnobConfig{}
}

func streamByID(id uint32, ss []streams.EnrichedStream) *streams.EnrichedStream {
	for i := range ss {
		if ss[i].ID == id {
			return &ss[i]
		}
	}
	return nil
}

func resolveCrossfaderSinks(cfg *config.Config, knob config.KnobConfig, ss []streams.EnrichedStream) (nodeA, nodeB, nameA, nameB string) {
	descA := strings.ToLower(cfg.ResolveOutput(knob.BusA))
	descB := strings.ToLower(cfg.ResolveOutput(knob.BusB))
	for _, s := range ss {
		if s.Kind != audio.KindSink {
			continue
		}
		lower := strings.ToLower(s.Name)
		if nodeA == "" && descA != "" && strings.Contains(lower, descA) {
			nodeA, nameA = s.NodeName, s.Name
		}
		if nodeB == "" && descB != "" && strings.Contains(lower, descB) {
			nodeB, nameB = s.NodeName, s.Name
		}
	}
	return
}

func streamNodeNameFor(id uint32, ss []streams.EnrichedStream) string {
	for _, s := range ss {
		if s.ID == id {
			return s.NodeName
		}
	}
	return ""
}
