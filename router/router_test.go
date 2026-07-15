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

func TestSoloMutesOnlyNonSoloedAssignmentsInSameGroup(t *testing.T) {
	fb := &fakeBackend{infos: []backend.TargetInfo{
		{ID: "fake:a", Group: "playback", Params: []backend.ParamSpec{{ID: "mute", Kind: backend.ParamToggle, Readable: true}}},
		{ID: "fake:b", Group: "playback", Params: []backend.ParamSpec{{ID: "mute", Kind: backend.ParamToggle, Readable: true}}},
		{ID: "fake:c", Group: "capture", Params: []backend.ParamSpec{{ID: "mute", Kind: backend.ParamToggle, Readable: true}}},
	}, values: map[string]backend.Value{}}
	rt := New(map[string]backend.Backend{"fake": fb}, map[int]Assignment{
		0: {Target: "fake:a", Params: map[surface.Role]string{surface.RoleMute: "mute", surface.RoleSolo: "solo"}},
		1: {Target: "fake:b", Params: map[surface.Role]string{surface.RoleMute: "mute", surface.RoleSolo: "solo"}},
		2: {Target: "fake:c", Params: map[surface.Role]string{surface.RoleMute: "mute", surface.RoleSolo: "solo"}},
	})
	rt.Activate(context.Background())
	if err := rt.ToggleSolo(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if len(fb.calls) != 2 {
		t.Fatalf("mute calls = %+v, want two playback-group calls", fb.calls)
	}
	got := map[backend.TargetID]bool{}
	for _, call := range fb.calls {
		got[call.target] = call.value.B
	}
	if got["fake:a"] {
		t.Fatalf("soloed target was muted: %+v", fb.calls)
	}
	if !got["fake:b"] {
		t.Fatalf("same-group peer was not muted: %+v", fb.calls)
	}
	if _, ok := got["fake:c"]; ok {
		t.Fatalf("other group was touched: %+v", fb.calls)
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

func TestRouterPageActivationAndScrollWindow(t *testing.T) {
	fb := &fakeBackend{}
	rt := NewWithPages(map[string]backend.Backend{"fake": fb}, 2, nil, []Page{{
		Name:   "lights",
		Button: "play",
		Assignments: []Assignment{
			{Label: "A", Target: "fake:a", Params: map[surface.Role]string{surface.RoleFader: "value"}},
			{Label: "B", Target: "fake:b", Params: map[surface.Role]string{surface.RoleFader: "value"}},
			{Label: "C", Target: "fake:c", Params: map[surface.Role]string{surface.RoleFader: "value"}},
		},
	}})

	if !rt.HandleGlobal(context.Background(), "play", true) {
		t.Fatal("page button should be handled")
	}
	snap := rt.Snapshot()
	if len(snap) != 2 || snap[0].TargetID != "fake:a" || snap[1].TargetID != "fake:b" {
		t.Fatalf("initial page window = %+v", snap)
	}
	if info := rt.PageInfo(); !info.Active || info.Name != "lights" || info.Offset != 0 || info.Total != 3 {
		t.Fatalf("page info = %+v", info)
	}

	rt.HandleGlobal(context.Background(), "down", true)
	snap = rt.Snapshot()
	if len(snap) != 2 || snap[0].TargetID != "fake:b" || snap[1].TargetID != "fake:c" {
		t.Fatalf("scrolled page window = %+v", snap)
	}
	rt.HandleGlobal(context.Background(), "down", true)
	if info := rt.PageInfo(); info.Offset != 1 {
		t.Fatalf("offset after clamp = %d, want 1", info.Offset)
	}
}

func TestRouterPageScrollResetsReadablePickup(t *testing.T) {
	fb := readableFake(backend.ParamContinuous, backend.Value{F: 0.5})
	fb.infos = append(fb.infos, backend.TargetInfo{
		ID:     "fake:other",
		Label:  "Other",
		Params: []backend.ParamSpec{{ID: "value", Kind: backend.ParamContinuous, Readable: true}},
	})
	fb.values["fake:other/value"] = backend.Value{F: 0.9}
	rt := NewWithPages(map[string]backend.Backend{"fake": fb}, 1, nil, []Page{{
		Name:   "lights",
		Button: "play",
		Assignments: []Assignment{
			{Target: "fake:brightness", Params: map[surface.Role]string{surface.RoleFader: "value"}},
			{Target: "fake:other", Params: map[surface.Role]string{surface.RoleFader: "value"}},
		},
	}})

	rt.HandleGlobal(context.Background(), "play", true)
	rt.HandleEvent(context.Background(), surface.Event{Strip: 0, Role: surface.RoleFader, Value: 0.49})
	if len(fb.calls) != 1 {
		t.Fatalf("first assignment should sync/write once, calls=%+v", fb.calls)
	}

	rt.HandleGlobal(context.Background(), "down", true)
	rt.HandleEvent(context.Background(), surface.Event{Strip: 0, Role: surface.RoleFader, Value: 0.5})
	if len(fb.calls) != 1 {
		t.Fatalf("new assignment wrote before pickup, calls=%+v", fb.calls)
	}
	snap := rt.Snapshot()
	if snap[0].Params["value"].Synced {
		t.Fatalf("new assignment should start unsynced: %+v", snap[0].Params["value"])
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
