package dispatcher

import (
	"context"

	"github.com/yfernandes/smc-mixer-tui/midi"
)

type muteUpdate struct {
	ch    int
	id    uint32
	bound bool
	mute  bool
}

type buttonEffects struct {
	ledState        bool
	chBound         bool
	chID            uint32
	chMuteEffective bool
	chMPRIS         string
	soloUpdates     []muteUpdate
	soloLEDs        []buttonLED

	advancedToggled   bool
	advancedNowOn     bool
	advancedOldCancel context.CancelFunc
	advancedBCtx      context.Context
	advancedGen       uint32
	chRec             bool
	chPinned          bool
	isAdvancedAction  bool
	advancedAction    string
}

type buttonLED struct {
	ch     int
	kind   midi.ButtonKind
	active bool
}
