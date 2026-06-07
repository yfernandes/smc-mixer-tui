package main

import (
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

func clearStaleBindings(disp *dispatcher.Dispatcher, ss []streams.EnrichedStream) {
	snap := disp.Snapshot()
	for ch, c := range snap {
		if c.StreamID != nil && !c.ManuallyUnbound && !streamLive(*c.StreamID, ss) {
			disp.LoseBinding(ch)
		}
	}
}

func refreshBindingMetadata(disp *dispatcher.Dispatcher, ss []streams.EnrichedStream) {
	snap := disp.Snapshot()
	byID := streamsByID(ss)
	for ch, c := range snap {
		if c.StreamID == nil {
			continue
		}
		s, ok := byID[*c.StreamID]
		if !ok {
			continue
		}
		disp.UpdateBindingMetadata(ch, s.ID, s.Name, mprisName(s))
	}
}

func streamsByID(ss []streams.EnrichedStream) map[uint32]streams.EnrichedStream {
	byID := make(map[uint32]streams.EnrichedStream, len(ss))
	for _, s := range ss {
		byID[s.ID] = s
	}
	return byID
}

func streamLive(id uint32, ss []streams.EnrichedStream) bool {
	for _, s := range ss {
		if s.ID == id {
			return true
		}
	}
	return false
}

func mprisName(s streams.EnrichedStream) string {
	return s.MPRISPlayer
}
