package dispatcher

import "testing"

func TestClampKnob(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"below range", -1, 0},
		{"zero", 0, 0},
		{"middle", 64, 64},
		{"max", 127, 127},
		{"above range", 128, 127},
	}

	for _, c := range cases {
		if got := clampKnob(c.in); got != c.want {
			t.Fatalf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}
