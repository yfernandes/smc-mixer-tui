package pipewire

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/yfernandes/smc-mixer-tui/audio"
)

// Stream is an active PipeWire audio node (playback, capture, or hardware device).
type Stream struct {
	ID        uint32
	Name      string         // application.name → node.description → node.name → "stream-<id>"
	NodeName  string         // node.name (stable PW/pactl-addressable name, e.g. alsa_output.pci-...)
	MediaName string         // media.name (e.g. YouTube video title, track name)
	PID       uint32         // application.process.id; 0 if absent
	Kind      audio.NodeKind // functional role of the node
}

// Client wraps wpctl and pw-dump subprocess calls.
// The exec field is injectable for tests.
type Client struct {
	exec func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// New returns a Client that invokes real system commands.
func New() *Client {
	return &Client{
		exec: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		},
	}
}

// ListStreams returns all active audio playback streams discovered via pw-dump.
func (c *Client) ListStreams(ctx context.Context) ([]Stream, error) {
	out, err := c.exec(ctx, "pw-dump")
	if err != nil {
		return nil, fmt.Errorf("pw-dump: %w", err)
	}
	return parseStreams(out)
}

// SetVolume sets the volume for stream id. vol is clamped to [0.0, 1.0].
func (c *Client) SetVolume(ctx context.Context, id uint32, vol float64) error {
	if vol < 0 {
		vol = 0
	} else if vol > 1 {
		vol = 1
	}
	volStr := strconv.FormatFloat(vol, 'f', 4, 64)
	out, err := c.exec(ctx, "wpctl", "set-volume", idStr(id), volStr)
	if err != nil {
		return fmt.Errorf("wpctl set-volume %d %.4f: %w\n%s", id, vol, err, out)
	}
	return nil
}

// SetMute sets the mute state for stream id.
func (c *Client) SetMute(ctx context.Context, id uint32, muted bool) error {
	arg := "0"
	if muted {
		arg = "1"
	}
	out, err := c.exec(ctx, "wpctl", "set-mute", idStr(id), arg)
	if err != nil {
		return fmt.Errorf("wpctl set-mute %d %s: %w\n%s", id, arg, err, out)
	}
	return nil
}

// GetVolume returns the current volume (0.0–1.0+) and mute state for stream id.
func (c *Client) GetVolume(ctx context.Context, id uint32) (float64, bool, error) {
	out, err := c.exec(ctx, "wpctl", "get-volume", idStr(id))
	if err != nil {
		return 0, false, fmt.Errorf("wpctl get-volume %d: %w", id, err)
	}
	return parseVolumeLine(strings.TrimSpace(string(out)))
}

// SinkInput is a PulseAudio-compat sink-input (one stream's connection to one sink).
type SinkInput struct {
	Index       uint32
	OwnerModule uint32 // pactl module ID; 0xFFFFFFFF = no owner
	NodeID      uint32 // PipeWire node.id from Properties; may be 0 for native PW streams
	NodeName    string // node.name from Properties; reliable fallback when NodeID is absent
}

// ListSinkInputs returns all current sink-inputs via pactl.
func (c *Client) ListSinkInputs(ctx context.Context) ([]SinkInput, error) {
	out, err := c.exec(ctx, "pactl", "list", "sink-inputs")
	if err != nil {
		return nil, fmt.Errorf("pactl list sink-inputs: %w", err)
	}
	return parseSinkInputs(out), nil
}

// LoadModule loads a PulseAudio/PipeWire module, returning its module ID.
func (c *Client) LoadModule(ctx context.Context, name, args string) (uint32, error) {
	var argv []string
	if args != "" {
		argv = []string{"load-module", name, args}
	} else {
		argv = []string{"load-module", name}
	}
	out, err := c.exec(ctx, "pactl", argv...)
	if err != nil {
		return 0, fmt.Errorf("pactl load-module %s: %w\n%s", name, err, out)
	}
	id, perr := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 32)
	if perr != nil {
		return 0, fmt.Errorf("parse module id %q: %w", strings.TrimSpace(string(out)), perr)
	}
	return uint32(id), nil
}

// UnloadModule unloads a PulseAudio/PipeWire module by ID.
func (c *Client) UnloadModule(ctx context.Context, id uint32) error {
	out, err := c.exec(ctx, "pactl", "unload-module", idStr(id))
	if err != nil {
		return fmt.Errorf("pactl unload-module %d: %w\n%s", id, err, out)
	}
	return nil
}

// MoveSinkInput moves a sink-input to the named sink.
// Use "@DEFAULT_SINK@" to restore to the default output.
func (c *Client) MoveSinkInput(ctx context.Context, si uint32, sinkName string) error {
	out, err := c.exec(ctx, "pactl", "move-sink-input", idStr(si), sinkName)
	if err != nil {
		return fmt.Errorf("pactl move-sink-input %d %s: %w\n%s", si, sinkName, err, out)
	}
	return nil
}

// SetSinkInputVolume sets the volume of a specific sink-input (0.0–1.0).
// This is per-stream, per-sink — does not affect the global sink volume.
func (c *Client) SetSinkInputVolume(ctx context.Context, si uint32, vol float64) error {
	if vol < 0 {
		vol = 0
	} else if vol > 1 {
		vol = 1
	}
	pct := fmt.Sprintf("%.0f%%", vol*100)
	out, err := c.exec(ctx, "pactl", "set-sink-input-volume", idStr(si), pct)
	if err != nil {
		return fmt.Errorf("pactl set-sink-input-volume %d %s: %w\n%s", si, pct, err, out)
	}
	return nil
}

// SetSinkVolume sets the device volume of a named sink (0.0–1.0).
// Unlike SetSinkInputVolume, this targets the sink node itself and does not
// interact with per-stream sink-input volumes or flat-volume normalization.
func (c *Client) SetSinkVolume(ctx context.Context, sinkName string, vol float64) error {
	if vol < 0 {
		vol = 0
	} else if vol > 1 {
		vol = 1
	}
	pct := fmt.Sprintf("%.0f%%", vol*100)
	out, err := c.exec(ctx, "pactl", "set-sink-volume", sinkName, pct)
	if err != nil {
		return fmt.Errorf("pactl set-sink-volume %s %s: %w\n%s", sinkName, pct, err, out)
	}
	return nil
}

// RouteStreamToSink routes a PipeWire stream node to a specific sink node via
// WirePlumber metadata. Works with PipeWire-native streams that don't appear in
// pactl sink-inputs. WirePlumber processes the change asynchronously.
func (c *Client) RouteStreamToSink(ctx context.Context, streamNodeID, sinkNodeID uint32) error {
	out, err := c.exec(ctx, "pw-metadata", idStr(streamNodeID), "target.object", idStr(sinkNodeID))
	if err != nil {
		return fmt.Errorf("pw-metadata route %d→%d: %w\n%s", streamNodeID, sinkNodeID, err, out)
	}
	return nil
}

// ClearStreamRoute removes the explicit WirePlumber routing target for a stream,
// returning it to automatic (default) routing.
func (c *Client) ClearStreamRoute(ctx context.Context, streamNodeID uint32) error {
	out, err := c.exec(ctx, "pw-metadata", "-d", idStr(streamNodeID), "target.object")
	if err != nil {
		// pw-metadata -d returns non-zero when the key didn't exist; ignore.
		_ = out
	}
	return nil
}

// findNodeIDByName looks up the PipeWire node ID for the node with the given
// node.name by scanning pw-dump output.
func (c *Client) findNodeIDByName(ctx context.Context, nodeName string) (uint32, error) {
	out, err := c.exec(ctx, "pw-dump")
	if err != nil {
		return 0, fmt.Errorf("pw-dump: %w", err)
	}
	ss, err := parseStreams(out)
	if err != nil {
		return 0, err
	}
	for _, s := range ss {
		if s.NodeName == nodeName {
			return s.ID, nil
		}
	}
	return 0, fmt.Errorf("node %q not found in pw-dump", nodeName)
}
