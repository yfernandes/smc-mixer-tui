package main

import (
	"regexp"
	"strings"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

func applyBindings(cfg *config.Config, disp *dispatcher.Dispatcher, ss []streams.EnrichedStream) {
	for _, action := range planBindings(cfg, disp.Snapshot(), ss) {
		disp.Bind(action.ch, action.id, action.name, action.kind, action.mprisName)
	}
	refreshBindingMetadata(disp, ss)
}

type bindingAction struct {
	ch        int
	id        uint32
	name      string
	kind      audio.NodeKind
	mprisName string
}

func planBindings(cfg *config.Config, snap [8]dispatcher.Channel, ss []streams.EnrichedStream) []bindingAction {
	var actions []bindingAction
	for ch := range 8 {
		chCfg := cfg.ChannelFor(ch)
		if chCfg == nil {
			continue
		}
		if channelBindingLive(snap[ch], ss) {
			continue
		}
		matcher := newStreamMatcher(ch, cfg, chCfg.Bind)
		if s := bindingCandidate(matcher, ss); s != nil {
			actions = append(actions, bindingAction{
				ch:        ch,
				id:        s.ID,
				name:      s.Name,
				kind:      s.Kind,
				mprisName: mprisName(*s),
			})
		}
	}
	return actions
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

func mprisName(s streams.EnrichedStream) string {
	if s.Source == streams.SourceMPRIS {
		return s.Name
	}
	return ""
}

type streamMatcher struct {
	wantKind     audio.NodeKind
	hasWantKind  bool
	resolvedName string
	regexSet     bool
	re           *regexp.Regexp
}

func newStreamMatcher(ch int, cfg *config.Config, bind config.BindConfig) streamMatcher {
	m := streamMatcher{resolvedName: cfg.MatchStringFor(ch)}
	if wantKind, ok := bind.AudioKind(); ok {
		m.wantKind = wantKind
		m.hasWantKind = true
	}
	if bind.MatchRegex != "" {
		m.regexSet = true
		if re, err := regexp.Compile("(?i)" + bind.MatchRegex); err == nil {
			m.re = re
		}
	}
	return m
}

func (m streamMatcher) matches(s streams.EnrichedStream) bool {
	if m.hasWantKind && s.Kind != m.wantKind {
		return false
	}
	if m.regexSet {
		if m.re == nil {
			return false
		}
		return m.re.MatchString(s.Name) || m.re.MatchString(s.BindKey)
	}
	if m.resolvedName != "" {
		lower := strings.ToLower(m.resolvedName)
		return strings.Contains(strings.ToLower(s.Name), lower) ||
			strings.Contains(strings.ToLower(s.BindKey), lower)
	}
	return false
}

func streamLive(id uint32, ss []streams.EnrichedStream) bool {
	for _, s := range ss {
		if s.ID == id {
			return true
		}
	}
	return false
}

func refreshBindingMetadata(disp *dispatcher.Dispatcher, ss []streams.EnrichedStream) {
	snap := disp.Snapshot()
	byID := make(map[uint32]streams.EnrichedStream, len(ss))
	for _, s := range ss {
		byID[s.ID] = s
	}
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
