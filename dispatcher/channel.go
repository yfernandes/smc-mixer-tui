package dispatcher

import (
	"math"

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
	bound             bool
	streamID          uint32
	synced            bool
	justDesynced      bool
	desyncFaderTarget float64
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
}

func (c *Channel) clearBinding() {
	c.StreamID = nil
	c.Name = ""
	c.MPRISName = ""
	c.Synced = false
	c.ActualVolume = 0
	c.LastSetVol = -1
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

func (c *Channel) updateActualVolume(vol float64) faderUpdate {
	c.ActualVolume = vol
	wasSynced := c.Synced
	if c.Synced && math.Abs(vol-c.LastSetVol) > PickupThreshold {
		c.Synced = false
	}

	u := c.faderUpdate()
	u.justDesynced = wasSynced && !c.Synced
	if u.justDesynced {
		u.desyncFaderTarget = min(vol, syncFaderCap)
	}
	return u
}

func (c *Channel) moveFader(pos float64) faderUpdate {
	c.FaderPos = pos
	if !c.Synced && math.Abs(pos-c.ActualVolume) < PickupThreshold {
		c.Synced = true
	}
	if c.Synced {
		c.LastSetVol = pos
	}
	return c.faderUpdate()
}

func (c Channel) faderUpdate() faderUpdate {
	id, bound := c.boundID()
	return faderUpdate{
		bound:    bound,
		streamID: id,
		synced:   c.Synced,
	}
}
