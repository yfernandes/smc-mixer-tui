package pwbackend

import (
	"context"
	"testing"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/pipewire"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

type fakeCrossfaderPW struct {
	setups, teardowns, attaches int
	attachIDs                   []uint32
	gains                       [][2]float64
	healthy                     bool
}

func (f *fakeCrossfaderPW) CrossfaderHealthy(context.Context, *pipewire.CrossfaderRouting) (bool, error) {
	return f.healthy, nil
}

func (f *fakeCrossfaderPW) SetupCrossfader(_ context.Context, _ string, streamID uint32, _ string, _, _ string) (*pipewire.CrossfaderRouting, error) {
	f.setups++
	return &pipewire.CrossfaderRouting{StreamNodeID: streamID, NullSinkName: "smc_music_void", GainAName: "smc_music_gain_a", GainBName: "smc_music_gain_b"}, nil
}
func (f *fakeCrossfaderPW) SetCrossfaderGains(_ context.Context, _ *pipewire.CrossfaderRouting, a, b float64) error {
	f.gains = append(f.gains, [2]float64{a, b})
	return nil
}
func (f *fakeCrossfaderPW) AttachCrossfaderStream(_ context.Context, routing *pipewire.CrossfaderRouting, streamID uint32, _ string) error {
	f.attaches++
	f.attachIDs = append(f.attachIDs, streamID)
	routing.StreamNodeID = streamID
	return nil
}
func (f *fakeCrossfaderPW) TeardownCrossfader(context.Context, *pipewire.CrossfaderRouting) {
	f.teardowns++
}
func (*fakeCrossfaderPW) RetargetCrossfaderOutput(context.Context, uint32, string, string) (uint32, error) {
	return 1, nil
}
func (*fakeCrossfaderPW) SetMute(context.Context, uint32, bool) error { return nil }

func TestCrossfaderLifecycleAnchorsGraphToRuleAcrossStreamChurn(t *testing.T) {
	pw := &fakeCrossfaderPW{healthy: true}
	cfg := &config.Config{Devices: map[string]config.DeviceConfig{
		"music": {Type: config.BindPlayback, Knob: &config.KnobConfig{Type: config.KnobSend, BusA: "a", BusB: "b"}},
		"a":     {Type: config.BindOutput, Match: "Speakers"},
		"b":     {Type: config.BindOutput, Match: "Headphones"},
	}}
	mgr := newCrossfaderManager(cfg, pw)
	outputs := []streams.EnrichedStream{{ID: 20, Name: "Speakers", NodeName: "sink.a", Kind: audio.KindSink}, {ID: 21, Name: "Headphones", NodeName: "sink.b", Kind: audio.KindSink}}
	first := streams.EnrichedStream{ID: 10, Name: "Music", NodeName: "music.1", Kind: audio.KindSource}
	mgr.Sync(context.Background(), map[string]streams.EnrichedStream{"music": first}, append(outputs, first))
	if pw.setups != 1 {
		t.Fatalf("setups = %d, want 1", pw.setups)
	}
	if len(pw.gains) != 1 || pw.gains[0] != [2]float64{1, 1} {
		t.Fatalf("initial gains = %v, want center [1 1]", pw.gains)
	}
	mgr.Sync(context.Background(), nil, outputs)
	second := streams.EnrichedStream{ID: 11, Name: "Music", NodeName: "music.2", Kind: audio.KindSource}
	mgr.Sync(context.Background(), map[string]streams.EnrichedStream{"music": second}, append(outputs, second))
	if pw.setups != 1 || pw.teardowns != 0 || pw.attaches != 1 {
		t.Fatalf("stream churn reconciliation: setups=%d teardowns=%d attaches=%d", pw.setups, pw.teardowns, pw.attaches)
	}
	if got := mgr.Snapshot()[0].StreamID; got != 11 {
		t.Fatalf("active stream = %d, want replacement 11", got)
	}
	mgr.Close(context.Background())
	if pw.teardowns != 1 {
		t.Fatalf("teardowns after close = %d, want 1", pw.teardowns)
	}
}

func TestCrossfaderLifecycleRebuildsGraphAfterPipeWireRestart(t *testing.T) {
	pw := &fakeCrossfaderPW{healthy: true}
	cfg := &config.Config{Devices: map[string]config.DeviceConfig{
		"music": {Type: config.BindPlayback, Knob: &config.KnobConfig{Type: config.KnobSend, BusA: "a", BusB: "b"}},
		"a":     {Type: config.BindOutput, Match: "Speakers"},
		"b":     {Type: config.BindOutput, Match: "Headphones"},
	}}
	mgr := newCrossfaderManager(cfg, pw)
	stream := streams.EnrichedStream{ID: 10, Name: "Music", NodeName: "music.1", Kind: audio.KindSource}
	all := []streams.EnrichedStream{
		{ID: 20, Name: "Speakers", NodeName: "sink.a", Kind: audio.KindSink},
		{ID: 21, Name: "Headphones", NodeName: "sink.b", Kind: audio.KindSink},
		stream,
	}
	resolved := map[string]streams.EnrichedStream{"music": stream}
	mgr.Sync(context.Background(), resolved, all)
	pw.healthy = false
	mgr.Sync(context.Background(), resolved, all)
	if pw.teardowns != 1 || pw.setups != 2 {
		t.Fatalf("restart reconciliation: setups=%d teardowns=%d", pw.setups, pw.teardowns)
	}
}

func TestCrossfaderLifecycleAttachesEveryMatchingApplicationStream(t *testing.T) {
	pw := &fakeCrossfaderPW{healthy: true}
	cfg := &config.Config{Devices: map[string]config.DeviceConfig{
		"firefox": {Type: config.BindPlayback, MatchRegex: "(?i)firefox.*", Knob: &config.KnobConfig{Type: config.KnobSend, BusA: "a", BusB: "b"}},
		"a":       {Type: config.BindOutput, Match: "Speakers"},
		"b":       {Type: config.BindOutput, Match: "Headphones"},
	}}
	mgr := newCrossfaderManager(cfg, pw)
	idle := streams.EnrichedStream{ID: 10, Name: "Old tab", BindKey: "firefox.instance_1", NodeName: "firefox.1", Kind: audio.KindSource}
	active := streams.EnrichedStream{ID: 11, Name: "Playing tab", BindKey: "firefox.instance_1", NodeName: "firefox.2", Kind: audio.KindSource, Active: true}
	all := []streams.EnrichedStream{
		{ID: 20, Name: "Speakers", NodeName: "sink.a", Kind: audio.KindSink},
		{ID: 21, Name: "Headphones", NodeName: "sink.b", Kind: audio.KindSink},
		idle, active,
	}
	mgr.Sync(context.Background(), map[string]streams.EnrichedStream{"firefox": idle}, all)
	if pw.setups != 1 || pw.attaches != 2 {
		t.Fatalf("multi-stream reconciliation: setups=%d attaches=%d, want 1 and 2", pw.setups, pw.attaches)
	}
	if got := pw.attachIDs; len(got) != 2 || got[len(got)-1] != active.ID {
		t.Fatalf("attach order = %v, want active stream %d reasserted last", got, active.ID)
	}
}
