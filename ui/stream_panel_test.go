package ui

import (
	"strings"
	"testing"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

func TestBindSubtitlePrefersStreamSubtitle(t *testing.T) {
	es := streams.EnrichedStream{
		Name:      "Firefox",
		MediaName: "Meet call",
		WinTitle:  "Browser window",
	}
	if got := bindSubtitle(es); got != "Meet call" {
		t.Fatalf("bindSubtitle() = %q, want media name", got)
	}
}

func TestBindSubtitleFallsBackToDistinctWindowTitle(t *testing.T) {
	es := streams.EnrichedStream{Name: "Firefox", WinTitle: "Project docs"}
	if got := bindSubtitle(es); got != "Project docs" {
		t.Fatalf("bindSubtitle() = %q, want window title", got)
	}
}

func TestCenteredScrollKeepsHighlightedStreamVisible(t *testing.T) {
	cases := []struct {
		name    string
		idx     int
		count   int
		visible int
		want    int
	}{
		{name: "no highlight", idx: -1, count: 20, visible: 8, want: 0},
		{name: "near top", idx: 2, count: 20, visible: 8, want: 0},
		{name: "middle", idx: 10, count: 20, visible: 8, want: 6},
		{name: "near bottom", idx: 19, count: 20, visible: 8, want: 12},
		{name: "short list", idx: 4, count: 5, visible: 8, want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := centeredScroll(tc.idx, tc.count, tc.visible); got != tc.want {
				t.Fatalf("centeredScroll(%d, %d, %d) = %d, want %d", tc.idx, tc.count, tc.visible, got, tc.want)
			}
		})
	}
}

func TestRenderStreamRowsAddsHeadersAndHighlight(t *testing.T) {
	avail := []streams.EnrichedStream{
		{ID: 1, Name: "Firefox", Kind: audio.KindSource},
		{ID: 2, Name: "Mic", Kind: audio.KindMic},
	}

	rows := renderStreamRows(avail, 0, len(avail), 1, 80)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "Sources") || !strings.Contains(joined, "Microphones") {
		t.Fatalf("renderStreamRows() should include kind headers, got:\n%s", joined)
	}
	if !strings.Contains(joined, "▶ Mic") {
		t.Fatalf("renderStreamRows() should highlight selected stream, got:\n%s", joined)
	}
}
