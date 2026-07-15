package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yfernandes/smc-mixer-tui/audio"
)

func (m Model) renderBar() string {
	if m.versionMismatch {
		return styleNoDevice.Render(" ⚠ version mismatch — TUI and daemon built from different commits; run 'make reinstall'")
	}
	if !m.deviceConnected {
		return styleNoDevice.Render(" ⚠ no MIDI device — waiting for SMC…   q quit")
	}
	names := [navSettingCount]string{"stream", "mute", "solo"}
	dim := globalBarStyle
	var settingParts [navSettingCount]string
	for i := 0; i < int(navSettingCount); i++ {
		if navSetting(i) == m.navSetting {
			settingParts[i] = bindBarStyle.Render(names[i])
		} else {
			settingParts[i] = dim.Render(names[i])
		}
	}
	page := m.ActivePage
	if m.routerPage.Active {
		page = fmt.Sprintf("router:%s %d/%d", m.routerPage.Name, min(m.routerPage.Offset+1, m.routerPage.Total), m.routerPage.Total)
	}
	return dim.Render(fmt.Sprintf(" page: %-18s   ←→ channel   ↑↓ ", page)) +
		settingParts[0] + dim.Render("/") + settingParts[1] + dim.Render("/") + settingParts[2] +
		dim.Render(fmt.Sprintf("   enter bind   u unbind   r reload(%d)   q quit", m.cfgReloads)) +
		m.renderRouterPageHint()
}

func (m Model) renderRouterPageHint() string {
	if !m.routerPage.Active || len(m.routerPage.Labels) == 0 {
		return ""
	}
	start := m.routerPage.Offset
	end := min(start+8, len(m.routerPage.Labels))
	var parts []string
	if start > 0 {
		parts = append(parts, "before: "+strings.Join(m.routerPage.Labels[max(0, start-2):start], ", "))
	}
	if end < len(m.routerPage.Labels) {
		parts = append(parts, "after: "+strings.Join(m.routerPage.Labels[end:min(len(m.routerPage.Labels), end+2)], ", "))
	}
	if len(parts) == 0 {
		return ""
	}
	return globalBarStyle.Render("   " + strings.Join(parts, "   "))
}

func selectStripStyle(selected bool, state channelState, kind audio.NodeKind) lipgloss.Style {
	switch {
	case state == stateBinding:
		return stripBindMode
	case state == stateUnbound || state == stateInactive:
		if selected {
			return stripSelectedUnbound
		}
		return stripUnbound
	case selected:
		switch kind {
		case audio.KindMic:
			return stripMicSelected
		case audio.KindSink:
			return stripSinkSelected
		default:
			return stripSrcSelected
		}
	default:
		switch kind {
		case audio.KindMic:
			return stripMic
		case audio.KindSink:
			return stripSinkBound
		default:
			return stripSrcBound
		}
	}
}
