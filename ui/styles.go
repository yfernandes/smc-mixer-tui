package ui

import "github.com/charmbracelet/lipgloss"

// Visual widths (chars) for the two sub-columns inside a strip.
const (
	leftW     = 8  // channel label, knob, fader, pct
	rightW    = 4  // " [X]" button column
	faderH    = 5  // rows tall for the vertical fader
	faderBarW = 4  // chars wide for the vertical fader block (≤ leftW)
	knobBarW  = 11 // chars wide for the horizontal knob bar (≤ leftW)
)

var (
	colorAccent = lipgloss.Color("69")  // blue — selected border
	colorDim    = lipgloss.Color("240") // gray — unbound / inactive
	colorHot    = lipgloss.Color("196") // red  — rec
	colorWarn   = lipgloss.Color("214") // orange — mute
	colorGold   = lipgloss.Color("226") // yellow — solo / bind mode
	colorGreen  = lipgloss.Color("82")  // green — playing
	colorFG     = lipgloss.Color("255") // white text on coloured buttons

	stripNormal = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim).
			Padding(0, 1)

	stripSelected = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(0, 1)

	stripUnbound = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim).
			Foreground(colorDim).
			Padding(0, 1)

	stripSelectedUnbound = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorAccent).
				Foreground(colorDim).
				Padding(0, 1)

	// Gold border + text when user is cycling streams to bind.
	stripBindMode = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorGold).
			Padding(0, 1)

	btnInactive = lipgloss.NewStyle().Foreground(colorDim)
	btnMuteOn   = lipgloss.NewStyle().Background(colorWarn).Foreground(colorFG).Bold(true)
	btnSoloOn   = lipgloss.NewStyle().Background(colorGold).Foreground(lipgloss.Color("0")).Bold(true)
	btnRecOn    = lipgloss.NewStyle().Background(colorHot).Foreground(colorFG).Bold(true)
	btnStopOn   = lipgloss.NewStyle().Background(colorAccent).Foreground(colorFG).Bold(true)

	stylePlay = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	styleRec  = lipgloss.NewStyle().Foreground(colorHot).Bold(true)

	globalBarStyle = lipgloss.NewStyle().Foreground(colorDim)
	bindBarStyle   = lipgloss.NewStyle().Foreground(colorGold).Bold(true)
	styleNoDevice  = lipgloss.NewStyle().Foreground(colorWarn).Bold(true)
)
