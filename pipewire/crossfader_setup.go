package pipewire

import (
	"context"
	"fmt"
)

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
	for _, step := range s.moduleLoadSteps() {
		id, err := c.LoadModule(ctx, step.name, step.args)
		if err != nil {
			return fmt.Errorf("%s: %w", step.label, err)
		}
		s.loaded = append(s.loaded, id)
		step.set(id)
	}
	return nil
}

func (s *crossfaderSetup) moduleLoadSteps() []crossfaderModuleStep {
	return []crossfaderModuleStep{
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
}

type crossfaderModuleStep struct {
	label string
	name  string
	args  string
	set   func(uint32)
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
