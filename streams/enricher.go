package streams

import (
	"context"
	"time"

	"github.com/yago/smc-mixer/pipewire"
)

// Source records which data source provided the identity for a stream.
type Source uint8

const (
	SourcePipeWire Source = iota // app.name / node.name from pw-dump
	SourceHyprland               // class from hyprctl clients
	SourceMPRIS                  // player name from DBus MPRIS
)

// EnrichedStream is a live PipeWire audio stream with the best available identity.
type EnrichedStream struct {
	ID      uint32 // PipeWire node ID
	PID     uint32 // OS process ID; 0 if unavailable
	Name    string // best display name
	BindKey string // stable key for config matching (MPRIS name or app.name)
	Source  Source
	Track   string // MPRIS: current track title
	Artist  string // MPRIS: first listed artist
}

// UpdateMsg is a tea-compatible message carrying a refreshed stream list.
// Pass it to program.Send to push updates into the Bubbletea event loop.
type UpdateMsg []EnrichedStream

// — internal join types —

type hyprWindow struct {
	PID   uint32
	Class string
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
	pw    func(context.Context) ([]pipewire.Stream, error)
	hypr  func(context.Context) ([]hyprWindow, error)
	mpris func(context.Context) ([]mprisPlayer, error)
}

// New returns an Enricher wired to live system data.
func New(pw *pipewire.Client) *Enricher {
	return &Enricher{
		pw:    pw.ListStreams,
		hypr:  queryHyprland,
		mpris: queryMPRIS,
	}
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

	hyprByPID := make(map[uint32]hyprWindow, len(hyprWindows))
	for _, w := range hyprWindows {
		if w.PID > 0 {
			hyprByPID[w.PID] = w
		}
	}
	mprisByPID := make(map[uint32]mprisPlayer, len(mprisPlayers))
	for _, p := range mprisPlayers {
		if p.PID > 0 {
			mprisByPID[p.PID] = p
		}
	}

	out := make([]EnrichedStream, 0, len(pwStreams))
	for _, s := range pwStreams {
		es := EnrichedStream{
			ID:      s.ID,
			PID:     s.PID,
			Name:    s.Name,
			BindKey: s.Name,
			Source:  SourcePipeWire,
		}
		// Hyprland class overrides PipeWire name
		if w, ok := hyprByPID[s.PID]; ok {
			es.Name = w.Class
			es.BindKey = w.Class
			es.Source = SourceHyprland
		}
		// MPRIS overrides everything (best source)
		if p, ok := mprisByPID[s.PID]; ok {
			es.Name = p.Name
			es.BindKey = p.Name
			es.Source = SourceMPRIS
			es.Track = p.Track
			es.Artist = p.Artist
		}
		out = append(out, es)
	}
	return out, nil
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
