package pwbackend

import (
	"regexp"
	"strings"

	"github.com/yfernandes/smc-mixer-tui/streams"
)

func resolveRule(rule *ruleState, ss []streams.EnrichedStream) *streams.EnrichedStream {
	if rule.current != nil {
		var stuck *streams.EnrichedStream
		for i := range ss {
			if ss[i].ID == rule.current.ID {
				stuck = &ss[i]
				break
			}
		}
		if stuck != nil {
			if stuck.Active {
				return stuck
			}
			// The previously-bound node is still present but has gone idle
			// (e.g. a backgrounded browser tab). Don't stay stuck on it if a
			// different matching stream is now the one actually producing audio.
			for i := range ss {
				if ss[i].ID != stuck.ID && ss[i].Active && matches(rule, ss[i]) {
					return &ss[i]
				}
			}
			return stuck
		}
	}
	if rule.boundPID != 0 {
		var fallback *streams.EnrichedStream
		for i := range ss {
			if ss[i].PID != rule.boundPID {
				continue
			}
			if rule.boundMediaName != "" && ss[i].MediaName == rule.boundMediaName {
				return &ss[i]
			}
			if fallback == nil {
				fallback = &ss[i]
			}
		}
		if fallback != nil {
			return fallback
		}
	}
	for i := range ss {
		if ss[i].Active && matches(rule, ss[i]) {
			return &ss[i]
		}
	}
	for i := range ss {
		if matches(rule, ss[i]) {
			return &ss[i]
		}
	}
	return nil
}

func matches(rule *ruleState, s streams.EnrichedStream) bool {
	if kind, ok := rule.device.AudioKind(); ok && s.Kind != kind {
		return false
	}
	switch {
	case rule.device.MatchRegex != "":
		re, err := regexp.Compile("(?i)" + rule.device.MatchRegex)
		return err == nil && (re.MatchString(s.Name) || re.MatchString(s.BindKey))
	case rule.device.MatchTitle != "":
		return strings.Contains(strings.ToLower(s.WinTitle), strings.ToLower(rule.device.MatchTitle))
	default:
		needle := strings.ToLower(rule.device.Match)
		return needle != "" && (strings.Contains(strings.ToLower(s.Name), needle) || strings.Contains(strings.ToLower(s.BindKey), needle))
	}
}
