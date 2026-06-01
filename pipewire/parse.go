package pipewire

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// pwNode is the minimal shape we extract from pw-dump's JSON array.
type pwNode struct {
	ID   uint32 `json:"id"`
	Type string `json:"type"`
	Info struct {
		Props map[string]json.RawMessage `json:"props"`
	} `json:"info"`
}

func parseStreams(data []byte) ([]Stream, error) {
	var nodes []pwNode
	if err := json.Unmarshal(data, &nodes); err != nil {
		return nil, fmt.Errorf("parse pw-dump: %w", err)
	}

	var streams []Stream
	for _, n := range nodes {
		if n.Type != "PipeWire:Interface:Node" {
			continue
		}
		class := rawStr(n.Info.Props["media.class"])
		var kind NodeKind
		switch class {
		case "Stream/Output/Audio":
			kind = KindSource
		case "Stream/Input/Audio", "Audio/Source", "Audio/Source/Virtual":
			kind = KindMic
		case "Audio/Sink", "Audio/Sink/Virtual":
			kind = KindSink
		default:
			continue
		}

		// Hardware devices expose a human-readable node.description; prefer it.
		name := rawStr(n.Info.Props["node.description"])
		if name == "" {
			name = rawStr(n.Info.Props["application.name"])
		}
		if name == "" {
			name = rawStr(n.Info.Props["node.name"])
		}
		if name == "" {
			name = fmt.Sprintf("stream-%d", n.ID)
		}
		streams = append(streams, Stream{
			ID:        n.ID,
			Name:      name,
			MediaName: rawStr(n.Info.Props["media.name"]),
			PID:       rawUint32(n.Info.Props["application.process.id"]),
			Kind:      kind,
		})
	}
	return streams, nil
}

// parseVolumeLine handles "Volume: 0.50" and "Volume: 0.50 [MUTED]".
func parseVolumeLine(line string) (float64, bool, error) {
	const prefix = "Volume: "
	if !strings.HasPrefix(line, prefix) {
		return 0, false, fmt.Errorf("unexpected wpctl output: %q", line)
	}
	rest := strings.TrimPrefix(line, prefix)
	muted := strings.Contains(rest, "[MUTED]")
	volStr := strings.Fields(rest)[0]
	vol, err := strconv.ParseFloat(volStr, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse volume %q: %w", volStr, err)
	}
	return vol, muted, nil
}

// rawUint32 unmarshals a JSON number or quoted number; returns 0 on error.
func rawUint32(raw json.RawMessage) uint32 {
	if len(raw) == 0 {
		return 0
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if v, err := strconv.ParseUint(s, 10, 32); err == nil {
			return uint32(v)
		}
	}
	var n uint64
	if json.Unmarshal(raw, &n) == nil {
		return uint32(n)
	}
	return 0
}

// rawStr unmarshals a JSON string value; returns "" for non-strings or errors.
func rawStr(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func idStr(id uint32) string {
	return strconv.FormatUint(uint64(id), 10)
}
