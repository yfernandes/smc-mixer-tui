package streams

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// queryHyprland calls "hyprctl clients -j" and returns the window list.
// Returns nil (not an error) when Hyprland is not running.
func queryHyprland(ctx context.Context) ([]hyprWindow, error) {
	out, err := exec.CommandContext(ctx, "hyprctl", "clients", "-j").Output()
	if err != nil {
		return nil, nil // Hyprland absent; treat as unavailable
	}
	return parseHyprClients(out)
}

// hyprctlWindow is the relevant subset of a hyprctl clients entry.
type hyprctlWindow struct {
	Class string `json:"class"`
	PID   uint32 `json:"pid"`
}

func parseHyprClients(data []byte) ([]hyprWindow, error) {
	var raw []hyprctlWindow
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse hyprctl clients: %w", err)
	}
	out := make([]hyprWindow, 0, len(raw))
	for _, w := range raw {
		if w.Class == "" || w.PID == 0 {
			continue
		}
		out = append(out, hyprWindow{PID: w.PID, Class: w.Class})
	}
	return out, nil
}
