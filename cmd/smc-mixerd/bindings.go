package main

import (
	"context"

	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/streams"
)

// clearPageAssignments removes all strip-to-stream assignments that are not
// manually pinned or manually unbound. Called on page switch so the incoming
// page starts from a clean slate. PipeWire routing is not touched.
func clearPageAssignments(disp *dispatcher.Dispatcher) {
	snap := disp.Snapshot()
	for ch, c := range snap {
		if c.ManuallyUnbound || c.Pinned {
			continue
		}
		disp.ResetStrip(ch)
	}
}

func applyBindings(ctx context.Context, cfg *config.Config, disp *dispatcher.Dispatcher, ss []streams.EnrichedStream, pinnedKeys map[int]string, getVol knobVolumeGetter) {
	clearStaleBindings(disp, ss)
	activePage := disp.ActivePage()
	// Sync pinned flags before planning so planBindings can skip already-live pinned slots.
	syncPinnedFlags(cfg, disp, activePage, pinnedKeys)
	for _, action := range planBindings(cfg, activePage, disp.Snapshot(), ss) {
		switch {
		case action.lose:
			disp.LoseBinding(action.ch)
		case action.syncSpec:
			// Stream already matched; only refresh config-derived metadata.
			dev := cfg.ChannelForPage(activePage, action.ch)
			disp.SetAdvancedSpec(action.ch, advancedSpecFrom(dev))
			applySyncMode(cfg, disp, action.ch, dev)
		case action.userBound:
			// PID-based reconnect: preserve UserBound semantics across stream restarts.
			snap := disp.Snapshot()
			disp.UserBind(action.ch, action.id, action.name, action.kind, action.mprisName, snap[action.ch].BoundPID)
			dev := cfg.ChannelForPage(activePage, action.ch)
			disp.SetAdvancedSpec(action.ch, advancedSpecFrom(dev))
			applySyncMode(cfg, disp, action.ch, dev)
			seedActualVolume(ctx, disp, action.ch, action.id, getVol)
		default:
			disp.Bind(action.ch, action.id, action.name, action.kind, action.mprisName)
			dev := cfg.ChannelForPage(activePage, action.ch)
			disp.SetAdvancedSpec(action.ch, advancedSpecFrom(dev))
			applySyncMode(cfg, disp, action.ch, dev)
			seedActualVolume(ctx, disp, action.ch, action.id, getVol)
		}
	}
	applyKnobBindings(ctx, cfg, disp, activePage, ss, getVol)
	refreshBindingMetadata(disp, ss)
}

// syncPinnedFlags updates Channel.Pinned for all channels based on current page and pinnedKeys.
// On main page: a slot is pinned if it appears in pinnedKeys.
// On other pages: a slot is pinned if its device key matches the pinned key for that slot.
func syncPinnedFlags(cfg *config.Config, disp *dispatcher.Dispatcher, activePage string, pinnedKeys map[int]string) {
	for ch := range 8 {
		pinnedKey, hasPinned := pinnedKeys[ch]
		var isPinned bool
		if hasPinned {
			if activePage == "main" {
				isPinned = true
			} else {
				isPinned = cfg.DeviceKeyForPage(activePage, ch) == pinnedKey
			}
		}
		disp.SetPinned(ch, isPinned)
	}
}

// seedActualVolume fetches the current PipeWire volume for a newly-bound stream and
// immediately populates ActualVolume. Without this, bind() leaves ActualVolume=0 and
// moveSoftPickup would treat 0 as the target, causing a false sync when the fader
// passes through zero before the volume poller runs (~50 ms later).
func seedActualVolume(ctx context.Context, disp *dispatcher.Dispatcher, ch int, id uint32, getVol knobVolumeGetter) {
	if getVol == nil {
		return
	}
	if vol, _, err := getVol(ctx, id); err == nil {
		disp.UpdateActualVolume(ch, vol)
	}
}

// applySyncMode applies the effective sync mode and pickup tolerance from config to channel ch.
// It is idempotent: safe to call on every applyBindings pass, including syncSpec refreshes.
func applySyncMode(cfg *config.Config, disp *dispatcher.Dispatcher, ch int, dev *config.DeviceConfig) {
	cfgMode := cfg.EffectiveSyncMode(dev)
	var dispMode dispatcher.SyncMode
	if cfgMode == config.SyncModeSoftPickup {
		dispMode = dispatcher.SyncModeSoftPickup
	}
	tol := float64(cfg.EffectivePickupToleranceCC()) / 127.0
	disp.SetChannelSyncMode(ch, dispMode, tol)
}

func advancedSpecFrom(dev *config.DeviceConfig) *dispatcher.AdvancedSpec {
	if dev == nil || dev.Advanced == nil {
		return nil
	}
	spec := &dispatcher.AdvancedSpec{}
	if dev.Advanced.Fader != nil {
		spec.FaderEffect = dev.Advanced.Fader.Effect
	}
	if dev.Advanced.Knob != nil {
		spec.KnobEffect = dev.Advanced.Knob.Effect
	}
	if dev.Advanced.MuteButton != nil {
		spec.MuteButtonAction = dev.Advanced.MuteButton.Action
	}
	if dev.Advanced.SoloButton != nil {
		spec.SoloButtonAction = dev.Advanced.SoloButton.Action
	}
	if dev.Advanced.StopButton != nil {
		spec.StopButtonAction = dev.Advanced.StopButton.Action
	}
	return spec
}

func configLabels(cfg *config.Config) [8]string {
	var labels [8]string
	for ch := range 8 {
		if dev := cfg.ChannelFor(ch); dev != nil {
			labels[ch] = dev.Label
		}
	}
	return labels
}
