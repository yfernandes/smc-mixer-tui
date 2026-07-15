package dispatcher

import "github.com/yfernandes/smc-mixer-tui/midi"

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
}

type buttonLED struct {
	ch     int
	kind   midi.ButtonKind
	active bool
}
