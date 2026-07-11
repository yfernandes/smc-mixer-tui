package daemon

import (
	"encoding/json"
	"fmt"
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
	kindStrips   msgKind = "strips"   // daemon → client: generic router strip update
	kindStreams  msgKind = "streams"  // daemon → client: enriched stream list update
	kindDevice   msgKind = "device"   // daemon → client: MIDI device status
	kindGlobal   msgKind = "global"   // daemon → client: transport button press
	kindBind     msgKind = "bind"     // client → daemon: bind stream to channel
	kindUnbind   msgKind = "unbind"   // client → daemon: unbind channel
	kindMute     msgKind = "mute"     // client → daemon: toggle mute on channel
	kindSolo     msgKind = "solo"     // client → daemon: toggle solo on channel
	kindSet      msgKind = "set"      // client → daemon: set generic param
	kindToggle   msgKind = "toggle"   // client → daemon: toggle generic param

	kindRoutingRequest msgKind = "routing_request" // client → daemon: request a routing snapshot
	kindRouting        msgKind = "routing"         // daemon → client: routing snapshot response
	kindRetarget       msgKind = "retarget"        // client → daemon: repoint a crossfade branch's output sink
)

// envelope is the newline-delimited wire format.
type envelope struct {
	Kind msgKind         `json:"kind"`
	Data json.RawMessage `json:"data"`
}

func encodeFrame(kind msgKind, v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	env := envelope{Kind: kind, Data: json.RawMessage(data)}
	frame, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	return append(frame, '\n'), nil
}

func decodeEnvelope(frame []byte) (envelope, error) {
	var env envelope
	if err := json.Unmarshal(frame, &env); err != nil {
		return envelope{}, fmt.Errorf("decode envelope: %w", err)
	}
	return env, nil
}

// ── Push payloads (daemon → client) ──────────────────────────────────────────

// channelWire is the JSON-safe form of dispatcher.Channel.
// Unexported fields (crossfader, advancedSpec) are omitted.
type channelWire struct {
	StreamID       *uint32        `json:"stream_id,omitempty"`
	Name           string         `json:"name"`
	Kind           audio.NodeKind `json:"kind"`
	MPRISName      string         `json:"mpris_name"`
	ActualVolume   float64        `json:"actual_volume"`
	FaderPos       float64        `json:"fader_pos"`
	FaderPosKnown  bool           `json:"fader_pos_known"`
	LastSetVol     float64        `json:"last_set_vol"`
	Synced         bool           `json:"synced"`
	Knob           int            `json:"knob"`
	Mute           bool           `json:"mute"`
	SoloMuted      bool           `json:"solo_muted"`
	Solo           bool           `json:"solo"`
	Rec            bool           `json:"rec"`
	Stop           bool           `json:"stop"`
	Advanced       bool           `json:"advanced"`
	UserBound      bool           `json:"user_bound"`
	CrossSinkAName string         `json:"cross_sink_a_name"`
	CrossSinkBName string         `json:"cross_sink_b_name"`
}

func toWire(c dispatcher.Channel) channelWire {
	return channelWire{
		StreamID:       c.StreamID,
		Name:           c.Name,
		Kind:           c.Kind,
		MPRISName:      c.MPRISName,
		ActualVolume:   c.ActualVolume,
		FaderPos:       c.FaderPos,
		FaderPosKnown:  c.FaderPosKnown,
		LastSetVol:     c.LastSetVol,
		Synced:         c.Synced,
		Knob:           c.Knob,
		Mute:           c.Mute,
		SoloMuted:      c.SoloMuted,
		Solo:           c.Solo,
		Rec:            c.Rec,
		Stop:           c.Stop,
		Advanced:       c.Advanced,
		UserBound:      c.UserBound,
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
		FaderPosKnown:  w.FaderPosKnown,
		LastSetVol:     w.LastSetVol,
		Synced:         w.Synced,
		Knob:           w.Knob,
		Mute:           w.Mute,
		SoloMuted:      w.SoloMuted,
		Solo:           w.Solo,
		Rec:            w.Rec,
		Stop:           w.Stop,
		Advanced:       w.Advanced,
		UserBound:      w.UserBound,
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

type StripWire struct {
	Strip    int                  `json:"strip"`
	Label    string               `json:"label"`
	Backend  string               `json:"backend"`
	TargetID string               `json:"target_id"`
	Params   map[string]ParamWire `json:"params"`
	Ext      json.RawMessage      `json:"ext,omitempty"`
}

type ParamWire struct {
	Kind     uint8   `json:"kind"`
	Value    float64 `json:"value"`
	Bool     bool    `json:"bool"`
	Readable bool    `json:"readable"`
	Synced   bool    `json:"synced"`
}

// initialPayload is sent once on connect so the TUI can render immediately.
type initialPayload struct {
	Snapshot      snapshotWire             `json:"snapshot"`
	Strips        []StripWire              `json:"strips,omitempty"`
	Streams       []streams.EnrichedStream `json:"streams"`
	Labels        [8]string                `json:"labels"`
	ConfigPath    string                   `json:"config_path,omitempty"`
	DaemonVersion string                   `json:"daemon_version,omitempty"`
}

// ── Command payloads (client → daemon) ───────────────────────────────────────

type bindPayload struct {
	Ch        int            `json:"ch"`
	ID        uint32         `json:"id"`
	Name      string         `json:"name"`
	Kind      audio.NodeKind `json:"kind"`
	MPRISName string         `json:"mpris_name"`
	PID       uint32         `json:"pid,omitempty"`
	MediaName string         `json:"media_name,omitempty"`
}

type unbindPayload struct {
	Ch int `json:"ch"`
}

type setPayload struct {
	Target string  `json:"target"`
	Param  string  `json:"param"`
	Value  float64 `json:"value"`
	Bool   bool    `json:"bool"`
}

type togglePayload struct {
	Target string `json:"target"`
	Param  string `json:"param"`
}

// ── Routing inspector payloads ────────────────────────────────────────────────

// RouteStep is one transformation stage of a route (e.g. a null sink, a gain
// stage, an output sink). InternalVolume is what the daemon last commanded
// (only meaningful when HasInternal is true); LiveVolume/LiveMuted are freshly
// queried from PipeWire (only meaningful when LiveKnown is true — the node may
// not exist, e.g. torn-down or never created).
// HasVolume is false for steps where volume isn't a meaningful concept for
// that node (e.g. the raw stream identity, or a null sink whose device volume
// is never touched and always reads 100%) — the UI omits the int=/live=
// fields entirely for those rather than showing permanently-uninteresting
// values.
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

// RouteBranch is one path a stream's signal can take (e.g. "A"/"B" for a
// crossfader, "Direct" for a plain binding), as an ordered list of steps.
type RouteBranch struct {
	Label string      `json:"label"`
	Steps []RouteStep `json:"steps"`
}

// RouteNode is the root of one managed stream's routing tree. AttachedCh is
// the hardware channel currently controlling it, or -1 if the stream is
// managed but not bound to any of the 8 channels. Trunk holds the steps
// shared by every branch (the raw stream node, its fader, and — for
// crossfade routes — the null sink) before Branches fork. Category groups
// nodes in the UI ("applications", "outputs", "inputs" — mirroring the page
// order) so the tree doesn't reorder itself as streams come and go.
// DeviceKey identifies the crossfaderManager entry this node came from
// (empty for non-crossfade nodes) — the client echoes it back on a retarget
// command to address the right route.
type RouteNode struct {
	StreamName string        `json:"stream_name"`
	Category   string        `json:"category"`
	AttachedCh int           `json:"attached_ch"`
	DeviceKey  string        `json:"device_key,omitempty"`
	Trunk      []RouteStep   `json:"trunk"`
	Branches   []RouteBranch `json:"branches"`
}

// RoutingSnapshot is the full routing inspector payload.
type RoutingSnapshot struct {
	Routes []RouteNode `json:"routes"`
}

// retargetPayload repoints one crossfade branch's output sink. Branch is "A"
// or "B"; SinkNodeName/SinkDisplayName describe the new destination.
type retargetPayload struct {
	DeviceKey       string `json:"device_key"`
	Branch          string `json:"branch"`
	SinkNodeName    string `json:"sink_node_name"`
	SinkDisplayName string `json:"sink_display_name"`
}

type muteTogglePayload struct {
	Ch int `json:"ch"`
}

type soloTogglePayload struct {
	Ch int `json:"ch"`
}
