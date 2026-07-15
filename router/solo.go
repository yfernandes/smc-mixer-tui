package router

import (
	"context"
	"fmt"

	"github.com/yfernandes/smc-mixer-tui/backend"
	"github.com/yfernandes/smc-mixer-tui/surface"
)

// ToggleSolo implements generic exclusive-within-group behavior. Solo itself
// remains router state; the backend only needs a mute parameter.
func (r *Router) ToggleSolo(ctx context.Context, strip int) error {
	r.mu.Lock()
	a, ok := r.assignments[strip]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("router: strip %d is unassigned", strip)
	}
	st := r.stateForLocked(strip)
	group := st.group
	solo := st.paramFor("solo")
	solo.lastValue.B = !solo.lastValue.B
	solo.remoteKnown = true
	solo.synced = true
	anySolo := false
	for other, otherA := range r.assignments {
		otherState := r.stateForLocked(other)
		if otherState.group == group && group != "" && otherState.paramFor("solo").lastValue.B {
			anySolo = true
		}
		_ = otherA
	}
	type update struct {
		target backend.TargetID
		mute   bool
	}
	var updates []update
	for other, otherA := range r.assignments {
		otherState := r.stateForLocked(other)
		if group == "" || otherState.group != group {
			continue
		}
		muteParam := otherA.Params[surface.RoleMute]
		if muteParam == "" {
			continue
		}
		muteState := otherState.paramFor(muteParam)
		shouldSoloMute := anySolo && !otherState.paramFor("solo").lastValue.B
		muted := muteState.lastValue.B
		if shouldSoloMute && !otherState.soloMuted {
			otherState.muteBeforeSolo = muted
			otherState.soloMuted = true
			muted = true
		} else if !shouldSoloMute && otherState.soloMuted {
			muted = otherState.muteBeforeSolo
			otherState.soloMuted = false
		}
		muteState.lastValue = backend.Value{B: muted}
		updates = append(updates, update{target: otherA.Target, mute: muted})
	}
	_ = a
	r.mu.Unlock()
	for _, update := range updates {
		b, found := r.backendFor(update.target)
		if !found {
			continue
		}
		if err := b.Set(ctx, update.target, "mute", backend.Value{B: update.mute}); err != nil {
			return err
		}
	}
	r.notify()
	return nil
}
