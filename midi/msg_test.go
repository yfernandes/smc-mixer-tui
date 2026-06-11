package midi

import "testing"

func TestClassifyFaders(t *testing.T) {
	for ch := 0; ch < 8; ch++ {
		msg, ok := Classify([3]byte{byte(0xe0 + ch), 0x00, 0x60})
		if !ok {
			t.Fatalf("ch%d fader not classified", ch)
		}
		f, ok := msg.(FaderMsg)
		if !ok {
			t.Fatalf("ch%d: want FaderMsg, got %T", ch, msg)
		}
		if f.Channel != ch || f.Value != uint16(0x60)<<7 {
			t.Fatalf("ch%d: got Channel=%d Value=%d", ch, f.Channel, f.Value)
		}
	}
}

func TestClassifyKnobs(t *testing.T) {
	for ch := 0; ch < 8; ch++ {
		cc := byte(16 + ch)

		inc, ok := Classify([3]byte{0xb0, cc, 1})
		if !ok {
			t.Fatalf("ch%d knob inc not classified", ch)
		}
		k, ok := inc.(KnobMsg)
		if !ok || k.Channel != ch || k.Delta != 1 {
			t.Fatalf("ch%d inc: got %+v", ch, inc)
		}

		dec, ok := Classify([3]byte{0xb0, cc, 65})
		if !ok {
			t.Fatalf("ch%d knob dec not classified", ch)
		}
		k, ok = dec.(KnobMsg)
		if !ok || k.Channel != ch || k.Delta != -1 {
			t.Fatalf("ch%d dec: got %+v", ch, dec)
		}
	}
}

func TestClassifyChannelButtons(t *testing.T) {
	cases := []struct {
		note    byte
		kind    ButtonKind
		channel int
	}{
		// Rec bank
		{0, ButtonRec, 0}, {7, ButtonRec, 7},
		// Solo bank
		{8, ButtonSolo, 0}, {15, ButtonSolo, 7},
		// Mute bank
		{16, ButtonMute, 0}, {23, ButtonMute, 7},
		// Stop bank
		{24, ButtonStop, 0}, {31, ButtonStop, 7},
	}

	for _, c := range cases {
		msg, ok := Classify([3]byte{0x90, c.note, 127})
		if !ok {
			t.Fatalf("note %d not classified", c.note)
		}
		b, ok := msg.(ButtonMsg)
		if !ok {
			t.Fatalf("note %d: want ButtonMsg, got %T", c.note, msg)
		}
		if b.Channel != c.channel || b.Kind != c.kind || !b.Pressed {
			t.Fatalf("note %d: got %+v, want ch=%d kind=%d pressed=true", c.note, b, c.channel, c.kind)
		}

		// release
		msg, ok = Classify([3]byte{0x90, c.note, 0})
		if !ok {
			t.Fatalf("note %d release not classified", c.note)
		}
		b = msg.(ButtonMsg)
		if b.Pressed {
			t.Fatalf("note %d release: expected Pressed=false", c.note)
		}
	}
}

func TestClassifyGlobalButtons(t *testing.T) {
	cases := []struct {
		note   byte
		action GlobalAction
	}{
		{46, ActionSeekBack},
		{47, ActionSeekForward},
		{91, ActionPrevious},
		{92, ActionNext},
		{93, ActionPause},
		{94, ActionPlay},
		{95, ActionRecord},
		{96, ActionUp},
		{97, ActionDown},
		{98, ActionLeft},
		{99, ActionRight},
	}

	for _, c := range cases {
		msg, ok := Classify([3]byte{0x90, c.note, 127})
		if !ok {
			t.Fatalf("note %d not classified", c.note)
		}
		g, ok := msg.(GlobalMsg)
		if !ok || g.Action != c.action || !g.Pressed {
			t.Fatalf("note %d: got %+v want action=%d pressed=true", c.note, msg, c.action)
		}
	}
}

func TestClassifyUnknown(t *testing.T) {
	// Unmapped CC, unmapped NoteOn note, random status
	for _, raw := range [][3]byte{
		{0xb0, 8, 64},  // CC 8, not in knob range
		{0x90, 50, 64}, // note 50, not a mapped button
		{0x80, 0, 0},   // NoteOff status
	} {
		_, ok := Classify(raw)
		if ok {
			t.Fatalf("expected no classification for %v", raw)
		}
	}
}
