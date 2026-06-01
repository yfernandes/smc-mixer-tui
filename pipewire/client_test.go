package pipewire

import (
	"context"
	"fmt"
	"testing"
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
        "application.name": "should-be-skipped"
      }
    }
  }
]`

func TestParseStreams(t *testing.T) {
	streams, err := parseStreams([]byte(fixtureJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(streams) != 2 {
		t.Fatalf("want 2 streams, got %d: %v", len(streams), streams)
	}

	// Node with application.name wins over node.name; string-encoded PID is parsed
	if streams[0].ID != 97 || streams[0].Name != "Firefox" || streams[0].PID != 1234 {
		t.Errorf("streams[0]: got %+v", streams[0])
	}
	// Node without application.name falls back to node.name; numeric PID is parsed
	if streams[1].ID != 42 || streams[1].Name != "mpv" || streams[1].PID != 5678 {
		t.Errorf("streams[1]: got %+v", streams[1])
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

// — helpers —

func contains(s, sub string) bool { return len(s) >= len(sub) && (s == sub || len(sub) == 0 || containsSlow(s, sub)) }
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
