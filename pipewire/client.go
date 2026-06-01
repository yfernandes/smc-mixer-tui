package pipewire

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// NodeKind classifies the functional role of a PipeWire audio node.
type NodeKind uint8

const (
	KindSource NodeKind = iota // app playing audio  (Stream/Output/Audio)
	KindMic                    // microphone / capture (Audio/Source, Stream/Input/Audio)
	KindSink                   // output device / speakers (Audio/Sink)
)

// Stream is an active PipeWire audio node (playback, capture, or hardware device).
type Stream struct {
	ID        uint32
	Name      string   // application.name → node.description → node.name → "stream-<id>"
	MediaName string   // media.name (e.g. YouTube video title, track name)
	PID       uint32   // application.process.id; 0 if absent
	Kind      NodeKind // functional role of the node
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
