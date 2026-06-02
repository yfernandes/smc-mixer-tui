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

	// Build client-ID → (PID, app name) maps. Some stream nodes omit
	// application.process.id and application.name entirely, storing the info
	// only on their owning Client node (referenced via client.id).
	clientPIDs := make(map[uint32]uint32)
	clientNames := make(map[uint32]string)
	for _, n := range nodes {
		if n.Type != "PipeWire:Interface:Client" {
			continue
		}
		pid := rawUint32(n.Info.Props["application.process.id"])
		if pid == 0 {
			pid = rawUint32(n.Info.Props["pipewire.sec.pid"])
		}
		if pid > 0 {
			clientPIDs[n.ID] = pid
		}
		if name := rawStr(n.Info.Props["application.name"]); name != "" {
			clientNames[n.ID] = name
		}
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

		clientID := rawUint32(n.Info.Props["client.id"])

		// Hardware devices expose a human-readable node.description; prefer it.
		name := rawStr(n.Info.Props["node.description"])
		if name == "" {
			name = rawStr(n.Info.Props["application.name"])
		}
		if name == "" && clientID > 0 {
			name = clientNames[clientID]
		}
		if name == "" {
			name = rawStr(n.Info.Props["node.name"])
		}
		if name == "" {
			name = fmt.Sprintf("stream-%d", n.ID)
		}

		// Resolve PID: prefer the node-level property, fall back to the client node.
		pid := rawUint32(n.Info.Props["application.process.id"])
		if pid == 0 && clientID > 0 {
			pid = clientPIDs[clientID]
		}

		streams = append(streams, Stream{
			ID:        n.ID,
			Name:      name,
			NodeName:  rawStr(n.Info.Props["node.name"]),
			MediaName: rawStr(n.Info.Props["media.name"]),
			PID:       pid,
			Kind:      kind,
		})
	}
	return streams, nil
}

// parseSinkInputs parses the output of "pactl list sink-inputs".
// Each block starting with "Sink Input #N" becomes one SinkInput entry.
func parseSinkInputs(data []byte) []SinkInput {
	var out []SinkInput
	var cur SinkInput
	inProps := false
	initialized := false

	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "Sink Input #") {
			if initialized {
				out = append(out, cur)
			}
			cur = SinkInput{}
			initialized = true
			inProps = false
			n, err := strconv.ParseUint(strings.TrimPrefix(trimmed, "Sink Input #"), 10, 32)
			if err == nil {
				cur.Index = uint32(n)
			}
			continue
		}
		if !initialized {
			continue
		}

		if trimmed == "Properties:" {
			inProps = true
			continue
		}
		if inProps {
			// Properties are indented two levels; a top-level key resets context.
			if !strings.HasPrefix(line, "\t\t") && !strings.HasPrefix(line, "    ") {
				inProps = false
			} else {
				// node.id = "129"  (may be absent for PipeWire-native streams)
				if strings.HasPrefix(trimmed, "node.id = ") {
					val := strings.Trim(strings.TrimPrefix(trimmed, "node.id = "), `"`)
					if n, err := strconv.ParseUint(val, 10, 32); err == nil {
						cur.NodeID = uint32(n)
					}
				}
				// node.name = "firefox.instance_1_46"
				if strings.HasPrefix(trimmed, "node.name = ") {
					cur.NodeName = strings.Trim(strings.TrimPrefix(trimmed, "node.name = "), `"`)
				}
				continue
			}
		}

		if strings.HasPrefix(trimmed, "Owner Module:") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "Owner Module:"))
			if n, err := strconv.ParseUint(val, 10, 32); err == nil {
				cur.OwnerModule = uint32(n)
			}
		}
	}
	if initialized {
		out = append(out, cur)
	}
	return out
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
