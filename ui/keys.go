package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/yago/smc-mixer/midi"
)

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit

	case "left":
		if !m.bindMode {
			m.selected = (m.selected + 7) % 8
		}
	case "right":
		if !m.bindMode {
			m.selected = (m.selected + 1) % 8
		}

	case "up":
		if m.bindMode && len(m.enriched) > 0 {
			m.bindCursor = (m.bindCursor + len(m.enriched) - 1) % len(m.enriched)
		}
	case "down":
		if m.bindMode && len(m.enriched) > 0 {
			m.bindCursor = (m.bindCursor + 1) % len(m.enriched)
		}

	case "enter":
		if m.bindMode {
			if len(m.enriched) > 0 {
				s := m.enriched[m.bindCursor]
				m.disp.Bind(m.selected, s.ID, s.Name)
			}
			m.bindMode = false
		} else {
			m.bindMode = true
			m.bindCursor = 0
		}

	case "esc":
		m.bindMode = false

	case "u":
		if !m.bindMode {
			m.disp.Unbind(m.selected)
		}
	}
	return m, nil
}

func (m Model) handleGlobal(msg midi.GlobalMsg) Model {
	if !msg.Pressed {
		return m
	}
	switch msg.Action {
	case midi.ActionPlay:
		m.playing = true
	case midi.ActionPause:
		m.playing = !m.playing
	case midi.ActionRecord:
		m.recording = !m.recording
	}
	return m
}
