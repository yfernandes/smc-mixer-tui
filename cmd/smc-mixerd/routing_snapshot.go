package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/daemon"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/pipewire"
)

// categoryOrder fixes the display order of route categories to match the
// page order (applications/outputs/inputs button pages); anything else (there
// shouldn't be anything else) sorts last.
var categoryOrder = map[string]int{"applications": 0, "outputs": 1, "inputs": 2}

func categoryFor(kind audio.NodeKind) string {
	switch kind {
	case audio.KindSource:
		return "applications"
	case audio.KindSink:
		return "outputs"
	case audio.KindMic:
		return "inputs"
	}
	return "other"
}

// buildRoutingSnapshot assembles the routing inspector payload: one RouteNode
// per stream the daemon manages, whether or not it's attached to a hardware
// channel. Crossfader routes get an A/B branch pair through the null-sink and
// gain-stage chain; plain channel bindings get a single "Direct" branch.
// Live values are queried fresh from PipeWire so mismatches with what the
// daemon last commanded are visible.
func buildRoutingSnapshot(ctx context.Context, pw *pipewire.Client, disp *dispatcher.Dispatcher, mgr *crossfaderManager, cfg *config.Config) daemon.RoutingSnapshot {
	live, _ := pw.ListStreams(ctx) // best-effort; nil live just means every step reports unknown
	byName := make(map[string]pipewire.Stream, len(live))
	for _, s := range live {
		byName[s.NodeName] = s
	}

	snap := disp.Snapshot()
	// volByNodeID maps a PipeWire node ID to the ActualVolume of whichever
	// hardware channel is directly bound to it — used to surface the internal
	// value for output sinks controlled by their own channel (e.g. ch2/ch3),
	// independent of any crossfader routing through them.
	volByNodeID := make(map[uint32]float64, 8)
	for _, c := range snap {
		if c.StreamID != nil {
			volByNodeID[*c.StreamID] = c.ActualVolume
		}
	}

	// liveStep builds a volume-bearing step (Gain A/B, Output). hasInternal is
	// true only when the daemon itself commands this node's volume; nodes an
	// independent hardware channel controls directly (e.g. an output sink
	// bound to its own channel) get their internal value from volByNodeID.
	liveStep := func(label, nodeName string, hasInternal bool, internalVol float64) daemon.RouteStep {
		step := daemon.RouteStep{Label: label, NodeName: nodeName, HasVolume: true, HasInternal: hasInternal, InternalVolume: internalVol}
		node, ok := byName[nodeName]
		if !ok {
			return step
		}
		if !hasInternal {
			if vol, ok := volByNodeID[node.ID]; ok {
				step.HasInternal = true
				step.InternalVolume = vol
			}
		}
		vol, muted, err := pw.GetVolume(ctx, node.ID)
		if err != nil {
			return step
		}
		step.LiveKnown = true
		step.LiveVolume = vol
		step.LiveMuted = muted
		return step
	}

	rawStream := func(id uint32) (name, nodeName string, kind audio.NodeKind) {
		for _, s := range live {
			if s.ID == id {
				return s.Name, s.NodeName, s.Kind
			}
		}
		return fmt.Sprintf("stream-%d", id), "", audio.KindSource
	}

	// faderStep resolves the "Volume fader" trunk step: the volume the daemon
	// applies to the raw stream before it reaches the null sink / output. Only
	// a channel-attached stream has a daemon-tracked internal value; unattached
	// streams still get a live reading.
	faderStep := func(streamID uint32, attachedCh int, snap [8]dispatcher.Channel) daemon.RouteStep {
		step := daemon.RouteStep{Label: "Volume Fader", HasVolume: true}
		if attachedCh >= 0 {
			step.HasInternal = true
			step.InternalVolume = snap[attachedCh].ActualVolume
		}
		if vol, muted, err := pw.GetVolume(ctx, streamID); err == nil {
			step.LiveKnown = true
			step.LiveVolume = vol
			step.LiveMuted = muted
		}
		return step
	}

	routes := mgr.Snapshot()
	crossStreamIDs := make(map[uint32]bool, len(routes))
	nodes := make([]daemon.RouteNode, 0, len(routes))
	for _, r := range routes {
		crossStreamIDs[r.streamID] = true
		displayName, nodeName, kind := rawStream(r.streamID)
		nodes = append(nodes, daemon.RouteNode{
			StreamName: displayName,
			Category:   categoryFor(kind),
			AttachedCh: r.attachedCh,
			DeviceKey:  r.deviceKey,
			// Null Sink has no volume of its own: nothing ever sets it, and its
			// device volume always reads 100% — showing int=/live= for it is
			// permanent noise, so it's excluded from HasVolume entirely.
			Trunk: []daemon.RouteStep{
				{Label: "Stream", NodeName: nodeName},
				faderStep(r.streamID, r.attachedCh, snap),
				{Label: "Null Sink", NodeName: r.nullSinkName},
			},
			Branches: []daemon.RouteBranch{
				{
					Label: "A",
					Steps: []daemon.RouteStep{
						liveStep("Gain A", r.gainAName, true, r.volA),
						liveStep("Output: "+r.nameA, r.sinkANode, false, 0),
					},
				},
				{
					Label: "B",
					Steps: []daemon.RouteStep{
						liveStep("Gain B", r.gainBName, true, r.volB),
						liveStep("Output: "+r.nameB, r.sinkBNode, false, 0),
					},
				},
			},
		})
	}

	// directNodeIDs tracks every raw PipeWire node already given its own root,
	// whether as a crossfade input (crossStreamIDs) or a plain channel binding
	// below — used to dedupe against the output-sink pass that follows, since
	// the same node can appear as both "channel N's stream" (on whichever page
	// currently has it bound) and "a crossfade route's output sink".
	directNodeIDs := make(map[uint32]bool, 8)
	for ch, c := range snap {
		if c.StreamID == nil || crossStreamIDs[*c.StreamID] {
			continue
		}
		directNodeIDs[*c.StreamID] = true
		_, nodeName, kind := rawStream(*c.StreamID)
		nodes = append(nodes, daemon.RouteNode{
			StreamName: c.Name,
			Category:   categoryFor(kind),
			AttachedCh: ch,
			Trunk: []daemon.RouteStep{
				{Label: "Stream", NodeName: nodeName},
			},
			Branches: []daemon.RouteBranch{
				{Label: "Direct", Steps: []daemon.RouteStep{faderStep(*c.StreamID, ch, snap)}},
			},
		})
	}

	// Crossfader output sinks are only visible as "channel N's stream" while
	// the hardware channel bound to them happens to be on the currently active
	// page — the daemon reuses physical strips across pages, so a sink's own
	// channel binding can be invisible right now. Surface every crossfade
	// output sink as its own root regardless, since its volume matters even
	// when no channel is currently pointed at it.
	for _, r := range routes {
		for _, sinkNode := range []string{r.sinkANode, r.sinkBNode} {
			node, ok := byName[sinkNode]
			if !ok || directNodeIDs[node.ID] {
				continue
			}
			directNodeIDs[node.ID] = true
			attachedCh := -1
			for ch, c := range snap {
				if c.StreamID != nil && *c.StreamID == node.ID {
					attachedCh = ch
					break
				}
			}
			nodes = append(nodes, daemon.RouteNode{
				StreamName: node.Name,
				Category:   categoryFor(node.Kind),
				AttachedCh: attachedCh,
				Trunk: []daemon.RouteStep{
					{Label: "Stream", NodeName: node.NodeName},
				},
				Branches: []daemon.RouteBranch{
					{Label: "Direct", Steps: []daemon.RouteStep{faderStep(node.ID, attachedCh, snap)}},
				},
			})
		}
	}

	// The dispatcher reuses its 8 physical channels across pages (e.g. mics
	// live on "inputs", outputs on "outputs"), so disp.Snapshot() above only
	// reflects whichever page is currently active — a mic bound on "inputs"
	// vanishes from the tree the moment the user switches to "applications".
	// planBindings is a pure function (no dispatcher mutation); run it for
	// every configured page against an empty snapshot to discover what each
	// page would bind, and surface any stream not already shown as its own
	// root, so every managed stream stays visible regardless of active page.
	if cfg != nil {
		enriched := mgr.LastStreams()
		for _, page := range cfg.PageNames() {
			for _, action := range planBindings(cfg, page, [8]dispatcher.Channel{}, enriched) {
				if action.lose || action.id == 0 || directNodeIDs[action.id] {
					continue
				}
				directNodeIDs[action.id] = true
				name, nodeName, kind := rawStream(action.id)
				nodes = append(nodes, daemon.RouteNode{
					StreamName: name,
					Category:   categoryFor(kind),
					AttachedCh: -1, // not the currently active page; not really "attached" right now
					Trunk: []daemon.RouteStep{
						{Label: "Stream", NodeName: nodeName},
					},
					Branches: []daemon.RouteBranch{
						{Label: "Direct", Steps: []daemon.RouteStep{faderStep(action.id, -1, snap)}},
					},
				})
			}
		}
	}

	// Stable order: grouped by category (page order), alphabetical by display
	// name within a category, tie-broken by node name — so the tree doesn't
	// reshuffle from run to run as maps/goroutines race, only actual additions
	// or removals change its shape.
	sort.SliceStable(nodes, func(i, j int) bool {
		ci, cj := categoryOrder[nodes[i].Category], categoryOrder[nodes[j].Category]
		if ci != cj {
			return ci < cj
		}
		ni, nj := strings.ToLower(nodes[i].StreamName), strings.ToLower(nodes[j].StreamName)
		if ni != nj {
			return ni < nj
		}
		return nodeNameOf(nodes[i]) < nodeNameOf(nodes[j])
	})

	return daemon.RoutingSnapshot{Routes: nodes}
}

// nodeNameOf returns the raw PipeWire node name from a RouteNode's first
// trunk step, used only as a deterministic sort tie-breaker.
func nodeNameOf(n daemon.RouteNode) string {
	if len(n.Trunk) == 0 {
		return ""
	}
	return n.Trunk[0].NodeName
}
