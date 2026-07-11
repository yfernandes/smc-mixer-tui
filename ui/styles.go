package ui

import "github.com/charmbracelet/lipgloss"

// Visual widths (chars) for the two sub-columns inside a strip.
const (
	leftW         = 8  // channel label, knob, fader, pct
	rightW        = 4  // " [X]" button column
	splitFaderH   = 5  // fader rows in split-strip fader zone (one per button: M/S/R/■)
	unifiedFaderH = 7  // fader rows in unified strips (taller bar to match total split strip height)
	faderBarW     = 4  // chars wide for the vertical fader block (≤ leftW)
	knobBarW      = 11 // chars wide for the horizontal knob bar (≤ leftW)
)

var (
	colorAccent = lipgloss.Color("69")      // blue — selected border
	colorDim    = lipgloss.Color("240")     // gray — unbound / inactive
	colorHot    = lipgloss.Color("196")     // red  — rec
	colorWarn   = lipgloss.Color("214")     // orange — mute
	colorGold   = lipgloss.Color("226")     // yellow — solo / bind mode
	colorGreen  = lipgloss.Color("82")      // green — playing
	colorFG     = lipgloss.Color("255")     // white text on coloured buttons
	colorMic    = lipgloss.Color("#FF4444") // red   — microphone / capture
	colorSrc    = lipgloss.Color("#44FF88") // green — audio source / app playing
	colorSink   = lipgloss.Color("#4488FF") // blue  — output device / sink
	colorRouter = lipgloss.Color("#44D7D7") // cyan  — generic router strip

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

	// Per-kind strip borders: unselected / selected.
	stripMic          = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorMic).Foreground(colorDim).Padding(0, 1)
	stripMicSelected  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorMic).Padding(0, 1)
	stripSrcBound     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorSrc).Foreground(colorDim).Padding(0, 1)
	stripSrcSelected  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorSrc).Padding(0, 1)
	stripSinkBound    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorSink).Foreground(colorDim).Padding(0, 1)
	stripSinkSelected = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorSink).Padding(0, 1)
	stripRouter       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorRouter).Foreground(colorDim).Padding(0, 1)
	stripRouterSelect = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorRouter).Padding(0, 1)

	btnInactive = lipgloss.NewStyle().Foreground(colorDim)
	btnMuteOn   = lipgloss.NewStyle().Background(colorWarn).Foreground(colorFG).Bold(true)
	btnSoloOn   = lipgloss.NewStyle().Background(colorGold).Foreground(lipgloss.Color("0")).Bold(true)
	btnRecOn    = lipgloss.NewStyle().Background(colorHot).Foreground(colorFG).Bold(true)
	btnStopOn   = lipgloss.NewStyle().Background(colorAccent).Foreground(colorFG).Bold(true)

	globalBarStyle = lipgloss.NewStyle().Foreground(colorDim)
	bindBarStyle   = lipgloss.NewStyle().Foreground(colorGold).Bold(true)
	navFocusStyle  = lipgloss.NewStyle().Foreground(colorGold) // focused nav setting (not bold)
	styleNoDevice  = lipgloss.NewStyle().Foreground(colorWarn).Bold(true)

	bindCursorStyle = lipgloss.NewStyle().Foreground(colorGold).Bold(true)
	bindItemStyle   = lipgloss.NewStyle()
	bindDimStyle    = lipgloss.NewStyle().Foreground(colorDim)

	// Kind tag styles used in the bind panel item rows.
	kindTagMicStyle  = lipgloss.NewStyle().Foreground(colorMic).Bold(true)
	kindTagSrcStyle  = lipgloss.NewStyle().Foreground(colorSrc).Bold(true)
	kindTagSinkStyle = lipgloss.NewStyle().Foreground(colorSink).Bold(true)

	// Kind header styles used as section dividers in the bind panel.
	kindHdrMicStyle  = lipgloss.NewStyle().Foreground(colorMic).Faint(true)
	kindHdrSrcStyle  = lipgloss.NewStyle().Foreground(colorSrc).Faint(true)
	kindHdrSinkStyle = lipgloss.NewStyle().Foreground(colorSink).Faint(true)

	// Dual fader bar styles.
	hwFaderStyle   = lipgloss.NewStyle().Foreground(colorHot)   // HW bar — red (physical position)
	appFaderStyle  = lipgloss.NewStyle().Foreground(colorGreen) // APP bar unsynced — green (PipeWire target)
	syncFaderStyle = lipgloss.NewStyle().Foreground(colorFG)    // both bars synced — white
	hwUnknownStyle = lipgloss.NewStyle().Foreground(colorGold)  // HW floor marker before first CC — yellow

	// pickupArrowStyle highlights the volume label when the channel is awaiting sync.
	pickupArrowStyle = lipgloss.NewStyle().Foreground(colorWarn).Bold(true)

	// Device-type subtitle styles used in split-strip zones.
	subtypeInputStyle    = lipgloss.NewStyle().Foreground(colorMic)
	subtypePlaybackStyle = lipgloss.NewStyle().Foreground(colorSrc)
	subtypeOutputStyle   = lipgloss.NewStyle().Foreground(colorSink)
	subtypeDimStyle      = lipgloss.NewStyle().Foreground(colorDim)
	subtypeRouterStyle   = lipgloss.NewStyle().Foreground(colorRouter)

	// splitDividerStyle draws the horizontal rule between knob and fader zones.
	// Width is set at call-site via .Width(n) to match the strip's inner content width.
	splitDividerStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), false, false, true, false).
				BorderForeground(colorDim)
)
