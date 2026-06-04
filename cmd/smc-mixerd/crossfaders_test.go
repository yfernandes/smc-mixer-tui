package main

import (
	"testing"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

func TestResolveCrossfaderSinksMatchesBySubstring(t *testing.T) {
	cfg := &config.Config{
		Devices: map[string]config.DeviceConfig{
			"speakers":   {Type: config.BindOutput, Match: "Ryzen HD Audio Analog Stereo"},
			"headphones": {Type: config.BindOutput, Match: "WH-1000XM4"},
		},
	}
	knob := config.KnobConfig{Type: config.KnobSend, BusA: "speakers", BusB: "headphones"}
	ss := []streams.EnrichedStream{
		{ID: 1, Name: "Ryzen HD Audio Analog Stereo", NodeName: "alsa_output.ryzen", Kind: audio.KindSink},
		{ID: 2, Name: "WH-1000XM4 Stereo", NodeName: "bluez_output.wh1000", Kind: audio.KindSink},
		{ID: 3, Name: "Some Source", NodeName: "source.foo", Kind: audio.KindSource},
	}

	nodeA, nodeB, nameA, nameB := resolveCrossfaderSinks(cfg, knob, ss)

	if nodeA != "alsa_output.ryzen" {
		t.Errorf("nodeA = %q, want alsa_output.ryzen", nodeA)
	}
	if nodeB != "bluez_output.wh1000" {
		t.Errorf("nodeB = %q, want bluez_output.wh1000", nodeB)
	}
	if nameA != "Ryzen HD Audio Analog Stereo" {
		t.Errorf("nameA = %q, want Ryzen HD Audio Analog Stereo", nameA)
	}
	if nameB != "WH-1000XM4 Stereo" {
		t.Errorf("nameB = %q, want WH-1000XM4 Stereo", nameB)
	}
}

func TestResolveCrossfaderSinksSkipsNonSinks(t *testing.T) {
	cfg := &config.Config{
		Devices: map[string]config.DeviceConfig{
			"speakers": {Type: config.BindOutput, Match: "My Speaker"},
		},
	}
	knob := config.KnobConfig{Type: config.KnobSend, BusA: "speakers", BusB: "speakers"}
	ss := []streams.EnrichedStream{
		{ID: 1, Name: "My Speaker", NodeName: "src_node", Kind: audio.KindSource},
		{ID: 2, Name: "My Speaker", NodeName: "mic_node", Kind: audio.KindMic},
	}

	nodeA, nodeB, _, _ := resolveCrossfaderSinks(cfg, knob, ss)

	if nodeA != "" || nodeB != "" {
		t.Errorf("should not match non-sink streams: nodeA=%q nodeB=%q", nodeA, nodeB)
	}
}

func TestResolveCrossfaderSinksCaseInsensitive(t *testing.T) {
	cfg := &config.Config{
		Devices: map[string]config.DeviceConfig{
			"hdmi": {Type: config.BindOutput, Match: "HDMI Output"},
		},
	}
	knob := config.KnobConfig{Type: config.KnobSend, BusA: "hdmi", BusB: "hdmi"}
	ss := []streams.EnrichedStream{
		{ID: 1, Name: "hdmi output digital stereo", NodeName: "alsa_output.hdmi", Kind: audio.KindSink},
	}

	nodeA, nodeB, _, _ := resolveCrossfaderSinks(cfg, knob, ss)

	if nodeA == "" {
		t.Error("nodeA: case-insensitive match should succeed")
	}
	if nodeB == "" {
		t.Error("nodeB: case-insensitive match should succeed (same sink is fine)")
	}
}

func TestResolveCrossfaderSinksUnresolvedDeviceFallsThrough(t *testing.T) {
	// ResolveOutput returns the key itself when not in Devices, which won't substring-match any sink.
	cfg := &config.Config{
		Devices: map[string]config.DeviceConfig{
			"speakers": {Type: config.BindOutput, Match: "Ryzen HD Audio"},
		},
	}
	knob := config.KnobConfig{Type: config.KnobSend, BusA: "ghost-device", BusB: "speakers"}
	ss := []streams.EnrichedStream{
		{ID: 1, Name: "Ryzen HD Audio Analog Stereo", NodeName: "alsa_output.ryzen", Kind: audio.KindSink},
	}

	nodeA, nodeB, _, _ := resolveCrossfaderSinks(cfg, knob, ss)

	if nodeA != "" {
		t.Errorf("nodeA: unknown device should not match any sink, got %q", nodeA)
	}
	if nodeB == "" {
		t.Error("nodeB: known device should match")
	}
}

func TestResolveCrossfaderSinksReturnsEmptyWhenNoStreams(t *testing.T) {
	cfg := &config.Config{
		Devices: map[string]config.DeviceConfig{
			"speakers": {Type: config.BindOutput, Match: "Ryzen HD Audio"},
		},
	}
	knob := config.KnobConfig{Type: config.KnobSend, BusA: "speakers", BusB: "speakers"}

	nodeA, nodeB, nameA, nameB := resolveCrossfaderSinks(cfg, knob, nil)

	if nodeA != "" || nodeB != "" || nameA != "" || nameB != "" {
		t.Errorf("empty stream list should return all empty: nodeA=%q nodeB=%q nameA=%q nameB=%q",
			nodeA, nodeB, nameA, nameB)
	}
}
