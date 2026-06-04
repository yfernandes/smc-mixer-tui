package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/yfernandes/smc-mixer-tui/midi"
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
			m.clampBindScroll()
		}
	case "down":
		if m.bindMode && len(m.enriched) > 0 {
			m.bindCursor = (m.bindCursor + 1) % len(m.enriched)
			m.clampBindScroll()
		}

	case "enter":
		if m.bindMode {
			if len(m.enriched) > 0 {
				s := m.enriched[m.bindCursor]
				m.disp.Bind(m.selected, s.ID, s.Name, s.Kind, s.MPRISPlayer)
			}
			m.bindMode = false
		} else {
			m.bindMode = true
			m.bindCursor = 0
			m.bindScroll = 0
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

// PageChangedMsg is emitted when the active page changes via a transport button.
type PageChangedMsg struct {
	Page string
}

func (m Model) handleGlobal(msg midi.GlobalMsg) (Model, tea.Cmd) {
	if !msg.Pressed {
		return m, nil
	}
	page, ok := globalActionToPage(msg.Action)
	if !ok {
		return m, nil
	}
	if m.ActivePage == page {
		m.ActivePage = "main"
	} else {
		m.ActivePage = page
	}
	m.ChannelAdvanced = [8]bool{}
	p := m.ActivePage
	return m, func() tea.Msg { return PageChangedMsg{Page: p} }
}

func globalActionToPage(a midi.GlobalAction) (string, bool) {
	switch a {
	case midi.ActionPlay:
		return "applications", true
	case midi.ActionRecord:
		return "inputs", true
	case midi.ActionPause:
		return "outputs", true
	case midi.ActionPrevious:
		return "system", true
	case midi.ActionNext:
		return "custom", true
	}
	return "", false
}
