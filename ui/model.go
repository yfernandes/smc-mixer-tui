package ui

import (
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/daemon"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/midi"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

// Dispatcher is the subset of dispatcher.Dispatcher used by the TUI.
type Dispatcher interface {
	Snapshot() [8]dispatcher.Channel
	Bind(ch int, id uint32, name string, kind audio.NodeKind, mprisName string, pid uint32, mediaName string)
	Unbind(ch int)
	ToggleMute(ch int)
	ToggleSolo(ch int)
	RequestRouting()
}

// navSetting identifies which per-channel parameter is focused for MIDI navigation.
type navSetting int

const (
	navStream       navSetting = iota // cycle through bound streams
	navMute                           // toggle mute
	navSolo                           // toggle solo
	navSettingCount navSetting = iota
)

// snapshotMsg carries a fresh dispatcher snapshot from the background tick.
type snapshotMsg [8]dispatcher.Channel

const bindVisible = 8 // max stream rows shown in the bind panel

// StripConfig carries per-channel config derived at startup from config.Pages["main"].
// IsSplit is true when the fader and knob slots reference different device keys; in
// that case the strip renders as two independent zones on the main page.
type StripConfig struct {
	IsSplit    bool
	KnobLabel  string // DeviceConfig.Label for the knob device
	KnobType   string // "input", "playback", "output", or ""
	FaderLabel string // DeviceConfig.Label for the fader device
	FaderType  string // same
}

// Model is the Bubbletea application model.
type Model struct {
	disp     Dispatcher
	reloadFn func() [8]StripConfig // called on 'r'; re-reads config and returns fresh strip configs

	channels   [8]dispatcher.Channel
	labels     [8]string
	stripCfgs  [8]StripConfig
	enriched   []streams.EnrichedStream
	selected   int  // 0–7 focused channel strip
	bindMode   bool // user is cycling streams to bind to selected channel
	bindCursor int  // current index into enriched
	bindScroll int  // first visible index in bind panel

	termW int // terminal width from WindowSizeMsg
	termH int // terminal height from WindowSizeMsg

	ActivePage      string  // current page name; "main" if none active
	ChannelAdvanced [8]bool // advanced mode per strip; resets on page switch
	deviceConnected bool
	navSetting      navSetting // currently focused per-channel setting for MIDI nav
	navStreamOpen   bool       // stream-list panel is visible; set on ◀/▶, cleared on context change
	cfgReloads      int        // incremented each time 'r' fires; shows in status bar
	versionMismatch bool       // true when TUI and daemon were built from different commits

	routingOpen bool                   // routing inspector overlay is visible; toggled by Tab, independent of ActivePage
	routing     daemon.RoutingSnapshot // last routing snapshot received from the daemon
}

// New creates the initial Model. snap and labels are the initial channel state
// and per-channel config labels; initial is the enriched stream list.
// stripCfgs describes which strips render as split zones on the main page.
// reloadFn is called when the user presses 'r'; it re-reads the config file and returns
// a fresh set of strip configs. May be nil (reload key becomes a no-op).
func New(disp Dispatcher, snap [8]dispatcher.Channel, labels [8]string, initial []streams.EnrichedStream, stripCfgs [8]StripConfig, reloadFn func() [8]StripConfig, versionMismatch bool) Model {
	return Model{
		disp:            disp,
		reloadFn:        reloadFn,
		channels:        snap,
		labels:          labels,
		stripCfgs:       stripCfgs,
		enriched:        sortedByKind(initial),
		deviceConnected: true, // assume connected until told otherwise
		ActivePage:      "main",
		versionMismatch: versionMismatch,
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

// routingTickMsg drives the periodic re-request of a routing snapshot while
// the routing inspector is open.
type routingTickMsg struct{}

func routingTickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(_ time.Time) tea.Msg {
		return routingTickMsg{}
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
	case daemon.RoutingMsg:
		m.routing = daemon.RoutingSnapshot(msg)
	case routingTickMsg:
		if !m.routingOpen {
			return m, nil
		}
		m.disp.RequestRouting()
		return m, routingTickCmd()
	}
	return m, nil
}
