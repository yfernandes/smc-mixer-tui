package streams

import "testing"

func TestParseHyprClients(t *testing.T) {
	data := []byte(`[
		{"class": "firefox", "pid": 1234},
		{"class": "spotify", "pid": 5678},
		{"class": "",        "pid": 9999},
		{"class": "ghost",   "pid": 0}
	]`)

	ws, err := parseHyprClients(data)
	if err != nil {
		t.Fatal(err)
	}
	// empty class and pid=0 entries must be dropped
	if len(ws) != 2 {
		t.Fatalf("want 2 windows, got %d: %v", len(ws), ws)
	}
	if ws[0].PID != 1234 || ws[0].Class != "firefox" {
		t.Errorf("ws[0]: %+v", ws[0])
	}
	if ws[1].PID != 5678 || ws[1].Class != "spotify" {
		t.Errorf("ws[1]: %+v", ws[1])
	}
}

func TestParseHyprClientsEmpty(t *testing.T) {
	ws, err := parseHyprClients([]byte(`[]`))
	if err != nil || len(ws) != 0 {
		t.Errorf("empty array: err=%v ws=%v", err, ws)
	}
}

func TestParseHyprClientsInvalidJSON(t *testing.T) {
	_, err := parseHyprClients([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestHyprWindowForPIDDirectMatch(t *testing.T) {
	byPID := map[uint32]hyprWindow{
		100: {PID: 100, Class: "spotify", Title: "Spotify"},
	}
	w, ok := hyprWindowForPID(100, byPID)
	if !ok || w.Class != "spotify" {
		t.Fatalf("direct match failed: ok=%v w=%+v", ok, w)
	}
}

func TestHyprWindowForPIDNoMatch(t *testing.T) {
	byPID := map[uint32]hyprWindow{
		100: {PID: 100, Class: "spotify"},
	}
	_, ok := hyprWindowForPID(1, byPID) // PID 1 has no ancestors to check
	if ok {
		t.Fatal("expected no match for unrelated PID")
	}
}
