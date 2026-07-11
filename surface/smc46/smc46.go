package smc46

import (
	"context"

	"github.com/yfernandes/smc-mixer-tui/midi"
	"github.com/yfernandes/smc-mixer-tui/surface"
)

const MaxFaderValue = 16383.0

// Surface adapts the existing MIDI listener/writer to the generic surface
// interface. Phase 1 still uses the daemon's legacy MIDI loop directly; this
// type exists so later phases can move device ownership behind the interface.
type Surface struct {
	Listener *midi.Listener
	Writer   *midi.Writer
}

func (s Surface) Descriptor() surface.Descriptor {
	return Descriptor()
}

func (s Surface) Run(ctx context.Context, events chan<- surface.Event) error {
	if s.Listener == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	msgs := make(chan midi.Msg, 256)
	errCh := make(chan error, 1)
	go func() {
		defer close(msgs)
		errCh <- s.Listener.Run(ctx, msgs)
	}()
	for msg := range msgs {
		if ev, ok := EventFromMsg(msg); ok {
			select {
			case events <- ev:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return <-errCh
}

func (s Surface) Feedback() surface.FeedbackWriter {
	return Feedback{Writer: s.Writer}
}

// Descriptor returns the SMC46 surface shape. This is the data source for the
// surface's eight strips during the router migration.
func Descriptor() surface.Descriptor {
	return surface.Descriptor{
		Name:   "SMC46",
		Strips: 8,
		Controls: []surface.ControlSpec{
			{Role: surface.RoleFader, Kind: surface.ControlAbsolute, Bits: 14},
			{Role: surface.RoleKnob, Kind: surface.ControlRelative},
			{Role: surface.RoleRec, Kind: surface.ControlMomentary},
			{Role: surface.RoleSolo, Kind: surface.ControlMomentary},
			{Role: surface.RoleMute, Kind: surface.ControlMomentary},
			{Role: surface.RoleStop, Kind: surface.ControlMomentary},
		},
		Globals: []surface.ControlSpec{
			{Role: RoleFromGlobal(midi.ActionPlay), Kind: surface.ControlMomentary},
			{Role: RoleFromGlobal(midi.ActionPause), Kind: surface.ControlMomentary},
			{Role: RoleFromGlobal(midi.ActionRecord), Kind: surface.ControlMomentary},
			{Role: RoleFromGlobal(midi.ActionPrevious), Kind: surface.ControlMomentary},
			{Role: RoleFromGlobal(midi.ActionNext), Kind: surface.ControlMomentary},
			{Role: RoleFromGlobal(midi.ActionSeekBack), Kind: surface.ControlMomentary},
			{Role: RoleFromGlobal(midi.ActionSeekForward), Kind: surface.ControlMomentary},
			{Role: RoleFromGlobal(midi.ActionUp), Kind: surface.ControlMomentary},
			{Role: RoleFromGlobal(midi.ActionDown), Kind: surface.ControlMomentary},
			{Role: RoleFromGlobal(midi.ActionLeft), Kind: surface.ControlMomentary},
			{Role: RoleFromGlobal(midi.ActionRight), Kind: surface.ControlMomentary},
		},
	}
}

// EventFromMsg translates the existing MIDI sum type into a surface event.
func EventFromMsg(msg midi.Msg) (surface.Event, bool) {
	switch m := msg.(type) {
	case midi.FaderMsg:
		return surface.Event{Strip: m.Channel, Role: surface.RoleFader, Value: float64(m.Value) / MaxFaderValue}, true
	case midi.KnobMsg:
		return surface.Event{Strip: m.Channel, Role: surface.RoleKnob, Delta: m.Delta}, true
	case midi.ButtonMsg:
		return surface.Event{Strip: m.Channel, Role: roleFromButton(m.Kind), Pressed: m.Pressed}, true
	case midi.GlobalMsg:
		return surface.Event{Strip: -1, Role: RoleFromGlobal(m.Action), Pressed: m.Pressed}, true
	default:
		return surface.Event{}, false
	}
}

func roleFromButton(kind midi.ButtonKind) surface.Role {
	switch kind {
	case midi.ButtonRec:
		return surface.RoleRec
	case midi.ButtonSolo:
		return surface.RoleSolo
	case midi.ButtonMute:
		return surface.RoleMute
	case midi.ButtonStop:
		return surface.RoleStop
	default:
		return ""
	}
}

// RoleFromGlobal maps SMC46 transport controls to stable role names.
func RoleFromGlobal(action midi.GlobalAction) surface.Role {
	switch action {
	case midi.ActionPlay:
		return "play"
	case midi.ActionPause:
		return "pause"
	case midi.ActionRecord:
		return "record"
	case midi.ActionPrevious:
		return "prev"
	case midi.ActionNext:
		return "next"
	case midi.ActionSeekBack:
		return "seek-back"
	case midi.ActionSeekForward:
		return "seek-forward"
	case midi.ActionUp:
		return "up"
	case midi.ActionDown:
		return "down"
	case midi.ActionLeft:
		return "left"
	case midi.ActionRight:
		return "right"
	default:
		return ""
	}
}

// Feedback adapts midi.Writer to surface.FeedbackWriter.
type Feedback struct {
	Writer *midi.Writer
}

func (f Feedback) SetLED(strip int, role surface.Role, on bool) {
	if f.Writer == nil {
		return
	}
	kind, ok := buttonFromRole(role)
	if !ok {
		return
	}
	f.Writer.SetButtonLED(strip, kind, on)
}

func (f Feedback) SetPosition(strip int, role surface.Role, v float64) {
	if f.Writer == nil || role != surface.RoleFader {
		return
	}
	f.Writer.SetFaderPosition(strip, v)
}

func (f Feedback) SetGlobalLED(role surface.Role, on bool) {
	if f.Writer == nil {
		return
	}
	action, ok := globalFromRole(role)
	if !ok {
		return
	}
	f.Writer.SetGlobalLED(action, on)
}

func buttonFromRole(role surface.Role) (midi.ButtonKind, bool) {
	switch role {
	case surface.RoleRec:
		return midi.ButtonRec, true
	case surface.RoleSolo:
		return midi.ButtonSolo, true
	case surface.RoleMute:
		return midi.ButtonMute, true
	case surface.RoleStop:
		return midi.ButtonStop, true
	default:
		return 0, false
	}
}

func globalFromRole(role surface.Role) (midi.GlobalAction, bool) {
	switch role {
	case "play":
		return midi.ActionPlay, true
	case "pause":
		return midi.ActionPause, true
	case "record":
		return midi.ActionRecord, true
	case "prev":
		return midi.ActionPrevious, true
	case "next":
		return midi.ActionNext, true
	case "seek-back":
		return midi.ActionSeekBack, true
	case "seek-forward":
		return midi.ActionSeekForward, true
	case "up":
		return midi.ActionUp, true
	case "down":
		return midi.ActionDown, true
	case "left":
		return midi.ActionLeft, true
	case "right":
		return midi.ActionRight, true
	default:
		return 0, false
	}
}
