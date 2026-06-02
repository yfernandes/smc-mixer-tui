package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

// SocketPath returns the path to the daemon Unix domain socket.
// Prefers XDG_RUNTIME_DIR (e.g. /run/user/1000) over ~/.local/share.
func SocketPath() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "smc-mixer.sock")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "smc-mixer", "smc-mixer.sock")
}

type msgKind string

const (
	kindInitial  msgKind = "initial"  // daemon → client: full state on connect
	kindSnapshot msgKind = "snapshot" // daemon → client: channel state update
	kindStreams   msgKind = "streams"  // daemon → client: enriched stream list update
	kindDevice   msgKind = "device"   // daemon → client: MIDI device status
	kindGlobal   msgKind = "global"   // daemon → client: transport button press
	kindBind     msgKind = "bind"     // client → daemon: bind stream to channel
	kindUnbind   msgKind = "unbind"   // client → daemon: unbind channel
)

// envelope is the newline-delimited wire format.
type envelope struct {
	Kind msgKind         `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// ── Push payloads (daemon → client) ──────────────────────────────────────────

// channelWire is the JSON-safe form of dispatcher.Channel.
// The unexported crossfader field is omitted; all exported fields are preserved.
type channelWire struct {
	StreamID       *uint32             `json:"stream_id,omitempty"`
	Name           string              `json:"name"`
	Kind           audio.NodeKind `json:"kind"`
	MPRISName      string              `json:"mpris_name"`
	ActualVolume   float64             `json:"actual_volume"`
	FaderPos       float64             `json:"fader_pos"`
	LastSetVol     float64             `json:"last_set_vol"`
	Synced         bool                `json:"synced"`
	Knob           int                 `json:"knob"`
	Mute           bool                `json:"mute"`
	SoloMuted      bool                `json:"solo_muted"`
	Solo           bool                `json:"solo"`
	Rec            bool                `json:"rec"`
	Stop           bool                `json:"stop"`
	CrossSinkAName string              `json:"cross_sink_a_name"`
	CrossSinkBName string              `json:"cross_sink_b_name"`
}

func toWire(c dispatcher.Channel) channelWire {
	return channelWire{
		StreamID:       c.StreamID,
		Name:           c.Name,
		Kind:           c.Kind,
		MPRISName:      c.MPRISName,
		ActualVolume:   c.ActualVolume,
		FaderPos:       c.FaderPos,
		LastSetVol:     c.LastSetVol,
		Synced:         c.Synced,
		Knob:           c.Knob,
		Mute:           c.Mute,
		SoloMuted:      c.SoloMuted,
		Solo:           c.Solo,
		Rec:            c.Rec,
		Stop:           c.Stop,
		CrossSinkAName: c.CrossSinkAName,
		CrossSinkBName: c.CrossSinkBName,
	}
}

func fromWire(w channelWire) dispatcher.Channel {
	return dispatcher.Channel{
		StreamID:       w.StreamID,
		Name:           w.Name,
		Kind:           w.Kind,
		MPRISName:      w.MPRISName,
		ActualVolume:   w.ActualVolume,
		FaderPos:       w.FaderPos,
		LastSetVol:     w.LastSetVol,
		Synced:         w.Synced,
		Knob:           w.Knob,
		Mute:           w.Mute,
		SoloMuted:      w.SoloMuted,
		Solo:           w.Solo,
		Rec:            w.Rec,
		Stop:           w.Stop,
		CrossSinkAName: w.CrossSinkAName,
		CrossSinkBName: w.CrossSinkBName,
	}
}

type snapshotWire [8]channelWire

func snapToWire(s [8]dispatcher.Channel) snapshotWire {
	var w snapshotWire
	for i, c := range s {
		w[i] = toWire(c)
	}
	return w
}

func snapFromWire(w snapshotWire) [8]dispatcher.Channel {
	var s [8]dispatcher.Channel
	for i, c := range w {
		s[i] = fromWire(c)
	}
	return s
}

// initialPayload is sent once on connect so the TUI can render immediately.
type initialPayload struct {
	Snapshot snapshotWire             `json:"snapshot"`
	Streams  []streams.EnrichedStream `json:"streams"`
}

// ── Command payloads (client → daemon) ───────────────────────────────────────

type bindPayload struct {
	Ch        int                 `json:"ch"`
	ID        uint32              `json:"id"`
	Name      string              `json:"name"`
	Kind      audio.NodeKind `json:"kind"`
	MPRISName string              `json:"mpris_name"`
}

type unbindPayload struct {
	Ch int `json:"ch"`
}
