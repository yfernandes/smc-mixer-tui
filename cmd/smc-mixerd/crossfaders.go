package main

import (
	"context"
	"fmt"
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
	mu     sync.Mutex
	cfg    *config.Config
	pw     *pipewire.Client
	disp   *dispatcher.Dispatcher
	active [8]*crossfaderState
}

type crossfaderState struct {
	routing  *pipewire.CrossfaderRouting
	ctrl     *channelCrossfader
	streamID uint32
	nameA    string
	nameB    string
}

type channelCrossfader struct {
	pw      *pipewire.Client
	routing *pipewire.CrossfaderRouting
}

func newCrossfaderManager(cfg *config.Config, pw *pipewire.Client, disp *dispatcher.Dispatcher) *crossfaderManager {
	return &crossfaderManager{cfg: cfg, pw: pw, disp: disp}
}

func (m *crossfaderManager) Sync(ctx context.Context, snap [8]dispatcher.Channel, ss []streams.EnrichedStream) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ch := range 8 {
		m.syncChannel(ctx, ch, snap[ch], ss)
	}
}

func (m *crossfaderManager) Close(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ch := range 8 {
		if m.active[ch] != nil {
			m.teardownChannel(ctx, ch)
		}
	}
}

func (m *crossfaderManager) syncChannel(ctx context.Context, ch int, channel dispatcher.Channel, ss []streams.EnrichedStream) {
	// If routing is active, guard it against the stream disappearing or config changes —
	// NOT against the dispatcher channel changing. Page navigation rebinds dispatcher
	// channels (and can set StreamID to nil) but must never tear down active routing.
	if m.active[ch] != nil {
		activeID := m.active[ch].streamID
		knob, stillSend := sendKnobForStream(m.cfg, activeID, ss)
		if stillSend {
			sinkA, sinkB, _, _ := resolveCrossfaderSinks(m.cfg, knob, ss)
			if sinkA != "" && sinkB != "" {
				// Routing valid. Attach hardware knob control only when the dispatcher
				// channel currently points at this stream (e.g. applications page active).
				// Detach when it points elsewhere (e.g. main page, different stream).
				m.syncDispatcherAttachment(ch, channel)
				return
			}
		}
		// Stream gone, config removed, or sinks offline → full teardown.
		m.teardownChannel(ctx, ch)
	}

	// Set up a new crossfader if the dispatcher has a stream that needs one.
	streamID := channel.StreamID
	if streamID == nil {
		return
	}
	knob, isSend := sendKnobForStream(m.cfg, *streamID, ss)
	if !isSend {
		return
	}
	sinkA, sinkB, _, _ := resolveCrossfaderSinks(m.cfg, knob, ss)
	if sinkA == "" || sinkB == "" {
		log.Printf("crossfader ch%d: sinks not found (A=%q B=%q)", ch, knob.BusA, knob.BusB)
		return
	}
	m.setupChannel(ctx, ch, *streamID, knob, ss)
}

// syncDispatcherAttachment attaches or detaches the hardware knob control for ch based
// on whether the dispatcher's current channel binding matches the active crossfader stream.
// The PipeWire routing is never touched here.
func (m *crossfaderManager) syncDispatcherAttachment(ch int, channel dispatcher.Channel) {
	state := m.active[ch]
	if channel.StreamID != nil && *channel.StreamID == state.streamID {
		m.disp.SetCrossfader(ch, state.ctrl, state.nameA, state.nameB)
	} else {
		m.disp.SetCrossfader(ch, nil, "", "")
	}
}

// sendKnobForStream returns the KnobConfig for a stream by matching it against the device
// config, independent of the active page. This keeps crossfader routing alive across page
// switches: the send matrix depends on what stream is playing, not which page is visible.
func sendKnobForStream(cfg *config.Config, streamID uint32, ss []streams.EnrichedStream) (config.KnobConfig, bool) {
	var stream *streams.EnrichedStream
	for i := range ss {
		if ss[i].ID == streamID {
			stream = &ss[i]
			break
		}
	}
	if stream == nil {
		return config.KnobConfig{}, false
	}
	for key := range cfg.Devices {
		knob, ok := cfg.KnobForDevice(key)
		if !ok || !knob.IsSend() {
			continue
		}
		dev := cfg.DeviceFor(key)
		if dev != nil && newStreamMatcher(dev).matches(*stream) {
			return knob, true
		}
	}
	return config.KnobConfig{}, false
}

func (m *crossfaderManager) teardownChannel(ctx context.Context, ch int) {
	m.pw.TeardownCrossfader(ctx, m.active[ch].routing)
	m.disp.SetCrossfader(ch, nil, "", "")
	m.active[ch] = nil
}

func (m *crossfaderManager) setupChannel(ctx context.Context, ch int, streamID uint32, knob config.KnobConfig, ss []streams.EnrichedStream) {
	sinkANodeName, sinkBNodeName, nameA, nameB := resolveCrossfaderSinks(m.cfg, knob, ss)
	if sinkANodeName == "" || sinkBNodeName == "" {
		log.Printf("crossfader ch%d: sinks not found (A=%q B=%q)", ch, knob.BusA, knob.BusB)
		return
	}

	streamNodeName := streamNodeNameFor(streamID, ss)
	tag := fmt.Sprintf("ch%d", ch)
	routing, err := m.pw.SetupCrossfader(ctx, tag, streamID, streamNodeName, sinkANodeName, sinkBNodeName)
	if err != nil {
		log.Printf("crossfader ch%d setup: %v", ch, err)
		return
	}

	ctrl := &channelCrossfader{pw: m.pw, routing: routing}
	m.active[ch] = &crossfaderState{routing: routing, ctrl: ctrl, streamID: streamID, nameA: nameA, nameB: nameB}
	m.disp.SetCrossfader(ch, ctrl, nameA, nameB)
	log.Printf("crossfader ch%d: %s ↔ %s", ch, nameA, nameB)
}

func (c *channelCrossfader) SetGains(ctx context.Context, volA, volB float64) error {
	return c.pw.SetCrossfaderGains(ctx, c.routing, volA, volB)
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
