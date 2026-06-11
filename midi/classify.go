package midi

// Classify converts a raw 3-byte MIDI message to a typed Msg.
// Returns (nil, false) for messages that don't correspond to any mapped control.
func Classify(raw [3]byte) (Msg, bool) {
	status, data1, data2 := raw[0], raw[1], raw[2]

	switch {
	case status == 0x90:
		pressed := data2 == 127
		return classifyButton(data1, pressed)

	case status >= 0xe0 && status <= 0xe7:
		// Pitchbend faders: full 14-bit value from (MSB<<7)|LSB → 0–16383
		return FaderMsg{Channel: int(status & 0x07), Value: uint16(data2)<<7 | uint16(data1)}, true

	case status == 0xb0 && data1 >= 16 && data1 <= 23:
		// Relative knobs: 1 = increment, 65 = decrement
		delta := 1
		if data2 == 65 {
			delta = -1
		}
		return KnobMsg{Channel: int(data1 - 16), Delta: delta}, true
	}

	return nil, false
}

func classifyButton(note byte, pressed bool) (Msg, bool) {
	switch {
	case note < 8:
		return ButtonMsg{Channel: int(note), Kind: ButtonRec, Pressed: pressed}, true
	case note < 16:
		return ButtonMsg{Channel: int(note - 8), Kind: ButtonSolo, Pressed: pressed}, true
	case note < 24:
		return ButtonMsg{Channel: int(note - 16), Kind: ButtonMute, Pressed: pressed}, true
	case note < 32:
		return ButtonMsg{Channel: int(note - 24), Kind: ButtonStop, Pressed: pressed}, true

	// Global transport buttons (MCU note numbers)
	case note == 46:
		return GlobalMsg{Action: ActionSeekBack, Pressed: pressed}, true
	case note == 47:
		return GlobalMsg{Action: ActionSeekForward, Pressed: pressed}, true
	case note == 91:
		return GlobalMsg{Action: ActionPrevious, Pressed: pressed}, true
	case note == 92:
		return GlobalMsg{Action: ActionNext, Pressed: pressed}, true
	case note == 93:
		return GlobalMsg{Action: ActionPause, Pressed: pressed}, true
	case note == 94:
		return GlobalMsg{Action: ActionPlay, Pressed: pressed}, true
	case note == 95:
		return GlobalMsg{Action: ActionRecord, Pressed: pressed}, true
	case note == 96:
		return GlobalMsg{Action: ActionUp, Pressed: pressed}, true
	case note == 97:
		return GlobalMsg{Action: ActionDown, Pressed: pressed}, true
	case note == 98:
		return GlobalMsg{Action: ActionLeft, Pressed: pressed}, true
	case note == 99:
		return GlobalMsg{Action: ActionRight, Pressed: pressed}, true
	}

	return nil, false
}
