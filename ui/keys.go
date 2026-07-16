package ui

import (
	"encoding/json"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yfernandes/smc-mixer-tui/backend"
	"github.com/yfernandes/smc-mixer-tui/backend/pwbackend"
	"github.com/yfernandes/smc-mixer-tui/daemon"
	"github.com/yfernandes/smc-mixer-tui/midi"
	"github.com/yfernandes/smc-mixer-tui/surface"
)

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.routingOpen {
		return m.handleRoutingKeys(msg)
	}

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
		if _, ok := m.strips[m.selected]; ok && !m.bindMode {
			return m, nil
		}
		if m.bindMode {
			avail := m.availableStreams()
			if len(avail) > 0 && m.bindCursor < len(avail) {
				s := avail[m.bindCursor]
				m.disp.Bind(m.selected, s.ID, s.Name, s.Kind, s.MPRISPlayer, s.PID, s.MediaName)
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

	case "r":
		if !m.bindMode && m.reloadFn != nil {
			m.stripCfgs = m.reloadFn()
			m.cfgReloads++
		}

	case "tab":
		m.routingOpen = true
		m.disp.RequestBackendView(pwbackend.Name, "routing", nil)
		return m, routingTickCmd()
	}
	return m, nil
}

// handleRoutingKeys handles all input while the routing inspector is open,
// isolated from normal page/bind-mode handling. Up/Down move the branch
// cursor (or, with the destination picker open, the picker's cursor); Enter
// opens the picker on the selected branch, or confirms a pick; Tab/Esc close
// the picker first, then the routing view.
func (m Model) handleRoutingKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab", "esc":
		if m.routingPickerOpen {
			m.routingPickerOpen = false
			return m, nil
		}
		m.routingOpen = false

	case "up":
		if m.routingPickerOpen {
			if n := len(m.pickerCandidates()); n > 0 {
				m.routingPickerCursor = (m.routingPickerCursor + n - 1) % n
			}
		} else if n := len(m.retargetTargets()); n > 0 {
			m.routingCursor = (m.routingCursor + n - 1) % n
		}

	case "down":
		if m.routingPickerOpen {
			if n := len(m.pickerCandidates()); n > 0 {
				m.routingPickerCursor = (m.routingPickerCursor + 1) % n
			}
		} else if n := len(m.retargetTargets()); n > 0 {
			m.routingCursor = (m.routingCursor + 1) % n
		}

	case "enter":
		if m.routingPickerOpen {
			m.confirmRetarget()
			m.routingPickerOpen = false
		} else if len(m.retargetTargets()) > 0 {
			m.routingPickerOpen = true
			m.routingPickerCursor = 0
		}
	}
	return m, nil
}

// confirmRetarget sends the currently selected picker candidate as the new
// destination for the currently selected branch. No-op if either list is
// empty (defensive; shouldn't happen since Enter only opens the picker when
// retargetTargets() is non-empty, and closes it before candidates can change
// out from under the cursor).
func (m Model) confirmRetarget() {
	targets := m.retargetTargets()
	candidates := m.pickerCandidates()
	if m.routingCursor >= len(targets) || m.routingPickerCursor >= len(candidates) {
		return
	}
	t := targets[m.routingCursor]
	node := m.routing.Routes[t.NodeIdx]
	branch := node.Branches[t.BranchIdx]
	sink := candidates[m.routingPickerCursor]
	data, _ := json.Marshal(pwbackend.RetargetRequest{DeviceKey: node.DeviceKey, Branch: branch.Label, SinkNodeName: sink.NodeName, SinkDisplayName: sink.Name})
	m.disp.RequestBackendView(pwbackend.Name, "retarget", data)
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
	if m.toggleGenericNav() {
		return
	}
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
	if m.toggleGenericNav() {
		return
	}
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

func (m *Model) toggleGenericNav() bool {
	s, ok := m.strips[m.selected]
	if !ok {
		return false
	}
	var role string
	switch m.navSetting {
	case navMute:
		role = string(surface.RoleMute)
	case navSolo:
		role = string(surface.RoleSolo)
	default:
		return false
	}
	param, ok := genericToggleParamFor(s, role)
	if !ok {
		return true
	}
	m.disp.ToggleParam(s.TargetID, param)
	return true
}

func genericToggleParamFor(s daemon.StripWire, preferred string) (string, bool) {
	if p, ok := s.Params[preferred]; ok && p.Kind == uint8(backend.ParamToggle) {
		return preferred, true
	}
	// Deterministic fallback: map iteration order is randomized per call, so
	// picking "any toggle" straight off the map would flicker between params
	// on strips that expose more than one.
	params := make([]string, 0, len(s.Params))
	for param := range s.Params {
		params = append(params, param)
	}
	sort.Strings(params)
	for _, param := range params {
		if s.Params[param].Kind == uint8(backend.ParamToggle) {
			return param, true
		}
	}
	return "", false
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
	m.disp.Bind(m.selected, s.ID, s.Name, s.Kind, s.MPRISPlayer, s.PID, s.MediaName)
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
