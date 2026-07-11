package ui

import "github.com/charmbracelet/lipgloss"

func (m Model) View() string {
	if m.routingOpen {
		if m.routingPickerOpen {
			return lipgloss.JoinVertical(lipgloss.Left, m.renderRouting(), m.renderRetargetPicker())
		}
		return m.renderRouting()
	}
	strips := make([]string, 8)
	for i := range 8 {
		if s, ok := m.strips[i]; ok {
			strips[i] = m.renderGenericStrip(i, s)
		} else {
			strips[i] = m.renderStrip(i)
		}
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, strips...)
	if m.bindMode {
		return lipgloss.JoinVertical(lipgloss.Left, row, m.renderBindPanel())
	}
	if m.navStreamOpen {
		return lipgloss.JoinVertical(lipgloss.Left, row, m.renderNavStreamPanel())
	}
	return lipgloss.JoinVertical(lipgloss.Left, row, m.renderBar())
}
