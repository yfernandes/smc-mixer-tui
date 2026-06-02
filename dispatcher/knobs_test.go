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

func TestCrossfadeGains(t *testing.T) {
	cases := []struct {
		name        string
		knob        int
		wantA       float64
		wantB       float64
		minCenterAB float64
	}{
		{"hard left", 0, 1.0, 0.0, 0},
		{"hard right", 127, 0.0, 1.0, 0},
		{"center plateau", 64, 0, 0, 0.98},
	}

	for _, c := range cases {
		gotA, gotB := crossfadeGains(c.knob)
		if c.minCenterAB > 0 {
			if gotA < c.minCenterAB || gotB < c.minCenterAB {
				t.Fatalf("%s: got A=%.4f B=%.4f, want both >= %.4f", c.name, gotA, gotB, c.minCenterAB)
			}
			continue
		}
		if !approxEq(gotA, c.wantA) || !approxEq(gotB, c.wantB) {
			t.Fatalf("%s: got A=%.4f B=%.4f, want A=%.4f B=%.4f", c.name, gotA, gotB, c.wantA, c.wantB)
		}
	}
}
