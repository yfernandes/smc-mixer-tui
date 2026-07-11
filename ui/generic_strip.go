package ui

import (
	"fmt"
	"strings"

	"github.com/yfernandes/smc-mixer-tui/backend"
	"github.com/yfernandes/smc-mixer-tui/daemon"
	"github.com/yfernandes/smc-mixer-tui/surface"
)

func (m Model) renderGenericStrip(ch int, s daemon.StripWire) string {
	cols := newStripColumns()
	focusedNav := ch == m.selected && !m.bindMode
	fader := genericParam(s, "value")
	if p, ok := s.Params[string(surface.RoleFader)]; ok {
		fader = p
	}
	for _, p := range s.Params {
		if p.Kind == uint8(backend.ParamContinuous) {
			fader = p
			break
		}
	}

	fRows := dualFaderRows(fader.Value, fader.Value, true, fader.Readable, fader.Synced, unifiedFaderH, leftW)
	header := fmt.Sprintf("R%-2d · ", ch+1) + subtypeRouterStyle.Render(s.Backend)
	name := s.Label
	if name == "" {
		name = s.TargetID
	}
	syncLine := "live"
	if fader.Readable && !fader.Synced {
		syncLine = pickupArrowStyle.Render("pickup")
	}

	lines := []string{
		truncate(header, leftW+rightW),
		truncate(name, leftW+rightW),
		truncate(syncLine, leftW+rightW),
		fmt.Sprintf("◎%3.0f%%", fader.Value*100),
		faderBar(fader.Value, knobBarW+1),
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
