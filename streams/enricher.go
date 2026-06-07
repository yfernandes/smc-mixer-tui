package streams

import (
	"context"
	"strings"
	"time"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/pipewire"
)

// Source records which data source provided the identity for a stream.
type Source uint8

const (
	SourcePipeWire Source = iota // app.name / node.name from pw-dump
	SourceHyprland               // class from hyprctl clients
	SourceMPRIS                  // player name from DBus MPRIS
)

// EnrichedStream is a live PipeWire audio node with the best available identity.
type EnrichedStream struct {
	ID          uint32 // PipeWire node ID
	PID         uint32 // OS process ID; 0 if unavailable
	Name        string // best display name (may be overwritten by Hyprland/MPRIS enrichment)
	AppName     string // original PipeWire application.name, before enrichment
	NodeName    string // PipeWire node.name (stable, used for pactl sink addressing)
	BindKey     string // stable key for config matching (MPRIS name or app.name)
	Source      Source
	Kind        audio.NodeKind // functional role: source app, microphone, or output sink
	MPRISPlayer string         // MPRIS player name (suffix after "org.mpris.MediaPlayer2.")
	Track       string         // MPRIS: current track title
	Artist      string         // MPRIS: first listed artist
	WinTitle    string         // Hyprland: window title of the owning process
	MediaName   string         // PipeWire: media.name (e.g. YouTube video title)
}

// UpdateMsg is a tea-compatible message carrying a refreshed stream list.
// Pass it to program.Send to push updates into the Bubbletea event loop.
type UpdateMsg []EnrichedStream

// — internal join types —

type hyprWindow struct {
	PID   uint32
	Class string
	Title string
}

type mprisPlayer struct {
	Name   string // suffix after "org.mpris.MediaPlayer2."
	PID    uint32
	Track  string
	Artist string
}

// — Enricher —

// Enricher gathers PipeWire streams and enriches them with identity from
// Hyprland and MPRIS, falling back to PipeWire app.name when neither matches.
type Enricher struct {
	pw        func(context.Context) ([]pipewire.Stream, error)
	hypr      func(context.Context) ([]hyprWindow, error)
	mpris     func(context.Context) ([]mprisPlayer, error)
	blacklist map[string]struct{} // names to suppress from Enrich output
}

// New returns an Enricher wired to live system data.
func New(pw *pipewire.Client) *Enricher {
	return &Enricher{
		pw:    pw.ListStreams,
		hypr:  queryHyprland,
		mpris: queryMPRIS,
	}
}

// SetBlacklist replaces the set of stream names hidden from Enrich output.
// Names are matched case-insensitively against the final display name.
func (e *Enricher) SetBlacklist(names []string) {
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[strings.ToLower(n)] = struct{}{}
	}
	e.blacklist = m
}

// Enrich fetches streams from all sources and returns the enriched list.
// Hyprland and MPRIS errors are silently ignored — those sources are optional.
func (e *Enricher) Enrich(ctx context.Context) ([]EnrichedStream, error) {
	pwStreams, err := e.pw(ctx)
	if err != nil {
		return nil, err
	}

	hyprWindows, _ := e.hypr(ctx)
	mprisPlayers, _ := e.mpris(ctx)

	hyprByPID := hyprWindowsByPID(hyprWindows)
	mprisByPID := mprisPlayersByPID(mprisPlayers)

	out := make([]EnrichedStream, 0, len(pwStreams))
	for _, s := range pwStreams {
		out = append(out, enrichStreamIdentity(s, hyprByPID, mprisByPID))
	}
	if len(e.blacklist) > 0 {
		filtered := out[:0]
		for _, es := range out {
			lower := strings.ToLower(es.Name)
			blocked := false
			for pattern := range e.blacklist {
				if strings.Contains(lower, pattern) {
					blocked = true
					break
				}
			}
			if !blocked {
				filtered = append(filtered, es)
			}
		}
		out = filtered
	}

	return out, nil
}

func hyprWindowsByPID(windows []hyprWindow) map[uint32]hyprWindow {
	byPID := make(map[uint32]hyprWindow, len(windows))
	for _, w := range windows {
		if w.PID > 0 {
			byPID[w.PID] = w
		}
	}
	return byPID
}

func mprisPlayersByPID(players []mprisPlayer) map[uint32]mprisPlayer {
	byPID := make(map[uint32]mprisPlayer, len(players))
	for _, p := range players {
		if p.PID > 0 {
			byPID[p.PID] = p
		}
	}
	return byPID
}

// mprisPlayerForPID looks up an MPRIS player by exact PID match first, then
// by walking up /proc ancestry. Returns (player, found, direct) where direct
// is true only for an exact PID match. Browsers like Chromium register MPRIS
// from the main process but spawn a separate audio utility subprocess for
// PipeWire, so the audio PID differs from the MPRIS owner PID.
func mprisPlayerForPID(pid uint32, byPID map[uint32]mprisPlayer) (mprisPlayer, bool, bool) {
	if p, ok := byPID[pid]; ok {
		return p, true, true
	}
	const maxDepth = 10
	cur := pid
	for range maxDepth {
		parent := procParentPID(cur)
		if parent <= 1 {
			break
		}
		cur = parent
		if p, ok := byPID[cur]; ok {
			return p, true, false
		}
	}
	return mprisPlayer{}, false, false
}

func enrichStreamIdentity(
	s pipewire.Stream,
	hyprByPID map[uint32]hyprWindow,
	mprisByPID map[uint32]mprisPlayer,
) EnrichedStream {
	es := EnrichedStream{
		ID:        s.ID,
		PID:       s.PID,
		Name:      s.Name,
		AppName:   s.Name,
		NodeName:  s.NodeName,
		BindKey:   s.Name,
		Source:    SourcePipeWire,
		Kind:      s.Kind,
		MediaName: s.MediaName,
	}
	if w, ok := hyprWindowForPID(s.PID, hyprByPID); ok {
		applyHyprlandIdentity(&es, w)
	}
	if p, found, direct := mprisPlayerForPID(s.PID, mprisByPID); found {
		applyMPRISIdentity(&es, p, direct)
	}
	return es
}

func applyHyprlandIdentity(es *EnrichedStream, w hyprWindow) {
	es.BindKey = w.Class
	es.Source = SourceHyprland
	es.WinTitle = w.Title

	// Split "Track/Context | App Name" into subtitle and name.
	if i := strings.Index(w.Title, " | "); i >= 0 {
		es.MediaName = strings.TrimSpace(w.Title[:i])
		es.Name = strings.TrimSpace(w.Title[i+3:])
	} else if w.Title != "" {
		es.Name = w.Title
	} else {
		es.Name = w.Class
	}
}

// applyMPRISIdentity enriches es with MPRIS metadata.
// When direct is true (PID matched the MPRIS owner exactly), the MPRIS player
// name becomes the authoritative display identity — e.g. "firefox.instance_1"
// lets the config match "firefox.*". When direct is false (ancestry match,
// e.g. Chromium's audio subprocess → browser process), only the control name
// and track metadata are set; the Hyprland-derived display identity is kept so
// existing config rules continue to match.
func applyMPRISIdentity(es *EnrichedStream, p mprisPlayer, direct bool) {
	es.MPRISPlayer = p.Name
	es.Track = p.Track
	es.Artist = p.Artist
	if direct {
		es.Name = p.Name
		es.BindKey = p.Name
		es.Source = SourceMPRIS
	}
}

// Poll calls Enrich every interval, invoking send with each result.
// It fires once immediately, then on each tick.
// Runs until ctx is cancelled.
func (e *Enricher) Poll(ctx context.Context, interval time.Duration, send func(UpdateMsg)) {
	fire := func() {
		if ss, err := e.Enrich(ctx); err == nil {
			send(UpdateMsg(ss))
		}
	}

	fire()

	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			fire()
		}
	}
}
