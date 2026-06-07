package pipewire

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/yfernandes/smc-mixer-tui/audio"
)

// — parseVolumeLine —

func TestParseVolumeLine(t *testing.T) {
	cases := []struct {
		line        string
		wantVol     float64
		wantMuted   bool
		wantErrFrag string
	}{
		{"Volume: 1.00", 1.0, false, ""},
		{"Volume: 0.50", 0.5, false, ""},
		{"Volume: 0.00", 0.0, false, ""},
		{"Volume: 1.00 [MUTED]", 1.0, true, ""},
		{"Volume: 0.35 [MUTED]", 0.35, true, ""},
		{"garbage", 0, false, "unexpected"},
		{"Volume: notafloat", 0, false, "parse volume"},
	}

	for _, c := range cases {
		vol, muted, err := parseVolumeLine(c.line)
		if c.wantErrFrag != "" {
			if err == nil || !contains(err.Error(), c.wantErrFrag) {
				t.Errorf("%q: want err containing %q, got %v", c.line, c.wantErrFrag, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error: %v", c.line, err)
			continue
		}
		if abs(vol-c.wantVol) > 1e-9 {
			t.Errorf("%q: vol = %.6f, want %.6f", c.line, vol, c.wantVol)
		}
		if muted != c.wantMuted {
			t.Errorf("%q: muted = %v, want %v", c.line, muted, c.wantMuted)
		}
	}
}

// — parseStreams —

const fixtureJSON = `[
  {
    "id": 97,
    "type": "PipeWire:Interface:Node",
    "info": {
      "props": {
        "media.class": "Stream/Output/Audio",
        "application.name": "Firefox",
        "node.name": "Firefox",
        "application.process.id": "1234"
      }
    }
  },
  {
    "id": 42,
    "type": "PipeWire:Interface:Node",
    "info": {
      "props": {
        "media.class": "Stream/Output/Audio",
        "node.name": "mpv",
        "application.process.id": 5678
      }
    }
  },
  {
    "id": 10,
    "type": "PipeWire:Interface:Client",
    "info": {
      "props": {
        "media.class": "Stream/Output/Audio",
        "application.name": "should-be-skipped"
      }
    }
  },
  {
    "id": 11,
    "type": "PipeWire:Interface:Node",
    "info": {
      "props": {
        "media.class": "Audio/Sink",
        "node.description": "Built-in Audio Analog Stereo",
        "node.name": "alsa_output.pci-0000_00_1f.3"
      }
    }
  }
]`

func TestParseStreams(t *testing.T) {
	streams, err := parseStreams([]byte(fixtureJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(streams) != 3 {
		t.Fatalf("want 3 streams, got %d: %v", len(streams), streams)
	}

	// Node with application.name wins over node.name; string-encoded PID is parsed
	if streams[0].ID != 97 || streams[0].Name != "Firefox" || streams[0].PID != 1234 || streams[0].Kind != audio.KindSource {
		t.Errorf("streams[0]: got %+v", streams[0])
	}
	// Node without application.name falls back to node.name; numeric PID is parsed
	if streams[1].ID != 42 || streams[1].Name != "mpv" || streams[1].PID != 5678 || streams[1].Kind != audio.KindSource {
		t.Errorf("streams[1]: got %+v", streams[1])
	}
	// Sink node uses node.description and has audio.KindSink
	if streams[2].ID != 11 || streams[2].Name != "Built-in Audio Analog Stereo" || streams[2].Kind != audio.KindSink {
		t.Errorf("streams[2]: got %+v", streams[2])
	}
}

func TestParseStreams_MissingPID(t *testing.T) {
	data := `[{"id":7,"type":"PipeWire:Interface:Node","info":{"props":{"media.class":"Stream/Output/Audio","node.name":"vlc"}}}]`
	ss, err := parseStreams([]byte(data))
	if err != nil || len(ss) != 1 || ss[0].PID != 0 {
		t.Errorf("missing PID should be 0, got %+v, err=%v", ss, err)
	}
}

func TestParseStreams_FallbackName(t *testing.T) {
	// Node with neither application.name nor node.name gets a synthetic name.
	data := `[{"id":5,"type":"PipeWire:Interface:Node","info":{"props":{"media.class":"Stream/Output/Audio"}}}]`
	streams, err := parseStreams([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(streams) != 1 || streams[0].Name != "stream-5" {
		t.Errorf("got %+v", streams)
	}
}

// TestParseStreams_ClientPIDResolution covers the Spotify case: the stream node
// has no application.process.id, but its client.id points to a Client node
// that carries pipewire.sec.pid. The parser must resolve the PID and the
// application.name via the client.
func TestParseStreams_ClientPIDResolution(t *testing.T) {
	data := `[
	  {
	    "id": 96,
	    "type": "PipeWire:Interface:Client",
	    "info": {"props": {
	      "application.name": "spotify",
	      "pipewire.sec.pid": 2001934
	    }}
	  },
	  {
	    "id": 124,
	    "type": "PipeWire:Interface:Node",
	    "info": {"props": {
	      "media.class": "Stream/Output/Audio",
	      "node.name":   "audio-src",
	      "client.id":   96
	    }}
	  }
	]`
	ss, err := parseStreams([]byte(data))
	if err != nil || len(ss) != 1 {
		t.Fatalf("err=%v, len=%d", err, len(ss))
	}
	s := ss[0]
	if s.PID != 2001934 {
		t.Errorf("PID: got %d, want 2001934", s.PID)
	}
	if s.Name != "spotify" {
		t.Errorf("Name: got %q, want %q", s.Name, "spotify")
	}
}

func TestParseStreams_SuspendedStreamFiltered(t *testing.T) {
	data := `[
	  {"id":1,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Stream/Output/Audio","node.name":"spotify"}}},
	  {"id":2,"type":"PipeWire:Interface:Node","info":{"state":"suspended","props":{"media.class":"Stream/Output/Audio","node.name":"zombie-tab"}}},
	  {"id":3,"type":"PipeWire:Interface:Node","info":{"state":"idle","props":{"media.class":"Stream/Output/Audio","node.name":"paused-player"}}}
	]`
	ss, err := parseStreams([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 2 {
		t.Fatalf("want 2 streams (running+idle), got %d: %v", len(ss), ss)
	}
	names := map[string]bool{}
	for _, s := range ss {
		names[s.Name] = true
	}
	if !names["spotify"] || !names["paused-player"] {
		t.Errorf("expected spotify and paused-player, got names=%v", names)
	}
	if names["zombie-tab"] {
		t.Errorf("suspended stream must be filtered out")
	}
}

func TestParseStreams_SuspendedSinkNotFiltered(t *testing.T) {
	// Hardware sinks must never be filtered by state — state filter is Stream-only.
	data := `[{"id":11,"type":"PipeWire:Interface:Node","info":{"state":"suspended","props":{"media.class":"Audio/Sink","node.description":"Built-in Audio"}}}]`
	ss, err := parseStreams([]byte(data))
	if err != nil || len(ss) != 1 {
		t.Fatalf("suspended sink must not be filtered: err=%v len=%d", err, len(ss))
	}
}

func TestParseStreams_InvalidJSON(t *testing.T) {
	_, err := parseStreams([]byte("{not json}"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// — Client.SetVolume / SetMute / GetVolume via injected exec —

func fakeClient(responses map[string]string) *Client {
	return &Client{
		exec: func(_ context.Context, name string, args ...string) ([]byte, error) {
			key := name
			for _, a := range args {
				key += " " + a
			}
			if resp, ok := responses[key]; ok {
				return []byte(resp), nil
			}
			return nil, fmt.Errorf("unexpected command: %s", key)
		},
	}
}

func TestClientSetVolume(t *testing.T) {
	c := fakeClient(map[string]string{
		"wpctl set-volume 97 0.7500": "",
	})
	if err := c.SetVolume(context.Background(), 97, 0.75); err != nil {
		t.Fatal(err)
	}
}

func TestClientSetVolume_Clamping(t *testing.T) {
	var calledWith string
	c := &Client{
		exec: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calledWith = args[2] // the volume argument
			return nil, nil
		},
	}

	_ = c.SetVolume(context.Background(), 1, 1.5)
	if calledWith != "1.0000" {
		t.Errorf("above 1.0: want 1.0000, got %s", calledWith)
	}

	_ = c.SetVolume(context.Background(), 1, -0.1)
	if calledWith != "0.0000" {
		t.Errorf("below 0.0: want 0.0000, got %s", calledWith)
	}
}

func TestClientSetMute(t *testing.T) {
	c := fakeClient(map[string]string{
		"wpctl set-mute 97 1": "",
		"wpctl set-mute 97 0": "",
	})
	if err := c.SetMute(context.Background(), 97, true); err != nil {
		t.Fatal("mute:", err)
	}
	if err := c.SetMute(context.Background(), 97, false); err != nil {
		t.Fatal("unmute:", err)
	}
}

func TestClientGetVolume(t *testing.T) {
	c := fakeClient(map[string]string{
		"wpctl get-volume 97": "Volume: 0.75\n",
		"wpctl get-volume 42": "Volume: 0.50 [MUTED]\n",
	})
	vol, muted, err := c.GetVolume(context.Background(), 97)
	if err != nil || abs(vol-0.75) > 1e-9 || muted {
		t.Errorf("97: vol=%v muted=%v err=%v", vol, muted, err)
	}
	vol, muted, err = c.GetVolume(context.Background(), 42)
	if err != nil || abs(vol-0.50) > 1e-9 || !muted {
		t.Errorf("42: vol=%v muted=%v err=%v", vol, muted, err)
	}
}

// — crossfader setup —

type recordingExec struct {
	loadIDs      []uint32
	failLoad     string
	modules      string
	pwDump       string
	sinkInputs   string
	moveSinkErr  bool
	commands     []string
	unloaded     []uint32
	nextLoadCall int
}

func (r *recordingExec) client() *Client {
	return &Client{
		exec: func(_ context.Context, name string, args ...string) ([]byte, error) {
			cmd := commandKey(name, args...)
			r.commands = append(r.commands, cmd)
			if name == "pactl" && len(args) >= 3 && args[0] == "load-module" {
				if r.failLoad != "" && strings.Contains(cmd, r.failLoad) {
					return nil, fmt.Errorf("load failed")
				}
				id := r.loadIDs[r.nextLoadCall]
				r.nextLoadCall++
				return []byte(fmt.Sprintf("%d\n", id)), nil
			}
			if name == "pactl" && len(args) == 2 && args[0] == "unload-module" {
				id, _ := parseTestID(args[1])
				r.unloaded = append(r.unloaded, id)
				return []byte{}, nil
			}
			if cmd == "pactl list short modules" {
				return []byte(r.modules), nil
			}
			if cmd == "pw-dump" {
				return []byte(r.pwDump), nil
			}
			if name == "pw-metadata" {
				return []byte{}, nil
			}
			if cmd == "pactl list sink-inputs" {
				return []byte(r.sinkInputs), nil
			}
			if name == "pactl" && len(args) == 3 && args[0] == "move-sink-input" {
				if r.moveSinkErr {
					return nil, fmt.Errorf("move failed")
				}
				return []byte{}, nil
			}
			return nil, fmt.Errorf("unexpected command: %s", cmd)
		},
	}
}

func TestSetupCrossfaderLoadsModulesAndMovesStream(t *testing.T) {
	rec := &recordingExec{
		loadIDs:    []uint32{101, 102, 103, 104, 105, 106, 107},
		pwDump:     crossfaderPWDumpFixture("smc_ch0_void", 801),
		sinkInputs: sinkInputFixture(77, 555, "firefox.node"),
	}

	routing, err := rec.client().SetupCrossfader(context.Background(), "ch0", 555, "firefox.node", "sink_a", "sink_b")
	if err != nil {
		t.Fatal(err)
	}

	if routing.NullSinkModule != 101 || routing.Loop2BModule != 107 || routing.StreamSI != 77 {
		t.Fatalf("unexpected routing: %+v", routing)
	}
	if routing.StreamNodeID != 555 {
		t.Fatalf("StreamNodeID = %d, want 555", routing.StreamNodeID)
	}
	if routing.NullSinkName != "smc_ch0_void" || routing.GainAName != "smc_ch0_gain_a" || routing.GainBName != "smc_ch0_gain_b" {
		t.Fatalf("unexpected generated names: %+v", routing)
	}
	if !commandsContain(rec.commands, "pw-metadata 555 target.object 801") {
		t.Fatalf("expected target.object route to null sink, commands=%v", rec.commands)
	}
	if !commandsContain(rec.commands, "pactl move-sink-input 77 smc_ch0_void") {
		t.Fatalf("expected stream move to null sink, commands=%v", rec.commands)
	}
	if !commandsContain(rec.commands, "pw-metadata -d 555 target.object") {
		t.Fatalf("expected target.object route to be cleared after move, commands=%v", rec.commands)
	}
	if len(rec.unloaded) != 0 {
		t.Fatalf("successful setup should not unload modules, got %v", rec.unloaded)
	}
}

func TestSetupCrossfaderCleansUpStaleModulesForTag(t *testing.T) {
	rec := &recordingExec{
		loadIDs:    []uint32{101, 102, 103, 104, 105, 106, 107},
		modules:    pulseModuleFixture(),
		pwDump:     crossfaderPWDumpFixture("smc_ch0_void", 801),
		sinkInputs: sinkInputFixture(77, 555, "firefox.node"),
	}

	_, err := rec.client().SetupCrossfader(context.Background(), "ch0", 555, "firefox.node", "sink_a", "sink_b")
	if err != nil {
		t.Fatal(err)
	}

	if !sameUint32s(rec.unloaded, []uint32{207, 206, 205, 204, 203, 202, 201}) {
		t.Fatalf("unloaded = %v, want stale ch0 modules in reverse load order", rec.unloaded)
	}
	firstLoad := -1
	for i, cmd := range rec.commands {
		if strings.HasPrefix(cmd, "pactl load-module") {
			firstLoad = i
			break
		}
	}
	if firstLoad < 0 {
		t.Fatalf("no load-module commands: %v", rec.commands)
	}
	for _, cmd := range rec.commands[:firstLoad] {
		if strings.HasPrefix(cmd, "pactl unload-module") {
			continue
		}
		if cmd == "pactl list short modules" {
			continue
		}
		t.Fatalf("unexpected command before new modules are loaded: %s", cmd)
	}
}

func TestSetupCrossfaderRollsBackLoadedModulesOnLoadFailure(t *testing.T) {
	rec := &recordingExec{
		loadIDs:  []uint32{101, 102, 103, 104, 105, 106, 107},
		failLoad: "source=smc_ch0_gain_a.monitor sink=sink_a",
		pwDump:   crossfaderPWDumpFixture("smc_ch0_void", 801),
	}

	_, err := rec.client().SetupCrossfader(context.Background(), "ch0", 555, "firefox.node", "sink_a", "sink_b")
	if err == nil || !strings.Contains(err.Error(), "loopback 2A") {
		t.Fatalf("expected loopback 2A error, got %v", err)
	}
	if !sameUint32s(rec.unloaded, []uint32{105, 104, 103, 102, 101}) {
		t.Fatalf("unloaded = %v, want reverse loaded modules", rec.unloaded)
	}
	if commandsContainPrefix(rec.commands, "pactl move-sink-input") {
		t.Fatalf("must not move stream after setup failure, commands=%v", rec.commands)
	}
}

func TestSetupCrossfaderRollsBackLoadedModulesWhenStreamMoveFails(t *testing.T) {
	rec := &recordingExec{
		loadIDs:     []uint32{101, 102, 103, 104, 105, 106, 107},
		pwDump:      crossfaderPWDumpFixture("smc_ch0_void", 801),
		sinkInputs:  sinkInputFixture(77, 555, "firefox.node"),
		moveSinkErr: true,
	}

	_, err := rec.client().SetupCrossfader(context.Background(), "ch0", 555, "firefox.node", "sink_a", "sink_b")
	if err == nil || !strings.Contains(err.Error(), "move stream to null sink") {
		t.Fatalf("expected move error, got %v", err)
	}
	if !sameUint32s(rec.unloaded, []uint32{107, 106, 105, 104, 103, 102, 101}) {
		t.Fatalf("unloaded = %v, want all modules in reverse", rec.unloaded)
	}
}

func TestTeardownCrossfaderRestoresDefaultSinkThenUnloads(t *testing.T) {
	rec := &recordingExec{}
	routing := &CrossfaderRouting{
		NullSinkModule: 101,
		GainAModule:    102,
		GainBModule:    103,
		LoopAModule:    104,
		LoopBModule:    105,
		Loop2AModule:   106,
		Loop2BModule:   107,
		StreamSI:       77,
		StreamNodeID:   555,
	}

	rec.client().TeardownCrossfader(context.Background(), routing)

	if len(rec.commands) < 2 || rec.commands[0] != "pw-metadata -d 555 target.object" || rec.commands[1] != "pactl move-sink-input 77 @DEFAULT_SINK@" {
		t.Fatalf("teardown should clear target.object then restore default sink, commands=%v", rec.commands)
	}
	if !sameUint32s(rec.unloaded, []uint32{107, 106, 105, 104, 103, 102, 101}) {
		t.Fatalf("unloaded = %v, want reverse routing modules", rec.unloaded)
	}
}

// — helpers —

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || containsSlow(s, sub))
}
func containsSlow(s, sub string) bool {
	for i := range s {
		if i+len(sub) <= len(s) && s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func commandKey(name string, args ...string) string {
	key := name
	for _, a := range args {
		key += " " + a
	}
	return key
}

func sinkInputFixture(index, nodeID uint32, nodeName string) string {
	return fmt.Sprintf(`Sink Input #%d
	Owner Module: 4294967295
	Properties:
		node.id = "%d"
		node.name = "%s"
`, index, nodeID, nodeName)
}

func pulseModuleFixture() string {
	return strings.Join([]string{
		"10\tmodule-null-sink\tsink_name=unrelated",
		"201\tmodule-null-sink\tsink_name=smc_ch0_void sink_properties=device.description=smc_ch0_void",
		"202\tmodule-null-sink\tsink_name=smc_ch0_gain_a sink_properties=device.description=smc_ch0_gain_a",
		"203\tmodule-null-sink\tsink_name=smc_ch0_gain_b sink_properties=device.description=smc_ch0_gain_b",
		"204\tmodule-loopback\tsource=smc_ch0_void.monitor sink=smc_ch0_gain_a source.dont.move=true sink.dont.move=true latency_msec=50",
		"205\tmodule-loopback\tsource=smc_ch0_void.monitor sink=smc_ch0_gain_b source.dont.move=true sink.dont.move=true latency_msec=50",
		"206\tmodule-loopback\tsource=smc_ch0_gain_a.monitor sink=sink_a source.dont.move=true sink.dont.move=true latency_msec=50",
		"207\tmodule-loopback\tsource=smc_ch0_gain_b.monitor sink=sink_b source.dont.move=true sink.dont.move=true latency_msec=50",
		"208\tmodule-null-sink\tsink_name=smc_ch1_void sink_properties=device.description=smc_ch1_void",
	}, "\n")
}

func crossfaderPWDumpFixture(nullName string, nullID uint32) string {
	return fmt.Sprintf(`[
		{"id": %d, "type": "PipeWire:Interface:Node", "info": {"props": {
			"media.class": "Audio/Sink",
			"node.name": "%s"
		}}}
	]`, nullID, nullName)
}

func parseTestID(s string) (uint32, error) {
	var id uint32
	_, err := fmt.Sscanf(s, "%d", &id)
	return id, err
}

func commandsContain(commands []string, want string) bool {
	for _, cmd := range commands {
		if cmd == want {
			return true
		}
	}
	return false
}

func commandsContainPrefix(commands []string, prefix string) bool {
	for _, cmd := range commands {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}
	return false
}

func sameUint32s(got, want []uint32) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
