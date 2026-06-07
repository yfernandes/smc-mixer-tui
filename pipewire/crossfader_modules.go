package pipewire

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

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
	stale := staleCrossfaderModules(modules, names)
	for i := len(stale) - 1; i >= 0; i-- {
		if err := c.UnloadModule(ctx, stale[i]); err != nil {
			return err
		}
	}
	return nil
}

func staleCrossfaderModules(modules []PulseModule, names crossfaderNames) []uint32 {
	var stale []uint32
	for _, m := range modules {
		if crossfaderModuleArgsMatch(m.Args, names) {
			stale = append(stale, m.ID)
		}
	}
	return stale
}

func crossfaderModuleArgsMatch(args string, names crossfaderNames) bool {
	for _, token := range []string{
		"sink_name=" + names.null,
		"sink_name=" + names.gainA,
		"sink_name=" + names.gainB,
		"source=" + names.null + ".monitor",
		"source=" + names.gainA + ".monitor",
		"source=" + names.gainB + ".monitor",
		"sink=" + names.null,
		"sink=" + names.gainA,
		"sink=" + names.gainB,
	} {
		if strings.Contains(args, token) {
			return true
		}
	}
	return false
}
