package pipewire

import "testing"

func TestMatchingSinkInputPrefersNodeID(t *testing.T) {
	sis := []SinkInput{
		{Index: 10, NodeID: 111, NodeName: "wrong"},
		{Index: 20, NodeID: 222, NodeName: "target"},
	}

	got, ok := matchingSinkInput(sis, 111, "target")

	if !ok || got != 10 {
		t.Fatalf("matchingSinkInput() = (%d, %v), want first node id match", got, ok)
	}
}

func TestMatchingSinkInputFallsBackToNodeName(t *testing.T) {
	sis := []SinkInput{
		{Index: 20, NodeName: "firefox.node"},
	}

	got, ok := matchingSinkInput(sis, 0, "firefox.node")

	if !ok || got != 20 {
		t.Fatalf("matchingSinkInput() = (%d, %v), want node name match", got, ok)
	}
}

func TestMatchingSinkInputNoMatch(t *testing.T) {
	got, ok := matchingSinkInput([]SinkInput{{Index: 20, NodeID: 222, NodeName: "other"}}, 111, "target")

	if ok || got != 0 {
		t.Fatalf("matchingSinkInput() = (%d, %v), want no match", got, ok)
	}
}
