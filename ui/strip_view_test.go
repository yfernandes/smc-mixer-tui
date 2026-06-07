package ui

import (
	"testing"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

func TestSplitFaderHeaderUsesStaticConfigWhenPresent(t *testing.T) {
	header, label := splitFaderHeader(1, StripConfig{FaderType: "output", FaderLabel: "Main speakers"}, dispatcher.Channel{}, stateUnbound)

	if !contains(header, "F2") || !contains(header, "output") {
		t.Fatalf("header = %q, want channel and output type", header)
	}
	if label != "Main speake…" {
		t.Fatalf("label = %q, want truncated config label", label)
	}
}

func TestSplitFaderHeaderShowsOfflineDynamicStream(t *testing.T) {
	id := uint32(9)
	header, label := splitFaderHeader(0, StripConfig{}, dispatcher.Channel{
		StreamID: &id,
		Kind:     audio.KindSource,
		Name:     "Firefox",
	}, stateInactive)

	if !contains(header, "F1") || !contains(header, "source") {
		t.Fatalf("header = %q, want source dynamic fader header", header)
	}
	if !contains(label, "⊗ offline") {
		t.Fatalf("label = %q, want offline tombstone", label)
	}
}

func TestStripHeaderUsesMPRISTrackMetadata(t *testing.T) {
	id := uint32(7)
	m := makeModel(&fakeDisp{}, nil)
	m.channels[0].StreamID = &id
	es := &streams.EnrichedStream{
		ID:      id,
		Name:    "spotify",
		AppName: "spotify",
		Source:  streams.SourceMPRIS,
		Artist:  "Nina Simone",
		Track:   "Sinnerman",
	}

	header, nameLine, subLine := m.stripHeader(0, m.channels[0], es, stateActive, false)
	if header != "CH1 - Spoti…" {
		t.Fatalf("header = %q, want enriched app header", header)
	}
	if nameLine != "Nina Simone" || subLine != "Sinnerman" {
		t.Fatalf("track lines = (%q, %q), want artist and track", nameLine, subLine)
	}
}

func TestStripHeaderMarksFocusedStreamNav(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m.navSetting = navStream

	_, nameLine, _ := m.stripHeader(0, dispatcher.Channel{}, nil, stateUnbound, true)
	if nameLine != "⇌ ---" {
		t.Fatalf("nameLine = %q, want focused stream marker", nameLine)
	}
}
