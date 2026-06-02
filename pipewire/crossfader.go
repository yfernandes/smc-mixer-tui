package pipewire

import (
	"context"
	"fmt"
	"time"
)

// CrossfaderRouting holds the pactl/PipeWire object IDs for one channel's crossfader.
//
// Signal chain:
//
//	Stream → [wpctl vol = fader] → NullSink → monitor
//	                                         → [loopA] → GainSinkA [pactl sink vol = crossfader_A] → monitor
//	                                         → [loopB] → GainSinkB [pactl sink vol = crossfader_B] → monitor
//	GainSinkA → [loop2A] → SinkA → [wpctl vol = outputA]
//	GainSinkB → [loop2B] → SinkB → [wpctl vol = outputB]
//
// Final_A = stream_signal * fader * crossfader_A * output_A
// Final_B = stream_signal * fader * crossfader_B * output_B
//
// Crossfader gain is applied at the GainSink volume level, leaving output sink
// device volumes (ch2/ch3 faders) fully independent.
type CrossfaderRouting struct {
	NullSinkModule uint32 // pactl module ID for the main null sink
	GainAModule    uint32 // pactl module ID for gain-stage null sink A
	GainBModule    uint32 // pactl module ID for gain-stage null sink B
	LoopAModule    uint32 // pactl module ID for loopback: NullSink.monitor → GainA
	LoopBModule    uint32 // pactl module ID for loopback: NullSink.monitor → GainB
	Loop2AModule   uint32 // pactl module ID for loopback: GainA.monitor → SinkA
	Loop2BModule   uint32 // pactl module ID for loopback: GainB.monitor → SinkB
	StreamSI       uint32 // pactl sink-input index for the stream (for teardown)
	NullSinkName   string // e.g. "smc_ch0_void"
	GainAName      string // e.g. "smc_ch0_gain_a"
	GainBName      string // e.g. "smc_ch0_gain_b"
}

// SetupCrossfader creates a null-sink, two gain-stage null sinks, and four loopbacks
// to independently route the stream to sinkANodeName and sinkBNodeName.
//
// streamNodeName is the PipeWire node.name of the stream (e.g. "firefox.instance_1_46").
// It is used as a fallback when the pactl sink-input lacks a node.id property,
// which is common for PipeWire-native streams such as Firefox or Chrome.
//
// tag must be unique per channel (e.g. "ch0") and stable across calls.
func (c *Client) SetupCrossfader(ctx context.Context, tag string, streamNodeID uint32, streamNodeName, sinkANodeName, sinkBNodeName string) (*CrossfaderRouting, error) {
	nullName := "smc_" + tag + "_void"
	gainAName := "smc_" + tag + "_gain_a"
	gainBName := "smc_" + tag + "_gain_b"

	// loaded collects module IDs in load order so unloadAll can reverse them.
	var loaded []uint32
	load := func(name, args string) (uint32, error) {
		id, err := c.LoadModule(ctx, name, args)
		if err != nil {
			return 0, err
		}
		loaded = append(loaded, id)
		return id, nil
	}
	unloadAll := func() {
		for i := len(loaded) - 1; i >= 0; i-- {
			_ = c.UnloadModule(ctx, loaded[i])
		}
	}

	nullModID, err := load("module-null-sink",
		"sink_name="+nullName+" sink_properties=device.description="+nullName)
	if err != nil {
		return nil, fmt.Errorf("null sink: %w", err)
	}

	gainAModID, err := load("module-null-sink",
		"sink_name="+gainAName+" sink_properties=device.description="+gainAName)
	if err != nil {
		unloadAll()
		return nil, fmt.Errorf("gain sink A: %w", err)
	}

	gainBModID, err := load("module-null-sink",
		"sink_name="+gainBName+" sink_properties=device.description="+gainBName)
	if err != nil {
		unloadAll()
		return nil, fmt.Errorf("gain sink B: %w", err)
	}

	// NullSink.monitor → GainA
	loopAModID, err := load("module-loopback",
		"source="+nullName+".monitor sink="+gainAName+
			" source.dont.move=true sink.dont.move=true latency_msec=50")
	if err != nil {
		unloadAll()
		return nil, fmt.Errorf("loopback A: %w", err)
	}

	// NullSink.monitor → GainB
	loopBModID, err := load("module-loopback",
		"source="+nullName+".monitor sink="+gainBName+
			" source.dont.move=true sink.dont.move=true latency_msec=50")
	if err != nil {
		unloadAll()
		return nil, fmt.Errorf("loopback B: %w", err)
	}

	// GainA.monitor → SinkA
	loop2AModID, err := load("module-loopback",
		"source="+gainAName+".monitor sink="+sinkANodeName+
			" source.dont.move=true sink.dont.move=true latency_msec=50")
	if err != nil {
		unloadAll()
		return nil, fmt.Errorf("loopback 2A: %w", err)
	}

	// GainB.monitor → SinkB
	loop2BModID, err := load("module-loopback",
		"source="+gainBName+".monitor sink="+sinkBNodeName+
			" source.dont.move=true sink.dont.move=true latency_msec=50")
	if err != nil {
		unloadAll()
		return nil, fmt.Errorf("loopback 2B: %w", err)
	}

	streamSI, err := c.findSinkInput(ctx, streamNodeID, streamNodeName)
	if err != nil {
		unloadAll()
		return nil, fmt.Errorf("find stream SI: %w", err)
	}

	if err := c.MoveSinkInput(ctx, streamSI, nullName); err != nil {
		unloadAll()
		return nil, fmt.Errorf("move stream to null sink: %w", err)
	}

	return &CrossfaderRouting{
		NullSinkModule: nullModID,
		GainAModule:    gainAModID,
		GainBModule:    gainBModID,
		LoopAModule:    loopAModID,
		LoopBModule:    loopBModID,
		Loop2AModule:   loop2AModID,
		Loop2BModule:   loop2BModID,
		StreamSI:       streamSI,
		NullSinkName:   nullName,
		GainAName:      gainAName,
		GainBName:      gainBName,
	}, nil
}

// SetCrossfaderGains adjusts the per-output crossfader gain by setting each
// gain-stage sink's device volume. volA and volB are 0.0–1.0.
// The output sink device volumes (ch2/ch3 faders) are not touched.
func (c *Client) SetCrossfaderGains(ctx context.Context, r *CrossfaderRouting, volA, volB float64) error {
	if err := c.SetSinkVolume(ctx, r.GainAName, volA); err != nil {
		return fmt.Errorf("crossfader gainA: %w", err)
	}
	if err := c.SetSinkVolume(ctx, r.GainBName, volB); err != nil {
		return fmt.Errorf("crossfader gainB: %w", err)
	}
	return nil
}

// TeardownCrossfader moves the stream back to the default sink and unloads all modules.
func (c *Client) TeardownCrossfader(ctx context.Context, r *CrossfaderRouting) {
	_ = c.MoveSinkInput(ctx, r.StreamSI, "@DEFAULT_SINK@")
	for _, moduleID := range r.moduleIDsInUnloadOrder() {
		_ = c.UnloadModule(ctx, moduleID)
	}
}

func (r *CrossfaderRouting) moduleIDsInUnloadOrder() []uint32 {
	return []uint32{
		r.Loop2BModule,
		r.Loop2AModule,
		r.LoopBModule,
		r.LoopAModule,
		r.GainBModule,
		r.GainAModule,
		r.NullSinkModule,
	}
}

// findSinkInput returns the pactl sink-input index for the given PW stream, matching
// first by node.id and falling back to node.name. The node.name fallback handles
// PipeWire-native streams (e.g. Firefox, Chrome) where pactl may omit node.id.
// Retries a few times to allow recently-started streams to appear.
func (c *Client) findSinkInput(ctx context.Context, nodeID uint32, nodeName string) (uint32, error) {
	for attempt := range 5 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(150 * time.Millisecond):
			}
		}
		sis, err := c.ListSinkInputs(ctx)
		if err != nil {
			continue
		}
		for _, si := range sis {
			if (nodeID != 0 && si.NodeID == nodeID) || (nodeName != "" && si.NodeName == nodeName) {
				return si.Index, nil
			}
		}
	}
	return 0, fmt.Errorf("node %d / %q not found in pactl sink-inputs", nodeID, nodeName)
}
