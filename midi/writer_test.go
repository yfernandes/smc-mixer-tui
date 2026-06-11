package midi

import (
	"os"
	"testing"
)

func newTestWriter(t *testing.T) (*Writer, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })
	return &Writer{f: w}, r
}

func read3(t *testing.T, r *os.File) [3]byte {
	t.Helper()
	var buf [3]byte
	if _, err := r.Read(buf[:]); err != nil {
		t.Fatal(err)
	}
	return buf
}

// TestSetFaderPositionEncoding verifies the 14-bit pitch-bend encoding and
// that SetFaderPosition uses the correct status byte for each channel.
func TestSetFaderPositionEncoding(t *testing.T) {
	cases := []struct {
		ch   int
		vol  float64
		want [3]byte
	}{
		// raw=0: lsb=0x00, msb=0x00
		{0, 0.0, [3]byte{0xE0, 0x00, 0x00}},
		// raw=16383=0x3FFF: lsb=0x7F, msb=0x7F
		{0, 1.0, [3]byte{0xE0, 0x7F, 0x7F}},
		// raw≈8191=0x1FFF: lsb=0x7F, msb=0x3F
		{0, 0.5, [3]byte{0xE0, 0x7F, 0x3F}},
		// channel offset applied correctly
		{7, 1.0, [3]byte{0xE7, 0x7F, 0x7F}},
		// vol clamped below 0
		{0, -0.5, [3]byte{0xE0, 0x00, 0x00}},
		// vol clamped above 1
		{0, 1.5, [3]byte{0xE0, 0x7F, 0x7F}},
	}
	for _, tc := range cases {
		wr, r := newTestWriter(t)
		wr.SetFaderPosition(tc.ch, tc.vol)
		got := read3(t, r)
		if got != tc.want {
			t.Errorf("ch=%d vol=%.2f: got 0x%02X 0x%02X 0x%02X, want 0x%02X 0x%02X 0x%02X",
				tc.ch, tc.vol, got[0], got[1], got[2], tc.want[0], tc.want[1], tc.want[2])
		}
	}
}

// TestWritePanicOnFaderChannel verifies that write() refuses to emit
// pitch-bend messages (0xE0–0xE7), enforcing that only writeFader may
// access those channels.
func TestWritePanicOnFaderChannel(t *testing.T) {
	for _, status := range []byte{0xE0, 0xE3, 0xE7} {
		wr, _ := newTestWriter(t)
		status := status
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("status 0x%02X: expected panic, got none", status)
				}
			}()
			wr.write([3]byte{status, 0x00, 0x00})
		}()
	}
}
