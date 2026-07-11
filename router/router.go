package router

import (
	"context"
	"log"
	"strings"
	"sync"

	"github.com/yfernandes/smc-mixer-tui/backend"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/surface"
)

type Assignment struct {
	Label  string
	Target backend.TargetID
	Params map[surface.Role]string
}

type Page struct {
	Name        string
	Button      surface.Role
	Assignments []Assignment
	Offset      int
}

type Router struct {
	mu          sync.Mutex
	backends    map[string]backend.Backend
	assignments map[int]Assignment
	knobs       map[int]int
}

func New(backends map[string]backend.Backend, assignments map[int]Assignment) *Router {
	return &Router{
		backends:    backends,
		assignments: assignments,
		knobs:       make(map[int]int),
	}
}

func NewFromConfig(backends map[string]backend.Backend, cfg config.RouterConfig) (*Router, error) {
	assignments := make(map[int]Assignment, len(cfg.Assignments))
	for strip, ac := range cfg.Assignments {
		params := make(map[surface.Role]string, len(ac.Params))
		for role, param := range ac.Params {
			params[surface.Role(role)] = param
		}
		assignments[strip] = Assignment{
			Label:  ac.Label,
			Target: backend.TargetID(ac.Target),
			Params: params,
		}
	}
	return New(backends, assignments), nil
}

func (r *Router) HandleEvent(ctx context.Context, ev surface.Event) bool {
	if ev.Strip < 0 {
		return false
	}
	r.mu.Lock()
	a, ok := r.assignments[ev.Strip]
	if !ok {
		r.mu.Unlock()
		return false
	}
	param, ok := a.Params[ev.Role]
	if !ok {
		r.mu.Unlock()
		return false
	}
	v, ok := r.valueForLocked(ev)
	r.mu.Unlock()
	if !ok {
		return false
	}
	b, ok := r.backendFor(a.Target)
	if !ok {
		return false
	}
	if err := b.Set(ctx, a.Target, param, v); err != nil {
		log.Printf("router: %v", err)
	}
	return true
}

func (r *Router) valueForLocked(ev surface.Event) (backend.Value, bool) {
	switch ev.Role {
	case surface.RoleFader:
		return backend.Value{F: clamp01(ev.Value)}, true
	case surface.RoleKnob:
		next := clampInt(r.knobs[ev.Strip]+ev.Delta, 0, 127)
		r.knobs[ev.Strip] = next
		return backend.Value{F: float64(next) / 127.0}, true
	default:
		if !ev.Pressed {
			return backend.Value{}, false
		}
		return backend.Value{B: true}, true
	}
}

func (r *Router) backendFor(id backend.TargetID) (backend.Backend, bool) {
	name, _, ok := strings.Cut(string(id), ":")
	if !ok {
		return nil, false
	}
	b, ok := r.backends[name]
	return b, ok
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
