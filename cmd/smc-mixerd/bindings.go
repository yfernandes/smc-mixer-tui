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
	snap := disp.Snapshot()
	for ch := range 8 {
		chCfg := cfg.ChannelFor(ch)
		if chCfg == nil {
			continue
		}
		if channelBindingLive(snap[ch], ss) {
			continue
		}
		if s := bindingCandidate(ch, cfg, chCfg.Bind, ss); s != nil {
			disp.Bind(ch, s.ID, s.Name, s.Kind, mprisName(*s))
		}
	}
}

func channelBindingLive(ch dispatcher.Channel, ss []streams.EnrichedStream) bool {
	return ch.StreamID != nil && streamLive(*ch.StreamID, ss)
}

func bindingCandidate(ch int, cfg *config.Config, bind config.BindConfig, ss []streams.EnrichedStream) *streams.EnrichedStream {
	matchStr := cfg.MatchStringFor(ch)
	for i := range ss {
		if streamMatchesBind(ss[i], bind, matchStr) {
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

func streamMatchesBind(s streams.EnrichedStream, bind config.BindConfig, resolvedMatch string) bool {
	switch bind.Type {
	case "input":
		if s.Kind != audio.KindMic {
			return false
		}
	case "playback":
		if s.Kind != audio.KindSource {
			return false
		}
	case "output":
		if s.Kind != audio.KindSink {
			return false
		}
	}
	if bind.MatchRegex != "" {
		re, err := regexp.Compile("(?i)" + bind.MatchRegex)
		if err == nil && (re.MatchString(s.Name) || re.MatchString(s.BindKey)) {
			return true
		}
		return false
	}
	if resolvedMatch != "" {
		lower := strings.ToLower(resolvedMatch)
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
