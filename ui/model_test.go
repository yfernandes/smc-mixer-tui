package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/daemon"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/midi"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

// — test doubles —

type fakeDisp struct {
	snap            [8]dispatcher.Channel
	binds           []bindCall
	unbinds         []int
	routingRequests int
	retargets       []retargetCall
}

type bindCall struct {
	ch        int
	id        uint32
	name      string
	kind      audio.NodeKind
	mprisName string
	pid       uint32
	mediaName string
}

type retargetCall struct {
	deviceKey, branch, sinkNodeName, sinkDisplayName string
}

func (f *fakeDisp) Snapshot() [8]dispatcher.Channel { return f.snap }
func (f *fakeDisp) Bind(ch int, id uint32, name string, kind audio.NodeKind, mprisName string, pid uint32, mediaName string) {
	f.binds = append(f.binds, bindCall{ch, id, name, kind, mprisName, pid, mediaName})
}
func (f *fakeDisp) Unbind(ch int)     { f.unbinds = append(f.unbinds, ch) }
func (f *fakeDisp) ToggleMute(ch int) {}
func (f *fakeDisp) ToggleSolo(ch int) {}
func (f *fakeDisp) RequestRouting()   { f.routingRequests++ }
func (f *fakeDisp) RetargetOutput(deviceKey, branch, sinkNodeName, sinkDisplayName string) {
	f.retargets = append(f.retargets, retargetCall{deviceKey, branch, sinkNodeName, sinkDisplayName})
}

// — helpers —

func makeModel(d *fakeDisp, ss []streams.EnrichedStream) Model {
	return New(d, d.snap, [8]string{}, ss, [8]StripConfig{}, nil, false)
}

// upd sends msg through Update and returns the new Model, discarding the Cmd.
func upd(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

func kLeft() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyLeft} }
func kRight() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRight} }
func kUp() tea.KeyMsg    { return tea.KeyMsg{Type: tea.KeyUp} }
func kDown() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyDown} }
func kEnter() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }
func kEsc() tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyEsc} }
func kRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// — navigation tests —

func TestRightMovesSelection(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m = upd(m, kRight())
	if m.selected != 1 {
		t.Fatalf("selected = %d, want 1", m.selected)
	}
}

func TestLeftMovesSelection(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m = upd(m, kLeft())
	if m.selected != 7 { // wraps around
		t.Fatalf("selected = %d, want 7 (wrap)", m.selected)
	}
}

func TestRightWrapsAtEnd(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	for i := 0; i < 8; i++ {
		m = upd(m, kRight())
	}
	if m.selected != 0 {
		t.Fatalf("selected = %d after full cycle, want 0", m.selected)
	}
}

func TestArrowsLockedInBindMode(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m.bindMode = true
	m = upd(m, kLeft())
	m = upd(m, kRight())
	if m.selected != 0 {
		t.Fatalf("left/right should be locked in bind mode, selected = %d", m.selected)
	}
}

// — bind mode tests —

func TestEnterActivatesBindMode(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m = upd(m, kEnter())
	if !m.bindMode {
		t.Fatal("Enter should activate bind mode")
	}
	if m.bindCursor != 0 {
		t.Fatalf("bindCursor = %d, want 0", m.bindCursor)
	}
}

func TestBindModeDownCyclesStreams(t *testing.T) {
	ss := []streams.EnrichedStream{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}, {ID: 3, Name: "C"}}
	m := makeModel(&fakeDisp{}, ss)
	m = upd(m, kEnter()) // enter bind mode
	m = upd(m, kDown())
	if m.bindCursor != 1 {
		t.Fatalf("bindCursor = %d, want 1", m.bindCursor)
	}
	m = upd(m, kDown())
	m = upd(m, kDown()) // wraps back to 0
	if m.bindCursor != 0 {
		t.Fatalf("bindCursor should wrap, got %d", m.bindCursor)
	}
}

func TestBindModeUpCyclesStreams(t *testing.T) {
	ss := []streams.EnrichedStream{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}}
	m := makeModel(&fakeDisp{}, ss)
	m = upd(m, kEnter()) // enter bind mode (cursor=0)
	m = upd(m, kUp())    // wrap to last
	if m.bindCursor != 1 {
		t.Fatalf("bindCursor = %d, want 1 (wrapped up)", m.bindCursor)
	}
}

func TestBindConfirmCallsBind(t *testing.T) {
	disp := &fakeDisp{}
	ss := []streams.EnrichedStream{{ID: 42, Name: "Firefox"}, {ID: 99, Name: "Spotify"}}
	m := makeModel(disp, ss)

	m = upd(m, kEnter()) // enter bind mode
	m = upd(m, kDown())  // move to Spotify (cursor=1)
	m = upd(m, kEnter()) // confirm

	if m.bindMode {
		t.Fatal("bind mode should exit after confirm")
	}
	if len(disp.binds) != 1 {
		t.Fatalf("expected 1 Bind call, got %d", len(disp.binds))
	}
	b := disp.binds[0]
	if b.ch != 0 || b.id != 99 || b.name != "Spotify" {
		t.Fatalf("Bind(%d, %d, %q), want (0, 99, Spotify)", b.ch, b.id, b.name)
	}
}

func TestBindConfirmNoStreams(t *testing.T) {
	disp := &fakeDisp{}
	m := makeModel(disp, nil) // no streams
	m = upd(m, kEnter())      // enter bind mode
	m = upd(m, kEnter())      // confirm with no streams

	if m.bindMode {
		t.Fatal("bind mode should exit even with no streams")
	}
	if len(disp.binds) != 0 {
		t.Fatal("no Bind call expected when no streams available")
	}
}

func TestEscCancelsBindMode(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m = upd(m, kEnter()) // enter
	m = upd(m, kEsc())   // cancel
	if m.bindMode {
		t.Fatal("Esc should exit bind mode")
	}
}

func TestUnbindKey(t *testing.T) {
	disp := &fakeDisp{}
	m := makeModel(disp, nil)
	m.selected = 3
	m = upd(m, kRune('u'))
	if len(disp.unbinds) != 1 || disp.unbinds[0] != 3 {
		t.Fatalf("expected Unbind(3), got %v", disp.unbinds)
	}
}

func TestUnbindLockedInBindMode(t *testing.T) {
	disp := &fakeDisp{}
	m := makeModel(disp, nil)
	m.bindMode = true
	m = upd(m, kRune('u'))
	if len(disp.unbinds) != 0 {
		t.Fatal("Unbind should be suppressed while in bind mode")
	}
}

// — snapshot update —

func TestSnapshotMsgUpdatesChannels(t *testing.T) {
	disp := &fakeDisp{}
	m := makeModel(disp, nil)

	var snap [8]dispatcher.Channel
	snap[2].ActualVolume = 0.75
	m = upd(m, snapshotMsg(snap))

	if m.channels[2].ActualVolume != 0.75 {
		t.Fatalf("channel 2 actual volume = %.2f, want 0.75", m.channels[2].ActualVolume)
	}
}

// — transport / page switching (GlobalMsg) —

func TestPlaySwitchesToApplicationsPage(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m = upd(m, midi.GlobalMsg{Action: midi.ActionPlay, Pressed: true})
	if m.ActivePage != "applications" {
		t.Fatalf("ActivePage = %q, want applications", m.ActivePage)
	}
}

func TestPlayAgainReturnsToMain(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m = upd(m, midi.GlobalMsg{Action: midi.ActionPlay, Pressed: true})
	m = upd(m, midi.GlobalMsg{Action: midi.ActionPlay, Pressed: true})
	if m.ActivePage != "main" {
		t.Fatalf("ActivePage = %q, want main after toggle", m.ActivePage)
	}
}

func TestPageSwitchResetsChannelAdvanced(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m.ChannelAdvanced[3] = true
	m = upd(m, midi.GlobalMsg{Action: midi.ActionRecord, Pressed: true})
	for i, adv := range m.ChannelAdvanced {
		if adv {
			t.Fatalf("ChannelAdvanced[%d] should be reset on page switch", i)
		}
	}
}

func TestGlobalReleaseIgnored(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m = upd(m, midi.GlobalMsg{Action: midi.ActionPlay, Pressed: false})
	if m.ActivePage != "main" {
		t.Fatalf("button release should not change ActivePage, got %q", m.ActivePage)
	}
}

// — MIDI navigation (global nav buttons) —

func gMsg(action midi.GlobalAction) midi.GlobalMsg {
	return midi.GlobalMsg{Action: action, Pressed: true}
}

func TestSeekForwardSelectsNextChannel(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m = upd(m, gMsg(midi.ActionSeekForward))
	if m.selected != 1 {
		t.Fatalf("selected = %d, want 1", m.selected)
	}
}

func TestSeekBackWrapsChannel(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m = upd(m, gMsg(midi.ActionSeekBack))
	if m.selected != 7 {
		t.Fatalf("selected = %d, want 7 (wrap)", m.selected)
	}
}

func TestUpDownCycleNavSetting(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	if m.navSetting != navStream {
		t.Fatalf("initial navSetting = %v, want navStream", m.navSetting)
	}
	m = upd(m, gMsg(midi.ActionDown))
	if m.navSetting != navMute {
		t.Fatalf("navSetting = %v after Down, want navMute", m.navSetting)
	}
	m = upd(m, gMsg(midi.ActionDown))
	if m.navSetting != navSolo {
		t.Fatalf("navSetting = %v after Down, want navSolo", m.navSetting)
	}
	m = upd(m, gMsg(midi.ActionDown)) // wraps
	if m.navSetting != navStream {
		t.Fatalf("navSetting = %v after wrap Down, want navStream", m.navSetting)
	}
	m = upd(m, gMsg(midi.ActionUp)) // wraps back
	if m.navSetting != navSolo {
		t.Fatalf("navSetting = %v after Up wrap, want navSolo", m.navSetting)
	}
}

func TestMidiRightOnStreamBindsNext(t *testing.T) {
	ss := []streams.EnrichedStream{{ID: 10, Name: "A"}, {ID: 20, Name: "B"}, {ID: 30, Name: "C"}}
	disp := &fakeDisp{}
	m := makeModel(disp, ss)
	// no stream bound yet → right binds first
	m = upd(m, gMsg(midi.ActionRight))
	if len(disp.binds) != 1 || disp.binds[0].id != 10 {
		t.Fatalf("expected Bind(id=10), got %v", disp.binds)
	}
}

func TestMidiRightCyclesFromBoundStream(t *testing.T) {
	id := uint32(20)
	ss := []streams.EnrichedStream{{ID: 10, Name: "A"}, {ID: 20, Name: "B"}, {ID: 30, Name: "C"}}
	disp := &fakeDisp{}
	disp.snap[0].StreamID = &id
	m := makeModel(disp, ss)
	m = upd(m, gMsg(midi.ActionRight)) // B → C
	if len(disp.binds) != 1 || disp.binds[0].id != 30 {
		t.Fatalf("expected Bind(id=30), got %v", disp.binds)
	}
}

// — navStreamOpen lifecycle —

func TestCycleOpensNavStreamPanel(t *testing.T) {
	ss := []streams.EnrichedStream{{ID: 1, Name: "A"}}
	m := makeModel(&fakeDisp{}, ss)
	if m.navStreamOpen {
		t.Fatal("navStreamOpen should be false on init")
	}
	m = upd(m, gMsg(midi.ActionRight)) // cycle stream → opens panel
	if !m.navStreamOpen {
		t.Fatal("navStreamOpen should be true after cycling stream")
	}
}

func TestNavSettingChangeClosesPanel(t *testing.T) {
	ss := []streams.EnrichedStream{{ID: 1, Name: "A"}}
	m := makeModel(&fakeDisp{}, ss)
	m = upd(m, gMsg(midi.ActionRight)) // open panel
	m = upd(m, gMsg(midi.ActionDown))  // change navSetting → closes panel
	if m.navStreamOpen {
		t.Fatal("navStreamOpen should close on navSetting change")
	}
}

func TestChannelChangeClosesPanel(t *testing.T) {
	ss := []streams.EnrichedStream{{ID: 1, Name: "A"}}
	m := makeModel(&fakeDisp{}, ss)
	m = upd(m, gMsg(midi.ActionRight))       // open panel
	m = upd(m, gMsg(midi.ActionSeekForward)) // change channel → closes panel
	if m.navStreamOpen {
		t.Fatal("navStreamOpen should close on channel change")
	}
}

func TestPageSwitchClosesPanel(t *testing.T) {
	ss := []streams.EnrichedStream{{ID: 1, Name: "A"}}
	m := makeModel(&fakeDisp{}, ss)
	m = upd(m, gMsg(midi.ActionRight)) // open panel
	m = upd(m, gMsg(midi.ActionPlay))  // switch page → closes panel
	if m.navStreamOpen {
		t.Fatal("navStreamOpen should close on page switch")
	}
}

func TestMuteCycleDoesNotOpenPanel(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m = upd(m, gMsg(midi.ActionDown))  // navSetting → mute
	m = upd(m, gMsg(midi.ActionRight)) // apply mute (not stream cycle)
	if m.navStreamOpen {
		t.Fatal("navStreamOpen should not open when navSetting is mute")
	}
}

func TestPageFilterRestrictsAvailableStreams(t *testing.T) {
	ss := []streams.EnrichedStream{
		{ID: 1, Name: "Firefox", Kind: audio.KindSource},
		{ID: 2, Name: "Mic", Kind: audio.KindMic},
		{ID: 3, Name: "Speakers", Kind: audio.KindSink},
	}
	m := makeModel(&fakeDisp{}, ss)

	// applications page → sources only
	m.ActivePage = "applications"
	avail := m.availableStreams()
	if len(avail) != 1 || avail[0].ID != 1 {
		t.Fatalf("applications page: want [Firefox], got %v", avail)
	}

	// outputs page → sinks only
	m.ActivePage = "outputs"
	avail = m.availableStreams()
	if len(avail) != 1 || avail[0].ID != 3 {
		t.Fatalf("outputs page: want [Speakers], got %v", avail)
	}

	// inputs page → mics only
	m.ActivePage = "inputs"
	avail = m.availableStreams()
	if len(avail) != 1 || avail[0].ID != 2 {
		t.Fatalf("inputs page: want [Mic], got %v", avail)
	}

	// main page → all streams
	m.ActivePage = "main"
	avail = m.availableStreams()
	if len(avail) != 3 {
		t.Fatalf("main page: want all 3 streams, got %d", len(avail))
	}
}

func TestUserBoundStreamExcludedFromCycle(t *testing.T) {
	id20 := uint32(20)
	ss := []streams.EnrichedStream{{ID: 10, Name: "A"}, {ID: 20, Name: "B"}, {ID: 30, Name: "C"}}
	disp := &fakeDisp{}
	// ch1 has stream 20 user-bound; ch0 is selected (default)
	disp.snap[1].StreamID = &id20
	disp.snap[1].UserBound = true
	m := makeModel(disp, ss)
	// cycling from ch0 (no binding) should skip stream 20
	m = upd(m, gMsg(midi.ActionRight)) // first available: A (id=10)
	if len(disp.binds) != 1 || disp.binds[0].id != 10 {
		t.Fatalf("expected Bind(id=10), got %v", disp.binds)
	}
	// update channels so ch0 is now bound to 10
	id10 := uint32(10)
	m.channels[0].StreamID = &id10
	disp.binds = nil
	m = upd(m, gMsg(midi.ActionRight)) // next after 10: C (id=30, skipping 20)
	if len(disp.binds) != 1 || disp.binds[0].id != 30 {
		t.Fatalf("expected Bind(id=30) skipping user-bound 20, got %v", disp.binds)
	}
}

func TestUserBoundStreamExcludedFromBindPanel(t *testing.T) {
	id20 := uint32(20)
	ss := []streams.EnrichedStream{{ID: 10, Name: "A"}, {ID: 20, Name: "B"}, {ID: 30, Name: "C"}}
	disp := &fakeDisp{}
	disp.snap[1].StreamID = &id20
	disp.snap[1].UserBound = true
	m := makeModel(disp, ss)
	m = upd(m, kEnter()) // enter bind mode on ch0
	avail := m.availableStreams()
	if len(avail) != 2 {
		t.Fatalf("expected 2 available streams (B excluded), got %d: %v", len(avail), avail)
	}
	for _, s := range avail {
		if s.ID == 20 {
			t.Fatal("user-bound stream 20 should not appear in available streams")
		}
	}
}

func TestSeekForwardLockedInBindMode(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m.bindMode = true
	m = upd(m, gMsg(midi.ActionSeekForward))
	if m.selected != 0 {
		t.Fatalf("seek-forward should be locked in bind mode, selected = %d", m.selected)
	}
}

// — view smoke test —

func TestViewReturnsNonEmpty(t *testing.T) {
	m := makeModel(&fakeDisp{}, []streams.EnrichedStream{{ID: 1, Name: "Firefox"}})
	v := m.View()
	if v == "" {
		t.Fatal("View() returned empty string")
	}
}

func TestViewInBindModeShowsBindBar(t *testing.T) {
	ss := []streams.EnrichedStream{{ID: 1, Name: "Firefox"}}
	m := makeModel(&fakeDisp{}, ss)
	m = upd(m, kEnter()) // enter bind mode
	v := m.View()
	if !contains(v, "Bind CH") {
		t.Fatal("View() in bind mode should contain bind panel header")
	}
}

func TestViewInactiveChannelShowsTombstone(t *testing.T) {
	// Channel 0 is bound to stream 99, but stream 99 is not in the enriched list.
	id := uint32(99)
	disp := &fakeDisp{}
	disp.snap[0].StreamID = &id
	disp.snap[0].Name = "gone-app"
	// enriched list has a different stream
	ss := []streams.EnrichedStream{{ID: 1, Name: "other"}}
	m := makeModel(disp, ss)
	v := m.View()
	if !contains(v, "⊗") {
		t.Fatal("inactive bound channel should display ⊗ tombstone")
	}
}

func TestUpdateMsgRefreshesEnriched(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	newStreams := []streams.EnrichedStream{{ID: 7, Name: "vlc"}}
	m = upd(m, streams.UpdateMsg(newStreams))
	if len(m.enriched) != 1 || m.enriched[0].Name != "vlc" {
		t.Errorf("UpdateMsg should refresh enriched list: %v", m.enriched)
	}
}

// — config reload ('r' key) —

func TestReloadKeyIncrementsCounter(t *testing.T) {
	called := 0
	reloadFn := func() [8]StripConfig {
		called++
		return [8]StripConfig{}
	}
	m := New(&fakeDisp{}, [8]dispatcher.Channel{}, [8]string{}, nil, [8]StripConfig{}, reloadFn, false)
	m = upd(m, kRune('r'))
	if m.cfgReloads != 1 {
		t.Fatalf("cfgReloads = %d after 'r', want 1", m.cfgReloads)
	}
	if called != 1 {
		t.Fatalf("reloadFn called %d times, want 1", called)
	}
}

func TestReloadKeyUpdatesStripCfgs(t *testing.T) {
	want := [8]StripConfig{{IsSplit: true, KnobType: "input"}}
	reloadFn := func() [8]StripConfig { return want }
	m := New(&fakeDisp{}, [8]dispatcher.Channel{}, [8]string{}, nil, [8]StripConfig{}, reloadFn, false)
	m = upd(m, kRune('r'))
	if m.stripCfgs[0].IsSplit != true || m.stripCfgs[0].KnobType != "input" {
		t.Fatalf("stripCfgs not updated after reload: %+v", m.stripCfgs[0])
	}
}

func TestReloadKeyNoopWithNilFn(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil) // reloadFn is nil
	m = upd(m, kRune('r'))
	if m.cfgReloads != 0 {
		t.Fatalf("cfgReloads = %d with nil reloadFn, want 0", m.cfgReloads)
	}
}

func TestReloadKeySuppressedInBindMode(t *testing.T) {
	called := 0
	reloadFn := func() [8]StripConfig { called++; return [8]StripConfig{} }
	m := New(&fakeDisp{}, [8]dispatcher.Channel{}, [8]string{}, nil, [8]StripConfig{}, reloadFn, false)
	m.bindMode = true
	m = upd(m, kRune('r'))
	if called != 0 {
		t.Fatalf("reloadFn should not be called in bind mode, called %d times", called)
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

// — routing inspector (Tab key) —

func kTab() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyTab} }

func TestTabOpensRoutingAndRequestsSnapshot(t *testing.T) {
	disp := &fakeDisp{}
	m := makeModel(disp, nil)
	m = upd(m, kTab())
	if !m.routingOpen {
		t.Fatal("Tab should open the routing inspector")
	}
	if disp.routingRequests != 1 {
		t.Fatalf("routingRequests = %d, want 1", disp.routingRequests)
	}
}

func TestTabTogglesRoutingClosed(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m = upd(m, kTab())
	m = upd(m, kTab())
	if m.routingOpen {
		t.Fatal("second Tab should close the routing inspector")
	}
}

func TestEscClosesRouting(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m = upd(m, kTab())
	m = upd(m, kEsc())
	if m.routingOpen {
		t.Fatal("Esc should close the routing inspector")
	}
}

func TestRoutingViewRendersStreamName(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m.routingOpen = true
	m.routing = daemon.RoutingSnapshot{Routes: []daemon.RouteNode{
		{StreamName: "Firefox", AttachedCh: -1, Branches: []daemon.RouteBranch{
			{Label: "Direct", Steps: []daemon.RouteStep{{Label: "Fader", NodeName: "firefox", HasInternal: true, InternalVolume: 0.5, LiveKnown: true, LiveVolume: 0.5}}},
		}},
	}}
	v := m.View()
	if !contains(v, "Firefox") {
		t.Fatalf("routing view should contain stream name, got: %s", v)
	}
	if !contains(v, "(unbound)") {
		t.Fatalf("routing view should mark unattached stream as unbound, got: %s", v)
	}
}

// — routing inspector: output retargeting —

func crossfadeRoutingSnapshot() daemon.RoutingSnapshot {
	branch := func(label string) daemon.RouteBranch {
		return daemon.RouteBranch{Label: label, Steps: []daemon.RouteStep{
			{Label: "Gain " + label, HasVolume: true, HasInternal: true, InternalVolume: 0.5, LiveKnown: true, LiveVolume: 0.5},
			{Label: "Output", NodeName: "sink-" + label, HasVolume: true, LiveKnown: true, LiveVolume: 1},
		}}
	}
	return daemon.RoutingSnapshot{Routes: []daemon.RouteNode{
		{StreamName: "Zen", Category: "applications", AttachedCh: -1, DeviceKey: "zen",
			Trunk:    []daemon.RouteStep{{Label: "Stream", NodeName: "Zen"}},
			Branches: []daemon.RouteBranch{branch("A"), branch("B")}},
	}}
}

func sinkStreams() []streams.EnrichedStream {
	return []streams.EnrichedStream{
		{ID: 1, Name: "Speakers", NodeName: "alsa_output.speakers", Kind: audio.KindSink},
		{ID: 2, Name: "Headphones", NodeName: "alsa_output.headphones", Kind: audio.KindSink},
		{ID: 3, Name: "Third Speaker", NodeName: "alsa_output.third", Kind: audio.KindSink},
	}
}

func TestRetargetTargetsFindsCrossfadeBranches(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m.routing = crossfadeRoutingSnapshot()
	targets := m.retargetTargets()
	if len(targets) != 2 {
		t.Fatalf("want 2 retarget targets (A and B), got %d", len(targets))
	}
}

func TestRoutingCursorMovesThroughBranches(t *testing.T) {
	m := makeModel(&fakeDisp{}, sinkStreams())
	m.routingOpen = true
	m.routing = crossfadeRoutingSnapshot()
	m = upd(m, kDown())
	if m.routingCursor != 1 {
		t.Fatalf("routingCursor = %d, want 1 after Down", m.routingCursor)
	}
	m = upd(m, kDown()) // wraps
	if m.routingCursor != 0 {
		t.Fatalf("routingCursor should wrap to 0, got %d", m.routingCursor)
	}
}

func TestEnterOpensRetargetPickerAndConfirmSendsCommand(t *testing.T) {
	disp := &fakeDisp{}
	m := makeModel(disp, sinkStreams())
	m.routingOpen = true
	m.routing = crossfadeRoutingSnapshot()

	m = upd(m, kDown())  // select branch B
	m = upd(m, kEnter()) // open picker
	if !m.routingPickerOpen {
		t.Fatal("Enter should open the retarget picker")
	}

	m = upd(m, kDown()) // Speakers -> Headphones
	m = upd(m, kDown()) // Headphones -> Third Speaker
	m = upd(m, kEnter())
	if m.routingPickerOpen {
		t.Fatal("Enter should close the picker after confirming")
	}
	if len(disp.retargets) != 1 {
		t.Fatalf("expected 1 RetargetOutput call, got %d", len(disp.retargets))
	}
	got := disp.retargets[0]
	want := retargetCall{deviceKey: "zen", branch: "B", sinkNodeName: "alsa_output.third", sinkDisplayName: "Third Speaker"}
	if got != want {
		t.Fatalf("RetargetOutput call = %+v, want %+v", got, want)
	}
}

func TestEscClosesPickerBeforeRoutingView(t *testing.T) {
	m := makeModel(&fakeDisp{}, sinkStreams())
	m.routingOpen = true
	m.routing = crossfadeRoutingSnapshot()
	m = upd(m, kEnter()) // open picker
	m = upd(m, kEsc())
	if m.routingPickerOpen {
		t.Fatal("first Esc should close the picker")
	}
	if !m.routingOpen {
		t.Fatal("routing view should still be open after closing just the picker")
	}
	m = upd(m, kEsc())
	if m.routingOpen {
		t.Fatal("second Esc should close the routing view")
	}
}
