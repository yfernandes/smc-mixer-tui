package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

// bindSubtitle returns the most useful secondary description for a stream in
// the bind picker. Returns "" when nothing useful is available.
func bindSubtitle(es streams.EnrichedStream) string {
	if s := streamSubtitle(es); s != "" {
		return s
	}
	if es.WinTitle != "" && es.WinTitle != es.Name {
		return es.WinTitle
	}
	return ""
}

// renderStreamRows builds the item rows for a stream list panel. It renders avail[scroll:end]
// with kind section headers at group boundaries and the item at highlightIdx marked with ▶.
// w is the total terminal width available for each row.
func renderStreamRows(avail []streams.EnrichedStream, scroll, end, highlightIdx, w int) []string {
	rows := make([]string, 0, end-scroll+1)
	for i := scroll; i < end; i++ {
		es := avail[i]
		if i == 0 || avail[i].Kind != avail[i-1].Kind {
			rows = append(rows, kindHeader(es.Kind, w))
		}
		tag, tagStyle := kindTag(es.Kind)
		label := " " + es.Name
		if sub := bindSubtitle(es); sub != "" {
			label += "  ·  " + sub
		}
		rows = append(rows, renderStreamRow(tag, tagStyle, label, i == highlightIdx, w))
	}
	return rows
}

func renderStreamRow(tag string, tagStyle lipgloss.Style, label string, highlighted bool, w int) string {
	prefix := " "
	style := bindItemStyle
	if highlighted {
		prefix = "▶"
		style = bindCursorStyle
	}
	return tagStyle.Render(tag) + style.Width(w-2-len(tag)).Render(prefix+label)
}

// renderBindPanel renders the full-width stream picker shown below the strips
// when the user is in bind mode. Only shows streams not user-bound to other channels.
func (m Model) renderBindPanel() string {
	header := bindBarStyle.Render(fmt.Sprintf(
		" Bind CH%d   ↑↓ navigate   enter confirm   esc cancel",
		m.selected+1,
	))

	avail := m.availableStreams()
	if len(avail) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, header,
			bindBarStyle.Render(" (no streams)"))
	}

	cursor := min(m.bindCursor, len(avail)-1)
	end := min(m.bindScroll+bindVisible, len(avail))
	return lipgloss.JoinVertical(lipgloss.Left, header, renderStreamPanelRows(avail, m.bindScroll, end, cursor, panelWidth(m.termW)))
}

// renderNavStreamPanel renders the stream list used while navigating with ◀▶ in navStream mode.
// The currently-bound stream is highlighted; cycling with ◀▶ binds immediately.
func (m Model) renderNavStreamPanel() string {
	header := bindBarStyle.Render(fmt.Sprintf(
		" CH%d stream   ◀▶ cycle   ↑↓ function   enter bind list   u unbind   q quit",
		m.selected+1,
	))

	avail := m.availableStreams()
	if len(avail) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, header,
			bindDimStyle.Render(" (no streams available)"))
	}

	boundIdx := m.boundStreamIndex(avail)
	scroll := centeredScroll(boundIdx, len(avail), bindVisible)
	end := min(scroll+bindVisible, len(avail))
	return lipgloss.JoinVertical(lipgloss.Left, header, renderStreamPanelRows(avail, scroll, end, boundIdx, panelWidth(m.termW)))
}

func (m Model) boundStreamIndex(avail []streams.EnrichedStream) int {
	id := m.channels[m.selected].StreamID
	if id == nil {
		return -1
	}
	for i, s := range avail {
		if s.ID == *id {
			return i
		}
	}
	return -1
}

func centeredScroll(idx, count, visible int) int {
	if idx < 0 {
		return 0
	}
	scroll := max(0, idx-visible/2)
	if scroll+visible > count {
		scroll = max(0, count-visible)
	}
	return scroll
}

func renderStreamPanelRows(avail []streams.EnrichedStream, scroll, end, highlightIdx, w int) string {
	var rows []string
	if scroll > 0 {
		rows = append(rows, bindDimStyle.Render(fmt.Sprintf("  ↑ %d more", scroll)))
	}
	rows = append(rows, renderStreamRows(avail, scroll, end, highlightIdx, w)...)
	if below := len(avail) - end; below > 0 {
		rows = append(rows, bindDimStyle.Render(fmt.Sprintf("  ↓ %d more", below)))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func panelWidth(termW int) int {
	if termW < 40 {
		return 120
	}
	return termW
}
