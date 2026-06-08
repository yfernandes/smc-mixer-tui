package main

import (
	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

type bindingAction struct {
	ch        int
	id        uint32
	name      string
	kind      audio.NodeKind
	mprisName string
	lose      bool // if true: lose binding only, all other fields ignored
	syncSpec  bool // if true: sync advancedSpec for an unchanged live binding
}

func planBindings(cfg *config.Config, activePage string, snap [8]dispatcher.Channel, ss []streams.EnrichedStream) []bindingAction {
	var actions []bindingAction
	for ch := range 8 {
		action, ok := planChannelBinding(cfg, activePage, ch, snap[ch], ss)
		if ok {
			actions = append(actions, action)
		}
	}
	return actions
}

func planChannelBinding(cfg *config.Config, activePage string, ch int, current dispatcher.Channel, ss []streams.EnrichedStream) (bindingAction, bool) {
	live := channelBindingLive(current, ss)
	if shouldPreserveBinding(activePage, current, live) {
		return bindingAction{}, false
	}

	dev := cfg.ChannelForPage(activePage, ch)
	if dev == nil {
		if live {
			return bindingAction{ch: ch, lose: true}, true
		}
		return bindingAction{}, false
	}

	if channelBindingMatchesConfig(current, dev, ss) {
		return bindingAction{ch: ch, syncSpec: true}, true
	}
	if current.UserBound && live {
		return bindingAction{}, false
	}

	matcher := newStreamMatcher(dev)
	if s := bindingCandidate(matcher, ss); s != nil {
		return bindingAction{
			ch:        ch,
			id:        s.ID,
			name:      s.Name,
			kind:      s.Kind,
			mprisName: mprisName(*s),
		}, true
	}
	// No replacement found. Preserve any live binding to handle transient enrichment
	// gaps on the same page (e.g. MPRIS identity briefly reverts after stream routing
	// changes). Cross-page stale bindings are cleared before planBindings is called
	// on a page switch, so a live-but-unmatched binding here is always same-page.
	return bindingAction{}, false
}

func shouldPreserveBinding(activePage string, current dispatcher.Channel, live bool) bool {
	if current.ManuallyUnbound {
		return true
	}
	return activePage == "main" && current.Pinned && live
}

// channelBindingMatchesConfig reports whether ch's live binding is for a stream
// that matches the device config. Used to avoid redundant rebinds on page switch.
func channelBindingMatchesConfig(ch dispatcher.Channel, dev *config.DeviceConfig, ss []streams.EnrichedStream) bool {
	if ch.StreamID == nil {
		return false
	}
	for _, s := range ss {
		if s.ID == *ch.StreamID {
			return newStreamMatcher(dev).matches(s)
		}
	}
	return false
}

func channelBindingLive(ch dispatcher.Channel, ss []streams.EnrichedStream) bool {
	return ch.StreamID != nil && streamLive(*ch.StreamID, ss)
}

func bindingCandidate(matcher streamMatcher, ss []streams.EnrichedStream) *streams.EnrichedStream {
	for i := range ss {
		if matcher.matches(ss[i]) {
			return &ss[i]
		}
	}
	return nil
}
