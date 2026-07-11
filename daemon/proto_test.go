package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

func TestSocketPathPrefersXDGRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/123")

	got := SocketPath()
	want := filepath.Join("/run/user/123", "smc-mixer.sock")
	if got != want {
		t.Fatalf("SocketPath() = %q, want %q", got, want)
	}
}

func TestSocketPathFallsBackToUserDataDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("HOME", home)

	got := SocketPath()
	want := filepath.Join(home, ".local", "share", "smc-mixer", "smc-mixer.sock")
	if got != want {
		t.Fatalf("SocketPath() = %q, want %q", got, want)
	}
}

func TestEncodeFrameProducesNewlineDelimitedEnvelope(t *testing.T) {
	frame, err := encodeFrame(kindBind, bindPayload{
		Ch:        2,
		ID:        42,
		Name:      "Firefox",
		Kind:      audio.KindSource,
		MPRISName: "firefox.instance_1",
	})
	if err != nil {
		t.Fatalf("encodeFrame() error = %v", err)
	}
	if !bytes.HasSuffix(frame, []byte("\n")) {
		t.Fatalf("frame %q is not newline-delimited", frame)
	}

	env, err := decodeEnvelope(bytes.TrimSpace(frame))
	if err != nil {
		t.Fatalf("decodeEnvelope() error = %v", err)
	}
	if env.Kind != kindBind {
		t.Fatalf("env.Kind = %q, want %q", env.Kind, kindBind)
	}

	var payload bindPayload
	if err := json.Unmarshal(env.Data, &payload); err != nil {
		t.Fatalf("payload decode error = %v", err)
	}
	if payload.Ch != 2 || payload.ID != 42 || payload.Name != "Firefox" ||
		payload.Kind != audio.KindSource || payload.MPRISName != "firefox.instance_1" {
		t.Fatalf("decoded payload = %+v", payload)
	}
}

func TestSnapshotWireRoundTripPreservesClientVisibleChannelState(t *testing.T) {
	streamID := uint32(129)
	in := [8]dispatcher.Channel{}
	in[0] = dispatcher.Channel{
		StreamID:       &streamID,
		Name:           "Firefox",
		Kind:           audio.KindSource,
		MPRISName:      "firefox.instance_1",
		ActualVolume:   0.45,
		FaderPos:       0.44,
		FaderPosKnown:  true,
		LastSetVol:     0.43,
		Synced:         true,
		Knob:           65,
		Mute:           true,
		SoloMuted:      true,
		Solo:           true,
		Rec:            true,
		Stop:           true,
		Advanced:       true,
		UserBound:      true,
		CrossSinkAName: "Speakers",
		CrossSinkBName: "Headphones",
	}
	in[1] = dispatcher.Channel{
		Name:   "Built-in Audio",
		Kind:   audio.KindSink,
		Knob:   64,
		Solo:   true,
		Synced: true,
	}

	got := snapFromWire(snapToWire(in))
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("snapFromWire(snapToWire(in)) mismatch:\ngot  = %#v\nwant = %#v", got, in)
	}
}

func TestReadInitialStateDecodesInitialFrame(t *testing.T) {
	streamID := uint32(7)
	snapshot := [8]dispatcher.Channel{}
	snapshot[0] = dispatcher.Channel{
		StreamID:     &streamID,
		Name:         "Spotify",
		Kind:         audio.KindSource,
		ActualVolume: 0.8,
		Knob:         64,
	}
	labels := [8]string{"Music", "Chat"}
	payload := initialPayload{
		Snapshot: snapToWire(snapshot),
		Strips: []StripWire{{
			Strip:    3,
			Label:    "Brightness",
			Backend:  "exec",
			TargetID: "exec:brightness",
			Params: map[string]ParamWire{
				"value": {Kind: 0, Value: 0.5, Readable: true, Synced: false},
			},
		}},
		Streams: []streams.EnrichedStream{{
			ID:      7,
			PID:     99,
			Name:    "Spotify",
			BindKey: "spotify",
			Source:  streams.SourceMPRIS,
			Kind:    audio.KindSource,
		}},
		Labels:     labels,
		ConfigPath: "/home/yago/.config/smc-mixer/config.yaml",
	}
	frame, err := encodeFrame(kindInitial, payload)
	if err != nil {
		t.Fatalf("encodeFrame() error = %v", err)
	}

	got, err := readInitialState(bufio.NewScanner(bytes.NewReader(frame)))
	if err != nil {
		t.Fatalf("readInitialState() error = %v", err)
	}
	if !reflect.DeepEqual(got.Snapshot, snapshot) {
		t.Fatalf("Snapshot = %#v, want %#v", got.Snapshot, snapshot)
	}
	if !reflect.DeepEqual(got.Streams, payload.Streams) {
		t.Fatalf("Streams = %#v, want %#v", got.Streams, payload.Streams)
	}
	if !reflect.DeepEqual(got.Strips, payload.Strips) {
		t.Fatalf("Strips = %#v, want %#v", got.Strips, payload.Strips)
	}
	if got.Labels != labels {
		t.Fatalf("Labels = %#v, want %#v", got.Labels, labels)
	}
	if got.ConfigPath != payload.ConfigPath {
		t.Fatalf("ConfigPath = %q, want %q", got.ConfigPath, payload.ConfigPath)
	}
}

func TestGenericCommandFrameRoundTrip(t *testing.T) {
	setFrame, err := encodeFrame(kindSet, setPayload{Target: "exec:brightness", Param: "value", Value: 0.75})
	if err != nil {
		t.Fatalf("encode set: %v", err)
	}
	env, err := decodeEnvelope(bytes.TrimSpace(setFrame))
	if err != nil {
		t.Fatalf("decode set: %v", err)
	}
	cmd, ok, err := decodeCommand(env)
	if err != nil || !ok {
		t.Fatalf("decodeCommand set ok=%v err=%v", ok, err)
	}
	if cmd.kind != kindSet || cmd.set.Target != "exec:brightness" || cmd.set.Param != "value" || cmd.set.Value != 0.75 {
		t.Fatalf("set command = %+v", cmd)
	}

	toggleFrame, err := encodeFrame(kindToggle, togglePayload{Target: "exec:lamp", Param: "mute"})
	if err != nil {
		t.Fatalf("encode toggle: %v", err)
	}
	env, err = decodeEnvelope(bytes.TrimSpace(toggleFrame))
	if err != nil {
		t.Fatalf("decode toggle: %v", err)
	}
	cmd, ok, err = decodeCommand(env)
	if err != nil || !ok {
		t.Fatalf("decodeCommand toggle ok=%v err=%v", ok, err)
	}
	if cmd.kind != kindToggle || cmd.set.Target != "exec:lamp" || cmd.set.Param != "mute" {
		t.Fatalf("toggle command = %+v", cmd)
	}
}

func TestReadInitialStateRejectsUnexpectedFrame(t *testing.T) {
	frame, err := encodeFrame(kindSnapshot, snapshotWire{})
	if err != nil {
		t.Fatalf("encodeFrame() error = %v", err)
	}

	_, err = readInitialState(bufio.NewScanner(bytes.NewReader(frame)))
	if err == nil || !strings.Contains(err.Error(), `got "snapshot" frame`) {
		t.Fatalf("readInitialState() error = %v, want unexpected frame error", err)
	}
}

func TestReadInitialStateRejectsClosedConnection(t *testing.T) {
	_, err := readInitialState(bufio.NewScanner(strings.NewReader("")))
	if err == nil || !strings.Contains(err.Error(), "connection closed") {
		t.Fatalf("readInitialState() error = %v, want connection closed error", err)
	}
}
