package ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/yfernandes/smc-mixer-tui/backend"
	"github.com/yfernandes/smc-mixer-tui/daemon"
	"github.com/yfernandes/smc-mixer-tui/surface"
)

func (m Model) renderGenericStrip(ch int, s daemon.StripWire) string {
	cols := newStripColumns()
	focusedNav := ch == m.selected && !m.bindMode
	fader := faderParam(s)
	knob := fader
	if p, ok := s.Params["crossfade"]; ok {
		knob = p
	}
	knobLabel := fmt.Sprintf("◎%3.0f%%", knob.Value*100)
	knobBar := faderBar(knob.Value, knobBarW+1)
	if _, ok := s.Params["crossfade"]; ok {
		var ext struct {
			SinkA string `json:"cross_sink_a_name"`
			SinkB string `json:"cross_sink_b_name"`
		}
		_ = json.Unmarshal(s.Ext, &ext)
		if ext.SinkA != "" && ext.SinkB != "" {
			knobLabel = truncate(crossfaderLabel(ext.SinkA, ext.SinkB), leftW+rightW)
		}
		knobBar = crossfadeBar(int(knob.Value*127), knobBarW+1)
	}

	fRows := dualFaderRows(fader.Value, fader.Value, true, fader.Readable, fader.Synced, unifiedFaderH, leftW)
	prefix := fmt.Sprintf("R%-2d · ", ch+1)
	header := prefix + subtypeRouterStyle.Render(truncate(s.Backend, leftW+rightW-len([]rune(prefix))))
	name := s.Label
	if name == "" {
		name = s.TargetID
	}
	// Style after sizing: truncate counts runes, so running it over an
	// ANSI-styled string slices escape sequences mid-way.
	syncLine := "live"
	if fader.Readable && !fader.Synced {
		syncLine = pickupArrowStyle.Render("pickup")
	}

	lines := []string{
		header,
		truncate(name, leftW+rightW),
		syncLine,
		knobLabel,
		knobBar,
		cols.row("", ""),
		cols.row(fRows[0], ""),
		cols.row(fRows[1], genericButton(s, surface.RoleMute, "M", focusedNav && m.navSetting == navMute)),
		cols.row(fRows[2], genericButton(s, surface.RoleSolo, "S", focusedNav && m.navSetting == navSolo)),
		cols.row(fRows[3], genericButton(s, surface.RoleRec, "R", false)),
		cols.row(fRows[4], genericButton(s, surface.RoleStop, "■", false)),
		cols.row(fRows[5], ""),
		cols.row(fRows[6], genericPickupLabel(fader)),
	}

	style := stripRouter
	if ch == m.selected {
		style = stripRouterSelect
	}
	return style.Render(strings.Join(lines, "\n"))
}

func genericParam(s daemon.StripWire, param string) daemon.ParamWire {
	if s.Params == nil {
		return daemon.ParamWire{}
	}
	return s.Params[param]
}

// faderParam picks the strip's primary continuous param deterministically.
// Map iteration order is randomized per call in Go, so any "first match wins"
// scan over s.Params renders a different param frame-to-frame when more than
// one continuous param exists — the strip visibly flickers at the tick rate.
// Preference order: the RoleFader-mapped param, then "volume" by convention,
// then readable continuous params, then any continuous param — ties broken by
// sorted param name.
func faderParam(s daemon.StripWire) daemon.ParamWire {
	if p, ok := s.Params[string(surface.RoleFader)]; ok {
		return p
	}
	if p, ok := s.Params["volume"]; ok && p.Kind == uint8(backend.ParamContinuous) {
		return p
	}
	keys := make([]string, 0, len(s.Params))
	for k := range s.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if p := s.Params[k]; p.Kind == uint8(backend.ParamContinuous) && p.Readable {
			return p
		}
	}
	for _, k := range keys {
		if p := s.Params[k]; p.Kind == uint8(backend.ParamContinuous) {
			return p
		}
	}
	return genericParam(s, "value")
}

func genericButton(s daemon.StripWire, role surface.Role, label string, focused bool) string {
	param, ok := genericToggleParamFor(s, string(role))
	if !ok {
		return renderBtnFocused(label, false, btnInactive, focused)
	}
	p := s.Params[param]
	style := btnInactive
	if p.Bool {
		switch role {
		case surface.RoleMute:
			style = btnMuteOn
		case surface.RoleSolo:
			style = btnSoloOn
		case surface.RoleRec:
			style = btnRecOn
		case surface.RoleStop:
			style = btnStopOn
		default:
			style = btnMuteOn
		}
	}
	return renderBtnFocused(label, p.Bool, style, focused)
}

func genericPickupLabel(p daemon.ParamWire) string {
	pct := fmt.Sprintf("%3.0f%%", p.Value*100)
	if p.Readable && !p.Synced {
		return pickupArrowStyle.Render(pct)
	}
	return pct
}
