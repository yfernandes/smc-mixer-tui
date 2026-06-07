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
		if m.bindMode {
			avail := m.availableStreams()
			if len(avail) > 0 {
				m.bindCursor = (m.bindCursor + len(avail) - 1) % len(avail)
				m.clampBindScroll()
			}
		}
	case "down":
		if m.bindMode {
			avail := m.availableStreams()
			if len(avail) > 0 {
				m.bindCursor = (m.bindCursor + 1) % len(avail)
				m.clampBindScroll()
			}
		}

	case "enter":
		if m.bindMode {
			avail := m.availableStreams()
			if len(avail) > 0 && m.bindCursor < len(avail) {
				s := avail[m.bindCursor]
				m.disp.Bind(m.selected, s.ID, s.Name, s.Kind, s.MPRISPlayer)
			}
			m.bindMode = false
		} else {
			m.bindMode = true
			m.navStreamOpen = false
			m.bindCursor = 0
			m.bindScroll = 0
		}

	case "esc":
		m.bindMode = false
		m.navStreamOpen = false

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

	// Navigation buttons drive the TUI directly; they do not switch pages.
	switch msg.Action {
	case midi.ActionSeekBack:
		if !m.bindMode {
			m.selected = (m.selected + 7) % 8
			m.navStreamOpen = false
		}
		return m, nil
	case midi.ActionSeekForward:
		if !m.bindMode {
			m.selected = (m.selected + 1) % 8
			m.navStreamOpen = false
		}
		return m, nil
	case midi.ActionUp:
		if !m.bindMode {
			m.navSetting = (m.navSetting + navSettingCount - 1) % navSettingCount
			m.navStreamOpen = false
		}
		return m, nil
	case midi.ActionDown:
		if !m.bindMode {
			m.navSetting = (m.navSetting + 1) % navSettingCount
			m.navStreamOpen = false
		}
		return m, nil
	case midi.ActionLeft:
		if !m.bindMode {
			m.applyNavLeft()
		}
		return m, nil
	case midi.ActionRight:
		if !m.bindMode {
			m.applyNavRight()
		}
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
	m.navStreamOpen = false
	p := m.ActivePage
	return m, func() tea.Msg { return PageChangedMsg{Page: p} }
}

// applyNavLeft executes a "left" action on the currently focused navSetting.
func (m *Model) applyNavLeft() {
	switch m.navSetting {
	case navStream:
		m.navStreamOpen = true
		m.cycleStream(-1)
	case navMute:
		m.disp.ToggleMute(m.selected)
	case navSolo:
		m.disp.ToggleSolo(m.selected)
	}
}

// applyNavRight executes a "right" action on the currently focused navSetting.
func (m *Model) applyNavRight() {
	switch m.navSetting {
	case navStream:
		m.navStreamOpen = true
		m.cycleStream(+1)
	case navMute:
		m.disp.ToggleMute(m.selected)
	case navSolo:
		m.disp.ToggleSolo(m.selected)
	}
}

// cycleStream binds the next (dir=+1) or previous (dir=-1) available stream to the
// selected channel. Streams user-bound to other channels are excluded from the cycle.
func (m *Model) cycleStream(dir int) {
	avail := m.availableStreams()
	if len(avail) == 0 {
		return
	}
	cur := -1
	c := m.channels[m.selected]
	if c.StreamID != nil {
		for i, s := range avail {
			if s.ID == *c.StreamID {
				cur = i
				break
			}
		}
	}
	var next int
	switch {
	case cur < 0 && dir > 0:
		next = 0
	case cur < 0 && dir < 0:
		next = len(avail) - 1
	default:
		next = (cur + dir + len(avail)) % len(avail)
	}
	s := avail[next]
	m.disp.Bind(m.selected, s.ID, s.Name, s.Kind, s.MPRISPlayer)
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
