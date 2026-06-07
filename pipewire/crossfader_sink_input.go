package pipewire

import (
	"context"
	"fmt"
	"time"
)

// findSinkInput returns the pactl sink-input index for the given PW stream, matching
// first by node.id and falling back to node.name. The node.name fallback handles
// PipeWire-native streams (e.g. Firefox, Chrome) where pactl may omit node.id.
// Retries a few times to allow recently-started streams to appear.
func (c *Client) findSinkInput(ctx context.Context, nodeID uint32, nodeName string) (uint32, error) {
	for attempt := range 5 {
		if err := waitBeforeSinkInputRetry(ctx, attempt); err != nil {
			return 0, err
		}
		sis, err := c.ListSinkInputs(ctx)
		if err != nil {
			continue
		}
		if index, ok := matchingSinkInput(sis, nodeID, nodeName); ok {
			return index, nil
		}
	}
	return 0, fmt.Errorf("node %d / %q not found in pactl sink-inputs", nodeID, nodeName)
}

func waitBeforeSinkInputRetry(ctx context.Context, attempt int) error {
	if attempt == 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(150 * time.Millisecond):
		return nil
	}
}

func matchingSinkInput(sis []SinkInput, nodeID uint32, nodeName string) (uint32, bool) {
	for _, si := range sis {
		if (nodeID != 0 && si.NodeID == nodeID) || (nodeName != "" && si.NodeName == nodeName) {
			return si.Index, true
		}
	}
	return 0, false
}
