package ui

import (
	"strings"
	"testing"
)

func TestCapitalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"spotify", "Spotify"},
		{"Zen", "Zen"},
		{"firefox", "Firefox"},
		{"a", "A"},
		{"ABC", "ABC"},
		{"", ""},
	}
	for _, c := range cases {
		if got := capitalize(c.in); got != c.want {
			t.Errorf("capitalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWrapTwo(t *testing.T) {
	cases := []struct {
		s, l1, l2 string
		width      int
	}{
		// Fits on one line — second line empty.
		{"short", "short", "", 12},
		// Split at word boundary.
		{"hello world", "hello", "world", 8},
		// Long second line gets truncated.
		{"Promisifying a delay function is annoying", "Promisifying", "a delay fun…", 12},
		// No space within width — hard-split at width boundary.
		{"nospaces!", "nospac", "es!", 6},
		// Artist – track pattern.
		{"TOOL – Invincible", "TOOL –", "Invincible", 12},
	}
	for _, c := range cases {
		l1, l2 := wrapTwo(c.s, c.width)
		if l1 != c.l1 || l2 != c.l2 {
			t.Errorf("wrapTwo(%q, %d):\n  got  (%q, %q)\n  want (%q, %q)",
				c.s, c.width, l1, l2, c.l1, c.l2)
		}
	}
}

func TestDualFaderRows_RowCount(t *testing.T) {
	rows := dualFaderRows(0.5, 0.5, true, true, false, 5, 8)
	if len(rows) != 5 {
		t.Fatalf("want 5 rows, got %d", len(rows))
	}
}

func TestDualFaderRows_TickChars(t *testing.T) {
	rows := dualFaderRows(0.5, 0.5, true, true, false, 5, 8)
	// All rows except the last start with the top tick; last row uses the bottom tick.
	for i, row := range rows {
		// Strip ANSI escapes by checking for the tick characters in the raw string.
		if i < len(rows)-1 {
			if !strings.Contains(row, "▔") {
				t.Errorf("row %d: want top tick ▔, got %q", i, row)
			}
		} else {
			if !strings.Contains(row, "🮀") {
				t.Errorf("last row: want bottom tick 🮀, got %q", row)
			}
		}
	}
}

func TestDualFaderRows_HWUnknownShowsFloorOnly(t *testing.T) {
	// When hwKnown=false, only the bottom row should carry HW content.
	rows := dualFaderRows(0.8, 0, false, false, false, 5, 8)
	for i, row := range rows[:len(rows)-1] {
		// Non-bottom rows: no HW block character.
		if strings.Contains(row, "▓") {
			t.Errorf("row %d: unexpected HW block in unknown state: %q", i, row)
		}
	}
	// Bottom row must contain the HW floor marker.
	if !strings.Contains(rows[len(rows)-1], "▓") {
		t.Errorf("bottom row: missing HW floor marker in unknown state")
	}
}

func TestDualFaderRows_AppNotShownWhenUnbound(t *testing.T) {
	rows := dualFaderRows(1.0, 1.0, true, false, false, 5, 8)
	for _, row := range rows {
		if strings.Contains(row, "░") {
			t.Errorf("APP bar should not appear when unbound: %q", row)
		}
	}
}

func TestDualFaderRows_BothWhiteWhenSynced(t *testing.T) {
	// Synced: both bars should use syncFaderStyle; neither hwFaderStyle nor appFaderStyle.
	// We can't easily inspect the style, but we can verify both block chars appear
	// when faders are high and synced=true.
	rows := dualFaderRows(1.0, 1.0, true, true, true, 5, 8)
	allContent := strings.Join(rows, "")
	if !strings.Contains(allContent, "▓") {
		t.Error("synced: HW blocks should be present")
	}
	if !strings.Contains(allContent, "░") {
		t.Error("synced: APP blocks should be present")
	}
}
