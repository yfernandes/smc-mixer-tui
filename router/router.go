package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/yfernandes/smc-mixer-tui/backend"
	"github.com/yfernandes/smc-mixer-tui/config"
	"github.com/yfernandes/smc-mixer-tui/dispatcher"
	"github.com/yfernandes/smc-mixer-tui/surface"
)

const defaultPollInterval = 2 * time.Second

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

type ParamState struct {
	Kind     backend.ParamKind
	Value    backend.Value
	Readable bool
	Synced   bool
}

type StripState struct {
	Strip    int
	Label    string
	Backend  string
	TargetID string
	Params   map[string]ParamState
	Ext      json.RawMessage
}

type Router struct {
	mu           sync.Mutex
	backends     map[string]backend.Backend
	assignments  map[int]Assignment
	knobs        map[int]int
	states       map[int]*assignState
	pollInterval time.Duration
	feedback     surface.FeedbackWriter
	onChange     func([]StripState)
}

type assignState struct {
	label       string
	backendName string
	specs       map[string]backend.ParamSpec
	params      map[string]*paramRuntime
	ext         json.RawMessage
}

type paramRuntime struct {
	lastValue   backend.Value
	synced      bool
	pickupSide  int8
	prevPos     float64
	remoteKnown bool
}

func New(backends map[string]backend.Backend, assignments map[int]Assignment) *Router {
	r := &Router{
		backends:     backends,
		assignments:  assignments,
		knobs:        make(map[int]int),
		states:       make(map[int]*assignState),
		pollInterval: defaultPollInterval,
	}
	for strip := range assignments {
		r.states[strip] = &assignState{
			specs:  make(map[string]backend.ParamSpec),
			params: make(map[string]*paramRuntime),
		}
	}
	return r
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

func (r *Router) SetChangeCallback(fn func([]StripState)) {
	r.mu.Lock()
	r.onChange = fn
	r.mu.Unlock()
}

func (r *Router) SetFeedbackWriter(w surface.FeedbackWriter) {
	r.mu.Lock()
	r.feedback = w
	snap := r.snapshotLocked()
	r.mu.Unlock()
	r.applyFeedback(snap)
}

func (r *Router) Activate(ctx context.Context) {
	r.refreshTargets(ctx)
	r.seedReadable(ctx)
	r.notify()
}

func (r *Router) Run(ctx context.Context) {
	for _, b := range r.backends {
		if w, ok := b.(backend.Watcher); ok {
			go func(b backend.Backend, w backend.Watcher) {
				ch := make(chan []backend.TargetInfo, 1)
				go w.Watch(ctx, ch)
				for infos := range ch {
					r.applyTargetInfos(b.Name(), infos)
					r.notify()
				}
			}(b, w)
		}
	}
	r.runPoller(ctx)
}

func (r *Router) Snapshot() []StripState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshotLocked()
}

func (r *Router) HandleEvent(ctx context.Context, ev surface.Event) bool {
	if ev.Strip < 0 {
		return false
	}
	a, param, spec, value, shouldSet := r.handleEventState(ev)
	if param == "" {
		return false
	}
	if !shouldSet {
		r.notify()
		return true
	}
	b, ok := r.backendFor(a.Target)
	if !ok {
		return false
	}
	if err := b.Set(ctx, a.Target, param, value); err != nil {
		log.Printf("router: %v", err)
		return true
	}
	if spec.Kind == backend.ParamToggle {
		r.notify()
	}
	return true
}

func (r *Router) SetParam(ctx context.Context, target, param string, value backend.Value) error {
	b, ok := r.backendFor(backend.TargetID(target))
	if !ok {
		return fmt.Errorf("router: unknown backend for target %q", target)
	}
	if err := b.Set(ctx, backend.TargetID(target), param, value); err != nil {
		return err
	}
	r.updateRemote(backend.TargetID(target), param, value)
	r.notify()
	return nil
}

func (r *Router) ToggleParam(ctx context.Context, target, param string) error {
	current, ok := r.cachedValue(backend.TargetID(target), param)
	if !ok {
		if b, found := r.backendFor(backend.TargetID(target)); found {
			if v, known, err := b.Get(ctx, backend.TargetID(target), param); err == nil && known {
				current = v
			}
		}
	}
	next := backend.Value{B: !current.B}
	return r.SetParam(ctx, target, param, next)
}

func (r *Router) handleEventState(ev surface.Event) (Assignment, string, backend.ParamSpec, backend.Value, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	a, ok := r.assignments[ev.Strip]
	if !ok {
		return Assignment{}, "", backend.ParamSpec{}, backend.Value{}, false
	}
	param, ok := a.Params[ev.Role]
	if !ok {
		return Assignment{}, "", backend.ParamSpec{}, backend.Value{}, false
	}
	st := r.stateForLocked(ev.Strip)
	spec := st.specFor(param)
	pr := st.paramFor(param)

	switch ev.Role {
	case surface.RoleFader:
		pos := clamp01(ev.Value)
		if spec.Readable {
			if !pr.remoteKnown {
				pr.lastValue = backend.Value{F: pos}
				pr.remoteKnown = true
			}
			if !moveSoftPickup(pr, pos, pr.lastValue.F) {
				return a, param, spec, backend.Value{}, false
			}
		} else {
			pr.synced = true
		}
		v := backend.Value{F: pos}
		pr.lastValue = v
		pr.remoteKnown = true
		return a, param, spec, v, true

	case surface.RoleKnob:
		next := clampInt(r.knobs[ev.Strip]+ev.Delta, 0, 127)
		r.knobs[ev.Strip] = next
		v := backend.Value{F: float64(next) / 127.0}
		pr.lastValue = v
		pr.remoteKnown = true
		pr.synced = true
		return a, param, spec, v, true

	default:
		if !ev.Pressed {
			return a, param, spec, backend.Value{}, false
		}
		if spec.Kind == backend.ParamToggle {
			v := backend.Value{B: !pr.lastValue.B}
			pr.lastValue = v
			pr.remoteKnown = true
			pr.synced = true
			return a, param, spec, v, true
		}
		v := backend.Value{B: true}
		pr.lastValue = v
		pr.remoteKnown = true
		pr.synced = true
		return a, param, spec, v, true
	}
}

func moveSoftPickup(pr *paramRuntime, pos, target float64) bool {
	prev := pr.prevPos
	pr.prevPos = pos

	if pr.synced {
		return true
	}

	tol := dispatcher.PickupThreshold
	const sideBelow, sideAbove int8 = -1, 1

	if pr.pickupSide == 0 {
		switch {
		case pos < target-tol:
			pr.pickupSide = sideBelow
		case pos > target+tol:
			pr.pickupSide = sideAbove
		default:
			pr.synced = true
			return true
		}
	}

	switch pr.pickupSide {
	case sideBelow:
		if prev < target-tol && pos >= target-tol {
			pr.synced = true
			return true
		}
	case sideAbove:
		if prev > target+tol && pos <= target+tol {
			pr.synced = true
			return true
		}
	}
	return false
}

func (r *Router) runPoller(ctx context.Context) {
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if r.pollOnce(ctx) {
				r.notify()
			}
		}
	}
}

func (r *Router) pollOnce(ctx context.Context) bool {
	type item struct {
		strip int
		a     Assignment
		param string
	}
	var items []item
	r.mu.Lock()
	for strip, a := range r.assignments {
		st := r.stateForLocked(strip)
		seen := map[string]bool{}
		for _, param := range a.Params {
			if seen[param] {
				continue
			}
			seen[param] = true
			spec := st.specFor(param)
			if spec.Readable && !spec.Push {
				items = append(items, item{strip: strip, a: a, param: param})
			}
		}
	}
	r.mu.Unlock()

	changed := false
	for _, it := range items {
		b, ok := r.backendFor(it.a.Target)
		if !ok {
			continue
		}
		v, known, err := b.Get(ctx, it.a.Target, it.param)
		if err != nil {
			log.Printf("router poll %s %s: %v", it.a.Target, it.param, err)
			continue
		}
		if !known {
			continue
		}
		if r.setRemote(it.strip, it.param, v) {
			changed = true
		}
	}
	return changed
}

func (r *Router) refreshTargets(ctx context.Context) {
	for name, b := range r.backends {
		infos, err := b.Targets(ctx)
		if err != nil {
			log.Printf("router targets %s: %v", name, err)
			continue
		}
		r.applyTargetInfos(name, infos)
	}
}

func (r *Router) applyTargetInfos(name string, infos []backend.TargetInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for strip, a := range r.assignments {
		if backendName(a.Target) != name {
			continue
		}
		for _, info := range infos {
			if info.ID != a.Target {
				continue
			}
			st := r.stateForLocked(strip)
			st.backendName = name
			st.label = a.Label
			if st.label == "" {
				st.label = info.Label
			}
			st.ext = append(st.ext[:0], info.Ext...)
			for _, spec := range info.Params {
				st.specs[spec.ID] = spec
				st.paramFor(spec.ID)
			}
		}
	}
}

func (r *Router) seedReadable(ctx context.Context) {
	type item struct {
		strip int
		a     Assignment
		param string
	}
	var items []item
	r.mu.Lock()
	for strip, a := range r.assignments {
		st := r.stateForLocked(strip)
		for _, param := range a.Params {
			if st.specFor(param).Readable {
				items = append(items, item{strip: strip, a: a, param: param})
			}
		}
	}
	r.mu.Unlock()

	for _, it := range items {
		b, ok := r.backendFor(it.a.Target)
		if !ok {
			continue
		}
		v, known, err := b.Get(ctx, it.a.Target, it.param)
		if err != nil {
			log.Printf("router seed %s %s: %v", it.a.Target, it.param, err)
			continue
		}
		if known {
			r.setRemote(it.strip, it.param, v)
		}
	}
}

func (r *Router) setRemote(strip int, param string, v backend.Value) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.stateForLocked(strip)
	pr := st.paramFor(param)
	old := pr.lastValue
	oldKnown := pr.remoteKnown
	if oldKnown && old == v {
		return false
	}
	pr.lastValue = v
	pr.remoteKnown = true
	if !pr.synced {
		pr.pickupSide = 0
	}
	return true
}

func (r *Router) updateRemote(target backend.TargetID, param string, v backend.Value) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for strip, a := range r.assignments {
		if a.Target != target {
			continue
		}
		pr := r.stateForLocked(strip).paramFor(param)
		pr.lastValue = v
		pr.remoteKnown = true
		pr.synced = true
	}
}

func (r *Router) cachedValue(target backend.TargetID, param string) (backend.Value, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for strip, a := range r.assignments {
		if a.Target != target {
			continue
		}
		pr := r.stateForLocked(strip).paramFor(param)
		return pr.lastValue, pr.remoteKnown
	}
	return backend.Value{}, false
}

func (r *Router) notify() {
	r.mu.Lock()
	snap := r.snapshotLocked()
	cb := r.onChange
	r.mu.Unlock()
	r.applyFeedback(snap)
	if cb != nil {
		cb(snap)
	}
}

func (r *Router) applyFeedback(snap []StripState) {
	r.mu.Lock()
	fw := r.feedback
	assignments := make(map[int]Assignment, len(r.assignments))
	for strip, a := range r.assignments {
		assignments[strip] = a
	}
	r.mu.Unlock()
	if fw == nil {
		return
	}
	for _, strip := range snap {
		a := assignments[strip.Strip]
		for role, param := range a.Params {
			p, ok := strip.Params[param]
			if ok && p.Kind == backend.ParamToggle {
				fw.SetLED(strip.Strip, role, p.Value.B)
			}
		}
	}
}

func (r *Router) snapshotLocked() []StripState {
	out := make([]StripState, 0, len(r.assignments))
	for strip, a := range r.assignments {
		st := r.stateForLocked(strip)
		params := make(map[string]ParamState, len(a.Params))
		for _, param := range a.Params {
			spec := st.specFor(param)
			pr := st.paramFor(param)
			params[param] = ParamState{
				Kind:     spec.Kind,
				Value:    pr.lastValue,
				Readable: spec.Readable,
				Synced:   pr.synced || !spec.Readable,
			}
		}
		label := st.label
		if label == "" {
			label = a.Label
		}
		out = append(out, StripState{
			Strip:    strip,
			Label:    label,
			Backend:  st.backendName,
			TargetID: string(a.Target),
			Params:   params,
			Ext:      append(json.RawMessage(nil), st.ext...),
		})
	}
	return out
}

func (r *Router) stateForLocked(strip int) *assignState {
	st := r.states[strip]
	if st == nil {
		st = &assignState{specs: make(map[string]backend.ParamSpec), params: make(map[string]*paramRuntime)}
		r.states[strip] = st
	}
	if st.backendName == "" {
		st.backendName = backendName(r.assignments[strip].Target)
	}
	return st
}

func (s *assignState) specFor(param string) backend.ParamSpec {
	if spec, ok := s.specs[param]; ok {
		return spec
	}
	return backend.ParamSpec{ID: param, Kind: backend.ParamContinuous}
}

func (s *assignState) paramFor(param string) *paramRuntime {
	pr := s.params[param]
	if pr == nil {
		pr = &paramRuntime{}
		s.params[param] = pr
	}
	return pr
}

func (r *Router) backendFor(id backend.TargetID) (backend.Backend, bool) {
	name := backendName(id)
	b, ok := r.backends[name]
	return b, ok
}

func backendName(id backend.TargetID) string {
	name, _, ok := strings.Cut(string(id), ":")
	if !ok {
		return ""
	}
	return name
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
