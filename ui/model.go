package ui

import (
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

// Model is the Bubbletea application model.
type Model struct {
	disp Dispatcher

	channels   [8]dispatcher.Channel
	enriched   []streams.EnrichedStream
	selected   int  // 0–7 focused channel strip
	bindMode   bool // user is cycling streams to bind to selected channel
	bindCursor int  // current index into enriched

	playing         bool
	recording       bool
	deviceConnected bool
}

// New creates the initial Model. initial is the result of an initial Enrich call.
func New(disp Dispatcher, initial []streams.EnrichedStream) Model {
	return Model{
		disp:            disp,
		channels:        disp.Snapshot(),
		enriched:        initial,
		deviceConnected: true, // assume connected until told otherwise
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
		m.enriched = []streams.EnrichedStream(msg)
	case midi.DeviceStatusMsg:
		m.deviceConnected = msg.Connected
	case midi.GlobalMsg:
		return m.handleGlobal(msg), nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		// layout is fixed-width; no reflow needed
	}
	return m, nil
}

