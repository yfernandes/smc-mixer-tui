package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/midi"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

// — test doubles —

type fakeDisp struct {
	snap    [8]dispatcher.Channel
	binds   []bindCall
	unbinds []int
}

type bindCall struct {
	ch        int
	id        uint32
	name      string
	kind      audio.NodeKind
	mprisName string
}

func (f *fakeDisp) Snapshot() [8]dispatcher.Channel { return f.snap }
func (f *fakeDisp) Bind(ch int, id uint32, name string, kind audio.NodeKind, mprisName string) {
	f.binds = append(f.binds, bindCall{ch, id, name, kind, mprisName})
}
func (f *fakeDisp) Unbind(ch int) { f.unbinds = append(f.unbinds, ch) }

// — helpers —

func makeModel(d *fakeDisp, ss []streams.EnrichedStream) Model {
	return New(d, d.snap, [8]string{}, ss)
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

// — transport (GlobalMsg) —

func TestPlaySetsPlaying(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m = upd(m, midi.GlobalMsg{Action: midi.ActionPlay, Pressed: true})
	if !m.playing {
		t.Fatal("expected playing=true after ActionPlay")
	}
}

func TestPauseTogglesPlaying(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m = upd(m, midi.GlobalMsg{Action: midi.ActionPlay, Pressed: true})
	m = upd(m, midi.GlobalMsg{Action: midi.ActionPause, Pressed: true})
	if m.playing {
		t.Fatal("Pause should stop playing")
	}
	m = upd(m, midi.GlobalMsg{Action: midi.ActionPause, Pressed: true})
	if !m.playing {
		t.Fatal("second Pause should restart playing (toggle)")
	}
}

func TestRecordToggles(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m = upd(m, midi.GlobalMsg{Action: midi.ActionRecord, Pressed: true})
	if !m.recording {
		t.Fatal("expected recording=true")
	}
	m = upd(m, midi.GlobalMsg{Action: midi.ActionRecord, Pressed: true})
	if m.recording {
		t.Fatal("expected recording=false after second press")
	}
}

func TestGlobalReleaseIgnored(t *testing.T) {
	m := makeModel(&fakeDisp{}, nil)
	m = upd(m, midi.GlobalMsg{Action: midi.ActionPlay, Pressed: false})
	if m.playing {
		t.Fatal("button release should not affect state")
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

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
