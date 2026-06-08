package ui

import (
	"fmt"

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
	return dim.Render(fmt.Sprintf(" page: %-12s   ←→ channel   ↑↓ ", m.ActivePage)) +
		settingParts[0] + dim.Render("/") + settingParts[1] + dim.Render("/") + settingParts[2] +
		dim.Render(fmt.Sprintf("   enter bind   u unbind   r reload(%d)   q quit", m.cfgReloads))
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
