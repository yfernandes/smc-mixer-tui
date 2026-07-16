package pwbackend

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/pipewire"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

type RouteStep struct {
	Label          string  `json:"label"`
	NodeName       string  `json:"node_name"`
	HasVolume      bool    `json:"has_volume"`
	HasInternal    bool    `json:"has_internal"`
	InternalVolume float64 `json:"internal_volume"`
	LiveKnown      bool    `json:"live_known"`
	LiveVolume     float64 `json:"live_volume"`
	LiveMuted      bool    `json:"live_muted"`
}

type RouteBranch struct {
	Label string      `json:"label"`
	Steps []RouteStep `json:"steps"`
}

type RouteNode struct {
	StreamName string        `json:"stream_name"`
	Category   string        `json:"category"`
	AttachedCh int           `json:"attached_ch"`
	DeviceKey  string        `json:"device_key,omitempty"`
	Trunk      []RouteStep   `json:"trunk"`
	Branches   []RouteBranch `json:"branches"`
}

type RoutingSnapshot struct {
	Routes []RouteNode `json:"routes"`
}

type RetargetRequest struct {
	DeviceKey       string `json:"device_key"`
	Branch          string `json:"branch"`
	SinkNodeName    string `json:"sink_node_name"`
	SinkDisplayName string `json:"sink_display_name"`
}

func (b *Backend) View(ctx context.Context, view string, req json.RawMessage) (json.RawMessage, error) {
	switch view {
	case "routing":
		data, err := json.Marshal(b.routingSnapshot(ctx))
		return data, err
	case "retarget":
		if b.cross == nil {
			return nil, fmt.Errorf("pipewire: crossfader unavailable")
		}
		var command RetargetRequest
		if err := json.Unmarshal(req, &command); err != nil {
			return nil, fmt.Errorf("pipewire retarget request: %w", err)
		}
		if err := b.cross.Retarget(ctx, command.DeviceKey, command.Branch, command.SinkNodeName, command.SinkDisplayName); err != nil {
			return nil, err
		}
		return json.RawMessage(`{}`), nil
	default:
		return nil, fmt.Errorf("pipewire: unknown view %q", view)
	}
}

func (b *Backend) routingSnapshot(ctx context.Context) RoutingSnapshot {
	b.mu.RLock()
	ss := append([]streams.EnrichedStream(nil), b.streams...)
	b.mu.RUnlock()
	live, _ := b.pw.ListStreams(ctx)
	byID := make(map[uint32]streams.EnrichedStream, len(ss))
	for _, stream := range ss {
		byID[stream.ID] = stream
	}
	liveStep := func(label, nodeName string, id uint32, internal *float64) RouteStep {
		step := RouteStep{Label: label, NodeName: nodeName, HasVolume: true}
		if internal != nil {
			step.HasInternal, step.InternalVolume = true, *internal
		}
		if id != 0 {
			if value, muted, err := b.pw.GetVolume(ctx, id); err == nil {
				step.LiveKnown, step.LiveVolume, step.LiveMuted = true, value, muted
			}
		}
		return step
	}

	nodes := make([]RouteNode, 0, len(ss))
	crossIDs := make(map[uint32]bool)
	if b.cross != nil {
		for _, route := range b.cross.Snapshot() {
			crossIDs[route.StreamID] = true
			stream := byID[route.StreamID]
			a, z := crossfadeGains(route.Value)
			nodes = append(nodes, RouteNode{
				StreamName: stream.Name, Category: categoryFor(stream.Kind), AttachedCh: -1, DeviceKey: route.DeviceKey,
				Trunk: []RouteStep{{Label: "Stream", NodeName: route.StreamName}, liveStep("Volume Fader", route.StreamName, route.StreamID, nil), {Label: "Null Sink", NodeName: route.NullSinkName}},
				Branches: []RouteBranch{
					{Label: "A", Steps: []RouteStep{liveStep("Gain A", route.GainAName, nodeIDByName(live, route.GainAName), &a), liveStep("Output: "+route.NameA, route.SinkANode, nodeIDByName(live, route.SinkANode), nil)}},
					{Label: "B", Steps: []RouteStep{liveStep("Gain B", route.GainBName, nodeIDByName(live, route.GainBName), &z), liveStep("Output: "+route.NameB, route.SinkBNode, nodeIDByName(live, route.SinkBNode), nil)}},
				},
			})
		}
	}
	for _, stream := range ss {
		if crossIDs[stream.ID] || strings.HasPrefix(stream.NodeName, "smc_") || strings.HasPrefix(stream.NodeName, "loopback-") {
			continue
		}
		nodes = append(nodes, RouteNode{StreamName: stream.Name, Category: categoryFor(stream.Kind), AttachedCh: -1, Trunk: []RouteStep{{Label: "Stream", NodeName: stream.NodeName}}, Branches: []RouteBranch{{Label: "Direct", Steps: []RouteStep{liveStep("Volume Fader", stream.NodeName, stream.ID, nil)}}}})
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Category != nodes[j].Category {
			return categoryRank(nodes[i].Category) < categoryRank(nodes[j].Category)
		}
		return strings.ToLower(nodes[i].StreamName) < strings.ToLower(nodes[j].StreamName)
	})
	return RoutingSnapshot{Routes: nodes}
}

func nodeIDByName(ss []pipewire.Stream, name string) uint32 {
	for _, stream := range ss {
		if stream.NodeName == name {
			return stream.ID
		}
	}
	return 0
}

func categoryFor(kind audio.NodeKind) string {
	switch kind {
	case audio.KindSource:
		return "applications"
	case audio.KindSink:
		return "outputs"
	case audio.KindMic:
		return "inputs"
	default:
		return "other"
	}
}

func categoryRank(category string) int {
	switch category {
	case "applications":
		return 0
	case "outputs":
		return 1
	case "inputs":
		return 2
	default:
		return 3
	}
}
