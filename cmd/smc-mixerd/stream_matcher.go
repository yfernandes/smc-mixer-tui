package main

import (
	"regexp"
	"strings"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

type streamMatcher struct {
	wantKind    audio.NodeKind
	hasWantKind bool
	strategies  []func(streams.EnrichedStream) bool
}

// newStreamMatcher builds a matcher for a device.
// Strategies are mutually exclusive and applied in priority order: regex > match-title > substring.
func newStreamMatcher(dev *config.DeviceConfig) streamMatcher {
	m := streamMatcher{}
	if wantKind, ok := dev.AudioKind(); ok {
		m.wantKind = wantKind
		m.hasWantKind = true
	}

	switch {
	case dev.MatchRegex != "":
		m.strategies = append(m.strategies, regexMatchStrategy(dev.MatchRegex))
	case dev.MatchTitle != "":
		m.strategies = append(m.strategies, titleMatchStrategy(dev.MatchTitle))
	case dev.Match != "":
		m.strategies = append(m.strategies, substringMatchStrategy(dev.Match))
	}
	return m
}

func (m streamMatcher) matches(s streams.EnrichedStream) bool {
	if m.hasWantKind && s.Kind != m.wantKind {
		return false
	}
	for _, fn := range m.strategies {
		if fn(s) {
			return true
		}
	}
	return false
}

func regexMatchStrategy(pattern string) func(streams.EnrichedStream) bool {
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return func(streams.EnrichedStream) bool { return false }
	}
	return func(s streams.EnrichedStream) bool {
		return re.MatchString(s.Name) || re.MatchString(s.BindKey)
	}
}

func titleMatchStrategy(match string) func(streams.EnrichedStream) bool {
	needle := strings.ToLower(match)
	return func(s streams.EnrichedStream) bool {
		return strings.Contains(strings.ToLower(s.WinTitle), needle)
	}
}

func substringMatchStrategy(match string) func(streams.EnrichedStream) bool {
	needle := strings.ToLower(match)
	return func(s streams.EnrichedStream) bool {
		return strings.Contains(strings.ToLower(s.Name), needle) ||
			strings.Contains(strings.ToLower(s.BindKey), needle)
	}
}
