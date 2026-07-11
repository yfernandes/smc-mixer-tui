package router

import (
	"context"
	"testing"

	"github.com/yfernandes/smc-mixer-tui/backend"
	"github.com/yfernandes/smc-mixer-tui/surface"
)

type fakeBackend struct {
	calls []fakeCall
}

type fakeCall struct {
	target backend.TargetID
	param  string
	value  backend.Value
}

func (f *fakeBackend) Name() string { return "fake" }
func (f *fakeBackend) Targets(context.Context) ([]backend.TargetInfo, error) {
	return nil, nil
}
func (f *fakeBackend) Set(_ context.Context, t backend.TargetID, param string, v backend.Value) error {
	f.calls = append(f.calls, fakeCall{target: t, param: param, value: v})
	return nil
}
func (f *fakeBackend) Get(context.Context, backend.TargetID, string) (backend.Value, bool, error) {
	return backend.Value{}, false, nil
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
