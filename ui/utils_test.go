package ui

import "testing"

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
