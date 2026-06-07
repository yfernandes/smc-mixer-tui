package daemon

import (
	"context"
	"testing"

	"github.com/yfernandes/smc-mixer-tui/audio"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
)

type daemonFakePW struct{}

func (daemonFakePW) SetVolume(context.Context, uint32, float64) error { return nil }
func (daemonFakePW) SetMute(context.Context, uint32, bool) error      { return nil }

func TestDecodeCommandBind(t *testing.T) {
	env := mustEnvelope(t, kindBind, bindPayload{
		Ch:        3,
		ID:        99,
		Name:      "Firefox",
		Kind:      audio.KindSource,
		MPRISName: "firefox.instance_1",
	})

	cmd, ok, err := decodeCommand(env)
	if err != nil || !ok {
		t.Fatalf("decodeCommand() = (%+v, %v, %v), want command", cmd, ok, err)
	}
	if cmd.kind != kindBind || cmd.ch != 3 || cmd.bind.ID != 99 ||
		cmd.bind.Name != "Firefox" || cmd.bind.Kind != audio.KindSource ||
		cmd.bind.MPRISName != "firefox.instance_1" {
		t.Fatalf("decoded command = %+v", cmd)
	}
}

func TestDecodeCommandUnknownKind(t *testing.T) {
	cmd, ok, err := decodeCommand(envelope{Kind: kindSnapshot})
	if err != nil || ok || cmd.kind != "" {
		t.Fatalf("decodeCommand(snapshot) = (%+v, %v, %v), want ignored", cmd, ok, err)
	}
}

func TestDecodeCommandRejectsMalformedPayload(t *testing.T) {
	_, ok, err := decodeCommand(envelope{Kind: kindMute, Data: []byte(`{"ch":"not-a-number"}`)})
	if err == nil || ok {
		t.Fatalf("decodeCommand() = (_, %v, %v), want decode error", ok, err)
	}
}

func TestHandleCmdAppliesDispatcherActions(t *testing.T) {
	disp := dispatcher.New(daemonFakePW{})
	srv := NewServer(disp, [8]string{}, "")
	ctx := context.Background()

	srv.handleCmd(ctx, mustEnvelope(t, kindBind, bindPayload{
		Ch:   1,
		ID:   42,
		Name: "Spotify",
		Kind: audio.KindSource,
	}))

	snap := disp.Snapshot()
	if snap[1].StreamID == nil || *snap[1].StreamID != 42 ||
		snap[1].Name != "Spotify" || snap[1].Kind != audio.KindSource || !snap[1].UserBound {
		t.Fatalf("bind did not update dispatcher snapshot: %+v", snap[1])
	}

	srv.handleCmd(ctx, mustEnvelope(t, kindMute, muteTogglePayload{Ch: 1}))
	if !disp.Snapshot()[1].Mute {
		t.Fatal("mute command did not toggle channel mute")
	}

	srv.handleCmd(ctx, mustEnvelope(t, kindSolo, soloTogglePayload{Ch: 1}))
	if !disp.Snapshot()[1].Solo {
		t.Fatal("solo command did not toggle channel solo")
	}

	srv.handleCmd(ctx, mustEnvelope(t, kindUnbind, unbindPayload{Ch: 1}))
	snap = disp.Snapshot()
	if snap[1].StreamID != nil || !snap[1].ManuallyUnbound {
		t.Fatalf("unbind did not clear and mark channel: %+v", snap[1])
	}
}

func mustEnvelope(t *testing.T, kind msgKind, payload any) envelope {
	t.Helper()
	frame, err := encodeFrame(kind, payload)
	if err != nil {
		t.Fatalf("encodeFrame() error = %v", err)
	}
	env, err := decodeEnvelope(frame)
	if err != nil {
		t.Fatalf("decodeEnvelope() error = %v", err)
	}
	return env
}
