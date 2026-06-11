package dispatcher

import (
	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/midi"
)

type streamBinding struct {
	id        uint32
	name      string
	kind      audio.NodeKind
	mprisName string
}

type faderUpdate struct {
	bound    bool
	streamID uint32
	synced   bool
}

func newChannel() Channel {
	return Channel{
		LastSetVol: -1,
		Knob:       64,
	}
}

func (c *Channel) bind(b streamBinding) {
	id := b.id
	c.StreamID = &id
	c.Name = b.name
	c.Kind = b.kind
	c.MPRISName = b.mprisName
	c.ActualVolume = 0
	c.LastSetVol = -1
	c.Synced = false
	// Reset soft pickup state: pickupSide=0 causes the next moveFader call to
	// re-evaluate direction relative to the (possibly updated) ActualVolume.
	// prevFaderPos retains the hardware position so the first crossing check is correct.
	c.pickupSide = 0
	c.prevFaderPos = c.FaderPos
	c.Stop = false
	c.UserBound = false
	c.BoundPID = 0 // cleared by config-driven binds; UserBind sets it after calling bind()
}

func (c *Channel) clearBinding() {
	c.StreamID = nil
	c.Name = ""
	c.MPRISName = ""
	c.Synced = false
	// Reset soft pickup direction tracking; it will be re-evaluated on the next fader
	// message based on ActualVolume at that time.
	c.pickupSide = 0
	c.ActualVolume = 0
	c.LastSetVol = -1
	c.Stop = false
	c.UserBound = false
	// BoundPID intentionally preserved: if the stream died, the next stream from the
	// same process should be reattached automatically (planChannelBinding checks this).
}

func (c Channel) boundID() (uint32, bool) {
	if c.StreamID == nil {
		return 0, false
	}
	return *c.StreamID, true
}

func (c Channel) boundTo(id uint32) bool {
	got, ok := c.boundID()
	return ok && got == id
}

func (c Channel) effectiveMute() bool {
	return c.Mute || c.SoloMuted
}

func (c *Channel) toggleButton(kind midi.ButtonKind) bool {
	switch kind {
	case midi.ButtonMute:
		c.Mute = !c.Mute
		return c.effectiveMute()
	case midi.ButtonSolo:
		c.Solo = !c.Solo
		return c.Solo
	case midi.ButtonRec:
		c.Rec = !c.Rec
		return c.Rec
	case midi.ButtonStop:
		c.Stop = !c.Stop
		return c.Stop
	default:
		return false
	}
}

func (c *Channel) moveFader(pos float64) faderUpdate {
	if c.SyncMode == SyncModeSoftPickup {
		c.moveSoftPickup(pos)
	} else {
		c.moveZero(pos)
	}
	return c.faderUpdate()
}

// moveZero implements drive-to-zero sync: the fader must reach 0 before it controls
// volume. This prevents accidental volume blasts on rebind.
//
// Invariant: Synced transitions false→true when pos < PickupThreshold; never transitions
// true→false here (only bind/clearBinding reset it).
func (c *Channel) moveZero(pos float64) {
	c.FaderPos = pos
	if !c.Synced && pos < PickupThreshold {
		c.Synced = true
	}
	if c.Synced {
		c.LastSetVol = pos
	}
}

// moveSoftPickup implements crossing-based sync: sync is established once the hardware
// fader crosses the current PipeWire volume (ActualVolume) within the tolerance window,
// approaching from the side it was on when the channel became unsynced.
//
// Invariants:
//   - pickupSide is 0 (unset) on bind/clearBinding and when ActualVolume changes while
//     unsynced. It is set to -1 (below) or +1 (above) on the first fader message, or
//     immediately triggers sync if the fader is already within the tolerance window.
//   - prevFaderPos holds the fader position from the previous call; used for crossing
//     detection and fast-sweep-overshoot handling.
//   - Once Synced=true, only LastSetVol is updated; Synced never reverts to false here.
func (c *Channel) moveSoftPickup(pos float64) {
	prev := c.prevFaderPos
	c.prevFaderPos = pos
	c.FaderPos = pos

	if c.Synced {
		c.LastSetVol = pos
		return
	}

	target := c.ActualVolume
	tol := c.pickupTol
	if tol <= 0 {
		tol = PickupThreshold
	}

	const sideBelow, sideAbove int8 = -1, 1

	if c.pickupSide == 0 {
		// First fader message after unsync (or after target changed). Determine which
		// side the fader is on and whether it's already within the tolerance window.
		switch {
		case pos < target-tol:
			c.pickupSide = sideBelow
		case pos > target+tol:
			c.pickupSide = sideAbove
		default:
			// Fader is already within the window: sync immediately.
			c.Synced = true
			c.LastSetVol = pos
			return
		}
	}

	// Crossing detection: require the fader to have been clearly on pickupSide (outside
	// the tolerance window) and now be at or inside the near edge of that window.
	// Fast sweeps that overshoot the target are also caught: if prev was outside the
	// window and pos jumped to the other side, pos has crossed through the window.
	switch c.pickupSide {
	case sideBelow:
		// Approaching from below: prev must have been below target-tol; pos must have
		// entered or passed the window (pos >= target-tol covers both normal and overshoot).
		if prev < target-tol && pos >= target-tol {
			c.Synced = true
			c.LastSetVol = pos
		}
	case sideAbove:
		// Approaching from above: symmetric.
		if prev > target+tol && pos <= target+tol {
			c.Synced = true
			c.LastSetVol = pos
		}
	}
}

func (c Channel) faderUpdate() faderUpdate {
	id, bound := c.boundID()
	return faderUpdate{
		bound:    bound,
		streamID: id,
		synced:   c.Synced,
	}
}
