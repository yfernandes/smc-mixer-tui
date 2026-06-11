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
	StreamNodeID   uint32 // PipeWire stream node ID; used to mute/unmute around routing changes
}

// SetupCrossfader creates a null-sink, two gain-stage null sinks, and four loopbacks
// to independently route the stream to sinkANodeName and sinkBNodeName.
//
// streamNodeName is the PipeWire node.name of the stream (e.g. "firefox.instance_1_46").
// Used as a fallback identifier when the pactl sink-input omits node.id (common for
// PipeWire-native streams such as Firefox or Chromium).
//
// tag must be unique per channel (e.g. "ch0") and stable across calls.
func (c *Client) SetupCrossfader(ctx context.Context, tag string, streamNodeID uint32, streamNodeName, sinkANodeName, sinkBNodeName string) (*CrossfaderRouting, error) {
	// Silence the stream before any graph changes to prevent audible transients.
	if streamNodeID != 0 {
		_ = c.SetMute(ctx, streamNodeID, true)
		// WirePlumber applies the mute asynchronously; wait for it to take effect
		// before moving the stream to the new null sink.
		time.Sleep(40 * time.Millisecond)
	}

	routing, err := c.setupCrossfaderInner(ctx, tag, streamNodeID, streamNodeName, sinkANodeName, sinkBNodeName)
	if err != nil {
		if streamNodeID != 0 {
			_ = c.SetMute(ctx, streamNodeID, false)
		}
		return nil, err
	}

	// The signal path has two loopback hops in series (NullSink→GainSink→HW sink),
	// each with latency_msec=50. Both must fill their buffers before unmuting or
	// the partial-buffer audio causes a transient buzz on stream reconnect.
	time.Sleep(150 * time.Millisecond)
	if streamNodeID != 0 {
		_ = c.SetMute(ctx, streamNodeID, false)
	}
	return routing, nil
}

func (c *Client) setupCrossfaderInner(ctx context.Context, tag string, streamNodeID uint32, streamNodeName, sinkANodeName, sinkBNodeName string) (*CrossfaderRouting, error) {
	if err := c.CleanupCrossfaderTag(ctx, tag); err != nil {
		return nil, fmt.Errorf("cleanup stale crossfader modules: %w", err)
	}

	plan := newCrossfaderSetup(tag, sinkANodeName, sinkBNodeName)
	if err := plan.loadModules(ctx, c); err != nil {
		plan.unloadLoaded(ctx, c)
		return nil, err
	}

	// Record the stream node ID now so mute/unmute and teardown work even if
	// the pactl move below fails.
	if streamNodeID != 0 {
		plan.r.StreamNodeID = streamNodeID
	}

	// Find the sink-input BEFORE any routing calls. RouteStreamToSink (WirePlumber)
	// was previously called here, but it causes a race: WirePlumber can asynchronously
	// move the stream and invalidate the sink-input index between findSinkInput and
	// MoveSinkInput. The nodeName fallback in findSinkInput handles PipeWire-native
	// streams (e.g. Firefox, Chromium) without needing WirePlumber to move them first.
	streamSI, err := c.findSinkInput(ctx, streamNodeID, streamNodeName)
	if err != nil {
		plan.unloadLoaded(ctx, c)
		return nil, fmt.Errorf("find stream SI: %w", err)
	}

	if err := c.MoveSinkInput(ctx, streamSI, plan.names.null); err != nil {
		plan.unloadLoaded(ctx, c)
		return nil, fmt.Errorf("move stream to null sink: %w", err)
	}

	return plan.routing(streamSI), nil
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
	// Silence before touching the graph: prevents clicks and buzzing from
	// in-flight audio buffers hitting an in-destruction routing path.
	if r.StreamNodeID != 0 {
		_ = c.SetMute(ctx, r.StreamNodeID, true)
		time.Sleep(30 * time.Millisecond)
	}

	if r.StreamNodeID != 0 {
		_ = c.ClearStreamRoute(ctx, r.StreamNodeID)
	}
	_ = c.MoveSinkInput(ctx, r.StreamSI, "@DEFAULT_SINK@")
	for _, moduleID := range r.moduleIDsInUnloadOrder() {
		_ = c.UnloadModule(ctx, moduleID)
	}

	if r.StreamNodeID != 0 {
		_ = c.SetMute(ctx, r.StreamNodeID, false)
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
