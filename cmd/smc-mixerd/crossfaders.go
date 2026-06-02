package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/pipewire"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

type crossfaderManager struct {
	cfg    *config.Config
	pw     *pipewire.Client
	disp   *dispatcher.Dispatcher
	active [8]*crossfaderState
}

type crossfaderState struct {
	routing  *pipewire.CrossfaderRouting
	streamID uint32
}

type channelCrossfader struct {
	pw      *pipewire.Client
	routing *pipewire.CrossfaderRouting
}

func newCrossfaderManager(cfg *config.Config, pw *pipewire.Client, disp *dispatcher.Dispatcher) *crossfaderManager {
	return &crossfaderManager{cfg: cfg, pw: pw, disp: disp}
}

func (m *crossfaderManager) Sync(ctx context.Context, snap [8]dispatcher.Channel, ss []streams.EnrichedStream) {
	for ch := range 8 {
		m.syncChannel(ctx, ch, snap[ch], ss)
	}
}

func (m *crossfaderManager) syncChannel(ctx context.Context, ch int, channel dispatcher.Channel, ss []streams.EnrichedStream) {
	knob, ok := m.cfg.KnobFor(ch)
	isCrossfade := ok && knob.Type == "crossfade"
	streamID := channel.StreamID

	if m.active[ch] != nil && (!isCrossfade || streamID == nil || *streamID != m.active[ch].streamID) {
		m.teardownChannel(ctx, ch)
	}

	if !isCrossfade || streamID == nil || m.active[ch] != nil {
		return
	}

	m.setupChannel(ctx, ch, *streamID, knob, ss)
}

func (m *crossfaderManager) teardownChannel(ctx context.Context, ch int) {
	m.pw.TeardownCrossfader(ctx, m.active[ch].routing)
	m.disp.SetCrossfader(ch, nil, "", "")
	m.active[ch] = nil
}

func (m *crossfaderManager) setupChannel(ctx context.Context, ch int, streamID uint32, knob config.KnobConfig, ss []streams.EnrichedStream) {
	sinkANodeName, sinkBNodeName, nameA, nameB := resolveCrossfaderSinks(m.cfg, knob, ss)
	if sinkANodeName == "" || sinkBNodeName == "" {
		log.Printf("crossfader ch%d: sinks not found (A=%q B=%q)", ch, knob.OutputA, knob.OutputB)
		return
	}

	streamNodeName := streamNodeNameFor(streamID, ss)
	tag := fmt.Sprintf("ch%d", ch)
	routing, err := m.pw.SetupCrossfader(ctx, tag, streamID, streamNodeName, sinkANodeName, sinkBNodeName)
	if err != nil {
		log.Printf("crossfader ch%d setup: %v", ch, err)
		return
	}

	m.active[ch] = &crossfaderState{routing: routing, streamID: streamID}
	m.disp.SetCrossfader(ch, &channelCrossfader{pw: m.pw, routing: routing}, nameA, nameB)
	log.Printf("crossfader ch%d: %s ↔ %s", ch, nameA, nameB)
}

func (c *channelCrossfader) SetGains(ctx context.Context, volA, volB float64) error {
	return c.pw.SetCrossfaderGains(ctx, c.routing, volA, volB)
}

func resolveCrossfaderSinks(cfg *config.Config, knob config.KnobConfig, ss []streams.EnrichedStream) (nodeA, nodeB, nameA, nameB string) {
	descA := strings.ToLower(cfg.ResolveOutput(knob.OutputA))
	descB := strings.ToLower(cfg.ResolveOutput(knob.OutputB))
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
