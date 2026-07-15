package main

import (
	"context"
	"testing"

	"github.com/yfernandes/smc-mixer-tui/backend"
	"github.com/yfernandes/smc-mixer-tui/midi"
	"github.com/yfernandes/smc-mixer-tui/router"
	"github.com/yfernandes/smc-mixer-tui/surface"
)

type fakeBackend struct{}

func (f *fakeBackend) Name() string { return "fake" }
func (f *fakeBackend) Targets(context.Context) ([]backend.TargetInfo, error) {
	return []backend.TargetInfo{{
		ID:     "fake:lamp",
		Label:  "Lamp",
		Params: []backend.ParamSpec{{ID: "value", Kind: backend.ParamContinuous}},
	}}, nil
}
func (f *fakeBackend) Set(context.Context, backend.TargetID, string, backend.Value) error {
	return nil
}
func (f *fakeBackend) Get(context.Context, backend.TargetID, string) (backend.Value, bool, error) {
	return backend.Value{}, false, nil
}

func TestRouteGlobalMsgArbitratesRouterButtonsAndScroll(t *testing.T) {
	rt := router.NewWithPages(map[string]backend.Backend{"fake": &fakeBackend{}}, 1, nil, []router.Page{{
		Name:   "lights",
		Button: "play",
		Assignments: []router.Assignment{{
			Target: "fake:lamp",
			Params: map[surface.Role]string{surface.RoleFader: "value"},
		}},
	}})

	if !routeGlobalMsg(context.Background(), rt, midi.GlobalMsg{Action: midi.ActionPlay, Pressed: true}) {
		t.Fatal("router page button press should route to router")
	}
	if !rt.ActivePage() {
		t.Fatal("router page should be active")
	}
	if !routeGlobalMsg(context.Background(), rt, midi.GlobalMsg{Action: midi.ActionDown, Pressed: true}) {
		t.Fatal("scroll press should route to router while router page is active")
	}
	if routeGlobalMsg(context.Background(), rt, midi.GlobalMsg{Action: midi.ActionRecord, Pressed: true}) {
		t.Fatal("dispatcher page button should not route to router")
	}
	if routeGlobalMsg(context.Background(), rt, midi.GlobalMsg{Action: midi.ActionPlay, Pressed: false}) {
		t.Fatal("button release should not route to router")
	}
}
