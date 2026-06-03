package streams

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
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
	Title string `json:"title"`
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
		out = append(out, hyprWindow{PID: w.PID, Class: w.Class, Title: w.Title})
	}
	return out, nil
}

// hyprWindowForPID looks up a Hyprland window by exact PID match first, then
// by walking up /proc ancestry. Apps like Chromium spawn separate audio
// subprocesses whose PIDs differ from the window PID.
func hyprWindowForPID(pid uint32, byPID map[uint32]hyprWindow) (hyprWindow, bool) {
	const maxDepth = 10
	cur := pid
	for range maxDepth {
		if w, ok := byPID[cur]; ok {
			return w, true
		}
		parent := procParentPID(cur)
		if parent <= 1 {
			break
		}
		cur = parent
	}
	return hyprWindow{}, false
}

// procParentPID reads the PPid field from /proc/<pid>/status.
func procParentPID(pid uint32) uint32 {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PPid:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "PPid:"))
			n, err := strconv.ParseUint(val, 10, 32)
			if err == nil {
				return uint32(n)
			}
		}
	}
	return 0
}
