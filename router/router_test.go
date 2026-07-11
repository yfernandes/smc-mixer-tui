package router

import (
	"context"
	"testing"

	"github.com/yfernandes/smc-mixer-tui/backend"
	"github.com/yfernandes/smc-mixer-tui/surface"
)

type fakeBackend struct {
	infos  []backend.TargetInfo
	values map[string]backend.Value
	calls  []fakeCall
}

type fakeCall struct {
	target backend.TargetID
	param  string
	value  backend.Value
}

func (f *fakeBackend) Name() string { return "fake" }
func (f *fakeBackend) Targets(context.Context) ([]backend.TargetInfo, error) {
	return f.infos, nil
}
func (f *fakeBackend) Set(_ context.Context, t backend.TargetID, param string, v backend.Value) error {
	f.calls = append(f.calls, fakeCall{target: t, param: param, value: v})
	if f.values != nil {
		f.values[string(t)+"/"+param] = v
	}
	return nil
}
func (f *fakeBackend) Get(_ context.Context, t backend.TargetID, param string) (backend.Value, bool, error) {
	v, ok := f.values[string(t)+"/"+param]
	return v, ok, nil
}

func TestHandleEventDispatchesAssignedFader(t *testing.T) {
	fb := &fakeBackend{}
	rt := New(map[string]backend.Backend{"fake": fb}, map[int]Assignment{
		3: {Target: "fake:brightness", Params: map[surface.Role]string{surface.RoleFader: "value"}},
	})

	if !rt.HandleEvent(context.Background(), surface.Event{Strip: 3, Role: surface.RoleFader, Value: 0.75}) {
		t.Fatal("expected event to be handled")
	}
	if len(fb.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(fb.calls))
	}
	call := fb.calls[0]
	if call.target != "fake:brightness" || call.param != "value" || call.value.F != 0.75 {
		t.Fatalf("unexpected call: %+v", call)
	}
}

func TestHandleEventIgnoresUnassignedStrip(t *testing.T) {
	fb := &fakeBackend{}
	rt := New(map[string]backend.Backend{"fake": fb}, nil)
	if rt.HandleEvent(context.Background(), surface.Event{Strip: 3, Role: surface.RoleFader, Value: 0.75}) {
		t.Fatal("unassigned strip should not be handled")
	}
	if len(fb.calls) != 0 {
		t.Fatalf("calls = %d, want 0", len(fb.calls))
	}
}

func TestHandleEventAccumulatesKnob(t *testing.T) {
	fb := &fakeBackend{}
	rt := New(map[string]backend.Backend{"fake": fb}, map[int]Assignment{
		1: {Target: "fake:gain", Params: map[surface.Role]string{surface.RoleKnob: "value"}},
	})

	rt.HandleEvent(context.Background(), surface.Event{Strip: 1, Role: surface.RoleKnob, Delta: 1})
	rt.HandleEvent(context.Background(), surface.Event{Strip: 1, Role: surface.RoleKnob, Delta: 1})

	if len(fb.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(fb.calls))
	}
	if got := fb.calls[1].value.F; got != 2.0/127.0 {
		t.Fatalf("knob value = %v, want %v", got, 2.0/127.0)
	}
}

func TestReadableFaderRequiresSoftPickupCrossing(t *testing.T) {
	fb := readableFake(backend.ParamContinuous, backend.Value{F: 0.5})
	rt := New(map[string]backend.Backend{"fake": fb}, map[int]Assignment{
		0: {Target: "fake:brightness", Params: map[surface.Role]string{surface.RoleFader: "value"}},
	})
	rt.Activate(context.Background())

	rt.HandleEvent(context.Background(), surface.Event{Strip: 0, Role: surface.RoleFader, Value: 0.2})
	rt.HandleEvent(context.Background(), surface.Event{Strip: 0, Role: surface.RoleFader, Value: 0.4})
	if len(fb.calls) != 0 {
		t.Fatalf("calls before pickup = %d, want 0", len(fb.calls))
	}
	rt.HandleEvent(context.Background(), surface.Event{Strip: 0, Role: surface.RoleFader, Value: 0.49})
	if len(fb.calls) != 1 || !rt.Snapshot()[0].Params["value"].Synced {
		t.Fatalf("pickup did not sync/write: calls=%+v snap=%+v", fb.calls, rt.Snapshot())
	}
}

func TestReadableFaderFastSweepOvershootSyncs(t *testing.T) {
	fb := readableFake(backend.ParamContinuous, backend.Value{F: 0.5})
	rt := New(map[string]backend.Backend{"fake": fb}, map[int]Assignment{
		0: {Target: "fake:brightness", Params: map[surface.Role]string{surface.RoleFader: "value"}},
	})
	rt.Activate(context.Background())

	rt.HandleEvent(context.Background(), surface.Event{Strip: 0, Role: surface.RoleFader, Value: 0.1})
	rt.HandleEvent(context.Background(), surface.Event{Strip: 0, Role: surface.RoleFader, Value: 0.9})
	if len(fb.calls) != 1 {
		t.Fatalf("calls after overshoot = %d, want 1", len(fb.calls))
	}
}

func TestFireAndForgetFaderBypassesPickup(t *testing.T) {
	fb := &fakeBackend{infos: []backend.TargetInfo{{
		ID:     "fake:brightness",
		Label:  "Brightness",
		Params: []backend.ParamSpec{{ID: "value", Kind: backend.ParamContinuous}},
	}}}
	rt := New(map[string]backend.Backend{"fake": fb}, map[int]Assignment{
		0: {Target: "fake:brightness", Params: map[surface.Role]string{surface.RoleFader: "value"}},
	})
	rt.Activate(context.Background())

	rt.HandleEvent(context.Background(), surface.Event{Strip: 0, Role: surface.RoleFader, Value: 0.2})
	if len(fb.calls) != 1 {
		t.Fatalf("fire-and-forget calls = %d, want 1", len(fb.calls))
	}
}

func TestActivateReseedsReadableParam(t *testing.T) {
	fb := readableFake(backend.ParamContinuous, backend.Value{F: 0.25})
	rt := New(map[string]backend.Backend{"fake": fb}, map[int]Assignment{
		0: {Target: "fake:brightness", Params: map[surface.Role]string{surface.RoleFader: "value"}},
	})
	rt.Activate(context.Background())
	if got := rt.Snapshot()[0].Params["value"].Value.F; got != 0.25 {
		t.Fatalf("seeded value = %v, want 0.25", got)
	}
	fb.values["fake:brightness/value"] = backend.Value{F: 0.75}
	rt.Activate(context.Background())
	if got := rt.Snapshot()[0].Params["value"].Value.F; got != 0.75 {
		t.Fatalf("reseeded value = %v, want 0.75", got)
	}
}

func TestToggleUsesCachedStateAndOptimisticallyUpdates(t *testing.T) {
	fb := readableFake(backend.ParamToggle, backend.Value{B: false})
	rt := New(map[string]backend.Backend{"fake": fb}, map[int]Assignment{
		0: {Target: "fake:lamp", Params: map[surface.Role]string{surface.RoleMute: "mute"}},
	})
	rt.Activate(context.Background())

	rt.HandleEvent(context.Background(), surface.Event{Strip: 0, Role: surface.RoleMute, Pressed: true})
	if len(fb.calls) != 1 || !fb.calls[0].value.B {
		t.Fatalf("toggle call = %+v, want true", fb.calls)
	}
	if got := rt.Snapshot()[0].Params["mute"].Value.B; !got {
		t.Fatalf("cached toggle state = %v, want true", got)
	}
}

func readableFake(kind backend.ParamKind, v backend.Value) *fakeBackend {
	return &fakeBackend{
		infos: []backend.TargetInfo{{
			ID:     "fake:brightness",
			Label:  "Brightness",
			Params: []backend.ParamSpec{{ID: "value", Kind: kind, Readable: true}},
		}, {
			ID:     "fake:lamp",
			Label:  "Lamp",
			Params: []backend.ParamSpec{{ID: "mute", Kind: kind, Readable: true}},
		}},
		values: map[string]backend.Value{
			"fake:brightness/value": v,
			"fake:lamp/mute":        v,
		},
	}
}
