package pipewire

import (
	"context"
	"fmt"
	"strconv"
	"strings"
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
	StreamNodeID   uint32 // PipeWire stream node ID whose target.object was pinned
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
	if err := c.CleanupCrossfaderTag(ctx, tag); err != nil {
		return nil, fmt.Errorf("cleanup stale crossfader modules: %w", err)
	}

	plan := newCrossfaderSetup(tag, sinkANodeName, sinkBNodeName)
	if err := plan.loadModules(ctx, c); err != nil {
		plan.unloadLoaded(ctx, c)
		return nil, err
	}

	nullNodeID, err := c.findNodeIDByName(ctx, plan.names.null)
	if err != nil {
		plan.unloadLoaded(ctx, c)
		return nil, fmt.Errorf("find null sink node: %w", err)
	}
	if streamNodeID != 0 {
		if err := c.RouteStreamToSink(ctx, streamNodeID, nullNodeID); err != nil {
			plan.unloadLoaded(ctx, c)
			return nil, fmt.Errorf("pin stream route: %w", err)
		}
		plan.r.StreamNodeID = streamNodeID
	}

	streamSI, err := c.findSinkInput(ctx, streamNodeID, streamNodeName)
	if err != nil {
		if streamNodeID != 0 {
			_ = c.ClearStreamRoute(ctx, streamNodeID)
		}
		plan.unloadLoaded(ctx, c)
		return nil, fmt.Errorf("find stream SI: %w", err)
	}

	if err := c.MoveSinkInput(ctx, streamSI, plan.names.null); err != nil {
		if streamNodeID != 0 {
			_ = c.ClearStreamRoute(ctx, streamNodeID)
		}
		plan.unloadLoaded(ctx, c)
		return nil, fmt.Errorf("move stream to null sink: %w", err)
	}
	if streamNodeID != 0 {
		_ = c.ClearStreamRoute(ctx, streamNodeID)
	}

	return plan.routing(streamSI), nil
}

// PulseModule is one loaded PulseAudio-compatible module in PipeWire-Pulse.
type PulseModule struct {
	ID   uint32
	Name string
	Args string
}

// ListModules returns the currently loaded PulseAudio-compatible modules.
func (c *Client) ListModules(ctx context.Context) ([]PulseModule, error) {
	out, err := c.exec(ctx, "pactl", "list", "short", "modules")
	if err != nil {
		return nil, fmt.Errorf("pactl list short modules: %w\n%s", err, out)
	}
	return parsePulseModules(out), nil
}

func parsePulseModules(data []byte) []PulseModule {
	var modules []PulseModule
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) < 2 {
			continue
		}
		id, err := strconv.ParseUint(fields[0], 10, 32)
		if err != nil {
			continue
		}
		m := PulseModule{ID: uint32(id), Name: fields[1]}
		if len(fields) >= 3 {
			m.Args = fields[2]
		}
		modules = append(modules, m)
	}
	return modules
}

// CleanupCrossfaderTag unloads leftover modules for one generated crossfader tag.
// This makes setup idempotent across daemon crashes/restarts; PipeWire-Pulse can
// otherwise keep duplicate null sinks with the same sink_name, making loopback
// source/sink names ambiguous.
func (c *Client) CleanupCrossfaderTag(ctx context.Context, tag string) error {
	modules, err := c.ListModules(ctx)
	if err != nil {
		return err
	}
	names := newCrossfaderSetup(tag, "", "").names
	var stale []uint32
	for _, m := range modules {
		if crossfaderModuleArgsMatch(m.Args, names) {
			stale = append(stale, m.ID)
		}
	}
	for i := len(stale) - 1; i >= 0; i-- {
		if err := c.UnloadModule(ctx, stale[i]); err != nil {
			return err
		}
	}
	return nil
}

func crossfaderModuleArgsMatch(args string, names crossfaderNames) bool {
	return strings.Contains(args, "sink_name="+names.null) ||
		strings.Contains(args, "sink_name="+names.gainA) ||
		strings.Contains(args, "sink_name="+names.gainB) ||
		strings.Contains(args, "source="+names.null+".monitor") ||
		strings.Contains(args, "source="+names.gainA+".monitor") ||
		strings.Contains(args, "source="+names.gainB+".monitor") ||
		strings.Contains(args, "sink="+names.null) ||
		strings.Contains(args, "sink="+names.gainA) ||
		strings.Contains(args, "sink="+names.gainB)
}

type crossfaderNames struct {
	null  string
	gainA string
	gainB string
	sinkA string
	sinkB string
}

type crossfaderSetup struct {
	names  crossfaderNames
	loaded []uint32
	r      CrossfaderRouting
}

func newCrossfaderSetup(tag, sinkANodeName, sinkBNodeName string) *crossfaderSetup {
	nullName := "smc_" + tag + "_void"
	gainAName := "smc_" + tag + "_gain_a"
	gainBName := "smc_" + tag + "_gain_b"
	return &crossfaderSetup{
		names: crossfaderNames{
			null:  nullName,
			gainA: gainAName,
			gainB: gainBName,
			sinkA: sinkANodeName,
			sinkB: sinkBNodeName,
		},
		r: CrossfaderRouting{
			NullSinkName: nullName,
			GainAName:    gainAName,
			GainBName:    gainBName,
		},
	}
}

func (s *crossfaderSetup) loadModules(ctx context.Context, c *Client) error {
	steps := []struct {
		label string
		name  string
		args  string
		set   func(uint32)
	}{
		{
			label: "null sink",
			name:  "module-null-sink",
			args:  "sink_name=" + s.names.null + " sink_properties=device.description=" + s.names.null,
			set:   func(id uint32) { s.r.NullSinkModule = id },
		},
		{
			label: "gain sink A",
			name:  "module-null-sink",
			args:  "sink_name=" + s.names.gainA + " sink_properties=device.description=" + s.names.gainA,
			set:   func(id uint32) { s.r.GainAModule = id },
		},
		{
			label: "gain sink B",
			name:  "module-null-sink",
			args:  "sink_name=" + s.names.gainB + " sink_properties=device.description=" + s.names.gainB,
			set:   func(id uint32) { s.r.GainBModule = id },
		},
		{
			label: "loopback A",
			name:  "module-loopback",
			args:  loopbackArgs(s.names.null+".monitor", s.names.gainA),
			set:   func(id uint32) { s.r.LoopAModule = id },
		},
		{
			label: "loopback B",
			name:  "module-loopback",
			args:  loopbackArgs(s.names.null+".monitor", s.names.gainB),
			set:   func(id uint32) { s.r.LoopBModule = id },
		},
		{
			label: "loopback 2A",
			name:  "module-loopback",
			args:  loopbackArgs(s.names.gainA+".monitor", s.names.sinkA),
			set:   func(id uint32) { s.r.Loop2AModule = id },
		},
		{
			label: "loopback 2B",
			name:  "module-loopback",
			args:  loopbackArgs(s.names.gainB+".monitor", s.names.sinkB),
			set:   func(id uint32) { s.r.Loop2BModule = id },
		},
	}

	for _, step := range steps {
		id, err := c.LoadModule(ctx, step.name, step.args)
		if err != nil {
			return fmt.Errorf("%s: %w", step.label, err)
		}
		s.loaded = append(s.loaded, id)
		step.set(id)
	}
	return nil
}

func (s *crossfaderSetup) unloadLoaded(ctx context.Context, c *Client) {
	for i := len(s.loaded) - 1; i >= 0; i-- {
		_ = c.UnloadModule(ctx, s.loaded[i])
	}
}

func (s *crossfaderSetup) routing(streamSI uint32) *CrossfaderRouting {
	s.r.StreamSI = streamSI
	return &s.r
}

func loopbackArgs(source, sink string) string {
	return "source=" + source + " sink=" + sink +
		" source.dont.move=true sink.dont.move=true latency_msec=50"
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
	if r.StreamNodeID != 0 {
		_ = c.ClearStreamRoute(ctx, r.StreamNodeID)
	}
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
