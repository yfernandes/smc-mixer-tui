package smc46

import (
	"math"
	"testing"

	"github.com/yfernandes/smc-mixer-tui/midi"
	"github.com/yfernandes/smc-mixer-tui/surface"
)

func TestEventFromMsgFader(t *testing.T) {
	tests := []struct {
		name string
		raw  uint16
		want float64
	}{
		{name: "zero", raw: 0, want: 0},
		{name: "middle", raw: 8191, want: float64(8191) / MaxFaderValue},
		{name: "max", raw: 16383, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, ok := EventFromMsg(midi.FaderMsg{Channel: 3, Value: tt.raw})
			if !ok {
				t.Fatal("expected event")
			}
			if ev.Strip != 3 || ev.Role != surface.RoleFader {
				t.Fatalf("unexpected event: %+v", ev)
			}
			if math.Abs(ev.Value-tt.want) > 0.0000001 {
				t.Fatalf("value = %v, want %v", ev.Value, tt.want)
			}
		})
	}
}

func TestEventFromMsgKnobAndButton(t *testing.T) {
	ev, ok := EventFromMsg(midi.KnobMsg{Channel: 2, Delta: -1})
	if !ok {
		t.Fatal("expected knob event")
	}
	if ev.Strip != 2 || ev.Role != surface.RoleKnob || ev.Delta != -1 {
		t.Fatalf("unexpected knob event: %+v", ev)
	}

	ev, ok = EventFromMsg(midi.ButtonMsg{Channel: 4, Kind: midi.ButtonMute, Pressed: true})
	if !ok {
		t.Fatal("expected button event")
	}
	if ev.Strip != 4 || ev.Role != surface.RoleMute || !ev.Pressed {
		t.Fatalf("unexpected button event: %+v", ev)
	}
}

func TestEventFromMsgGlobal(t *testing.T) {
	ev, ok := EventFromMsg(midi.GlobalMsg{Action: midi.ActionPlay, Pressed: true})
	if !ok {
		t.Fatal("expected global event")
	}
	if ev.Strip != -1 || ev.Role != "play" || !ev.Pressed {
		t.Fatalf("unexpected global event: %+v", ev)
	}
}
