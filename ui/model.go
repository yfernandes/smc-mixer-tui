package ui

import (
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/midi"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

// Dispatcher is the subset of dispatcher.Dispatcher used by the TUI.
type Dispatcher interface {
	Snapshot() [8]dispatcher.Channel
	Bind(ch int, id uint32, name string, kind audio.NodeKind, mprisName string)
	Unbind(ch int)
	ToggleMute(ch int)
	ToggleSolo(ch int)
}

// navSetting identifies which per-channel parameter is focused for MIDI navigation.
type navSetting int

const (
	navStream      navSetting = iota // cycle through bound streams
	navMute                          // toggle mute
	navSolo                          // toggle solo
	navSettingCount navSetting = iota
)

// snapshotMsg carries a fresh dispatcher snapshot from the background tick.
type snapshotMsg [8]dispatcher.Channel

const bindVisible = 8 // max stream rows shown in the bind panel

// Model is the Bubbletea application model.
type Model struct {
	disp Dispatcher

	channels   [8]dispatcher.Channel
	labels     [8]string
	enriched   []streams.EnrichedStream
	selected   int  // 0–7 focused channel strip
	bindMode   bool // user is cycling streams to bind to selected channel
	bindCursor int  // current index into enriched
	bindScroll int  // first visible index in bind panel

	termW int // terminal width from WindowSizeMsg
	termH int // terminal height from WindowSizeMsg

	ActivePage      string     // current page name; "main" if none active
	ChannelAdvanced [8]bool    // advanced mode per strip; resets on page switch
	deviceConnected bool
	navSetting      navSetting // currently focused per-channel setting for MIDI nav
	navStreamOpen   bool       // stream-list panel is visible; set on ◀/▶, cleared on context change
}

// New creates the initial Model. snap and labels are the initial channel state
// and per-channel config labels; initial is the enriched stream list.
// All are provided by the daemon on connect.
func New(disp Dispatcher, snap [8]dispatcher.Channel, labels [8]string, initial []streams.EnrichedStream) Model {
	return Model{
		disp:            disp,
		channels:        snap,
		labels:          labels,
		enriched:        sortedByKind(initial),
		deviceConnected: true, // assume connected until told otherwise
		ActivePage:      "main",
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

// pageKindFilter returns the NodeKind that the active page restricts to, and
// whether such a restriction is in effect. Pages without a kind restriction
// (e.g. "main") return false.
func (m Model) pageKindFilter() (audio.NodeKind, bool) {
	switch m.ActivePage {
	case "applications":
		return audio.KindSource, true
	case "outputs":
		return audio.KindSink, true
	case "inputs":
		return audio.KindMic, true
	}
	return 0, false
}

// availableStreams returns m.enriched filtered by two rules:
//  1. Streams user-bound to another channel are excluded.
//  2. On page-specific views (applications/outputs/inputs) only the matching
//     NodeKind is shown.
func (m Model) availableStreams() []streams.EnrichedStream {
	blocked := map[uint32]bool{}
	for ch, c := range m.channels {
		if ch == m.selected {
			continue
		}
		if c.UserBound && c.StreamID != nil {
			blocked[*c.StreamID] = true
		}
	}
	kindOnly, hasKind := m.pageKindFilter()

	if len(blocked) == 0 && !hasKind {
		return m.enriched
	}
	out := make([]streams.EnrichedStream, 0, len(m.enriched))
	for _, s := range m.enriched {
		if blocked[s.ID] {
			continue
		}
		if hasKind && s.Kind != kindOnly {
			continue
		}
		out = append(out, s)
	}
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
		for ch := range 8 {
			m.ChannelAdvanced[ch] = m.channels[ch].Advanced
		}
		return m, tickCmd(m.disp)
	case streams.UpdateMsg:
		m.enriched = sortedByKind([]streams.EnrichedStream(msg))
	case tea.WindowSizeMsg:
		m.termW, m.termH = msg.Width, msg.Height
	case midi.DeviceStatusMsg:
		m.deviceConnected = msg.Connected
	case midi.GlobalMsg:
		return m.handleGlobal(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}
