package ui

import (
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yago/smc-mixer/dispatcher"
	"github.com/yago/smc-mixer/midi"
	"github.com/yago/smc-mixer/streams"
)

// Dispatcher is the subset of dispatcher.Dispatcher used by the TUI.
type Dispatcher interface {
	Snapshot() [8]dispatcher.Channel
	Bind(ch int, id uint32, name string)
	Unbind(ch int)
}

// snapshotMsg carries a fresh dispatcher snapshot from the background tick.
type snapshotMsg [8]dispatcher.Channel

const bindVisible = 8 // max stream rows shown in the bind panel

// Model is the Bubbletea application model.
type Model struct {
	disp Dispatcher

	channels   [8]dispatcher.Channel
	enriched   []streams.EnrichedStream
	selected   int  // 0–7 focused channel strip
	bindMode   bool // user is cycling streams to bind to selected channel
	bindCursor int  // current index into enriched
	bindScroll int  // first visible index in bind panel

	termW int // terminal width from WindowSizeMsg
	termH int // terminal height from WindowSizeMsg

	playing         bool
	recording       bool
	deviceConnected bool
}

// New creates the initial Model. initial is the result of an initial Enrich call.
func New(disp Dispatcher, initial []streams.EnrichedStream) Model {
	return Model{
		disp:            disp,
		channels:        disp.Snapshot(),
		enriched:        sortedByKind(initial),
		deviceConnected: true, // assume connected until told otherwise
	}
}

// sortedByKind returns a copy of ss stable-sorted by NodeKind (source < mic < sink).
func sortedByKind(ss []streams.EnrichedStream) []streams.EnrichedStream {
	out := make([]streams.EnrichedStream, len(ss))
	copy(out, ss)
	slices.SortStableFunc(out, func(a, b streams.EnrichedStream) int {
		return int(a.Kind) - int(b.Kind)
	})
	return out
}

// clampBindScroll keeps bindScroll so that bindCursor stays within the visible window.
func (m *Model) clampBindScroll() {
	if m.bindCursor < m.bindScroll {
		m.bindScroll = m.bindCursor
	} else if m.bindCursor >= m.bindScroll+bindVisible {
		m.bindScroll = m.bindCursor - bindVisible + 1
	}
}

func (m Model) Init() tea.Cmd {
	return tickCmd(m.disp)
}

func tickCmd(d Dispatcher) tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(_ time.Time) tea.Msg {
		return snapshotMsg(d.Snapshot())
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case snapshotMsg:
		m.channels = [8]dispatcher.Channel(msg)
		return m, tickCmd(m.disp)
	case streams.UpdateMsg:
		m.enriched = sortedByKind([]streams.EnrichedStream(msg))
	case tea.WindowSizeMsg:
		m.termW, m.termH = msg.Width, msg.Height
	case midi.DeviceStatusMsg:
		m.deviceConnected = msg.Connected
	case midi.GlobalMsg:
		return m.handleGlobal(msg), nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

