package surface

import "context"

// ControlKind describes how a physical control reports changes.
type ControlKind uint8

const (
	ControlAbsolute ControlKind = iota
	ControlRelative
	ControlMomentary
)

// Role names a physical control's logical role on a surface.
type Role string

// Common per-strip roles.
const (
	RoleFader Role = "fader"
	RoleKnob  Role = "knob"
	RoleRec   Role = "rec"
	RoleSolo  Role = "solo"
	RoleMute  Role = "mute"
	RoleStop  Role = "stop"
)

// ControlSpec describes one control in a surface descriptor.
type ControlSpec struct {
	Role Role
	Kind ControlKind
	Bits int
}

// Descriptor describes a control surface independently of a transport library.
type Descriptor struct {
	Name     string
	Strips   int
	Controls []ControlSpec
	Globals  []ControlSpec
}

// Event is a normalized input event from a surface.
type Event struct {
	Strip   int
	Role    Role
	Value   float64
	Delta   int
	Pressed bool
}

// FeedbackWriter writes feedback to the surface. Implementations may ignore
// unsupported operations.
type FeedbackWriter interface {
	SetLED(strip int, role Role, on bool)
	SetPosition(strip int, role Role, v float64)
	SetGlobalLED(role Role, on bool)
}

// Surface is the runtime interface for a connected control surface.
type Surface interface {
	Descriptor() Descriptor
	Run(ctx context.Context, events chan<- Event) error
	Feedback() FeedbackWriter
}
