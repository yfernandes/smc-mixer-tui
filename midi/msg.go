package midi

// ButtonKind identifies which button on a channel strip was pressed.
type ButtonKind uint8

const (
	ButtonRec  ButtonKind = iota // note 0–7
	ButtonSolo                   // note 8–15
	ButtonMute                   // note 16–23
	ButtonStop                   // note 24–31
)

// GlobalAction identifies a transport button.
type GlobalAction uint8

const (
	ActionPlay GlobalAction = iota
	ActionPause
	ActionRecord
	ActionPrevious
	ActionNext
	ActionSeekBack
	ActionSeekForward
	ActionUp
	ActionDown
	ActionLeft
	ActionRight
)

// Msg is the sum type for all classified MIDI events.
type Msg interface{ midiMsg() }

// ButtonMsg is emitted for channel strip buttons (rec/solo/mute/stop).
type ButtonMsg struct {
	Channel int        // 0–7
	Kind    ButtonKind
	Pressed bool
}

// GlobalMsg is emitted for transport buttons.
type GlobalMsg struct {
	Action  GlobalAction
	Pressed bool
}

// FaderMsg is emitted for pitchbend faders (0xe0–0xe7).
// Value is the full 14-bit pitch-bend position: (MSB<<7)|LSB, range 0–16383.
type FaderMsg struct {
	Channel int    // 0–7
	Value   uint16 // 0–16383
}

// KnobMsg is emitted for relative CC knobs (CC 16–23).
type KnobMsg struct {
	Channel int // 0–7
	Delta   int // +1 or -1
}

func (ButtonMsg) midiMsg() {}
func (GlobalMsg) midiMsg()  {}
func (FaderMsg) midiMsg()   {}
func (KnobMsg) midiMsg()    {}

// DeviceStatusMsg is sent to the UI when the MIDI device connects or disconnects.
// It is not routed through the MIDI pipeline — main sends it directly via program.Send.
type DeviceStatusMsg struct {
	Connected bool
	Device    string // set when Connected == true
}
