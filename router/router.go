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

type PageInfo struct {
	Name   string
	Offset int
	Total  int
	Labels []string
	Active bool
}

type Router struct {
	mu           sync.Mutex
	backends     map[string]backend.Backend
	strips       int
	base         map[int]Assignment
	assignments  map[int]Assignment
	pages        []Page
	activePage   int
	knobs        map[int]int
	states       map[int]*assignState
	pollInterval time.Duration
	feedback     surface.FeedbackWriter
	onChange     func([]StripState, PageInfo)
}

type assignState struct {
	label          string
	backendName    string
	group          string
	soloMuted      bool
	muteBeforeSolo bool
	specs          map[string]backend.ParamSpec
	params         map[string]*paramRuntime
	ext            json.RawMessage
}

type paramRuntime struct {
	lastValue   backend.Value
	synced      bool
	pickupSide  int8
	prevPos     float64
	remoteKnown bool
}

func New(backends map[string]backend.Backend, assignments map[int]Assignment) *Router {
	return NewWithPages(backends, 8, assignments, nil)
}

func NewWithPages(backends map[string]backend.Backend, strips int, assignments map[int]Assignment, pages []Page) *Router {
	if strips <= 0 {
		strips = 8
	}
	base := cloneAssignments(assignments)
	r := &Router{
		backends:     backends,
		strips:       strips,
		base:         base,
		assignments:  cloneAssignments(base),
		pages:        clonePages(pages),
		activePage:   -1,
		knobs:        make(map[int]int),
		states:       make(map[int]*assignState),
		pollInterval: defaultPollInterval,
	}
	for strip := range r.assignments {
		r.states[strip] = &assignState{
			specs:  make(map[string]backend.ParamSpec),
			params: make(map[string]*paramRuntime),
		}
	}
	return r
}

func NewFromConfig(backends map[string]backend.Backend, cfg config.RouterConfig) (*Router, error) {
	return NewFromConfigWithStrips(backends, cfg, 8)
}

func NewFromConfigWithStrips(backends map[string]backend.Backend, cfg config.RouterConfig, strips int) (*Router, error) {
	assignments := make(map[int]Assignment, len(cfg.Assignments))
	for strip, ac := range cfg.Assignments {
		assignments[strip] = assignmentFromConfig(ac)
	}
	pages := make([]Page, 0, len(cfg.Pages))
	for _, pc := range cfg.Pages {
		page := Page{
			Name:        pc.Name,
			Button:      surface.Role(pc.Button),
			Assignments: make([]Assignment, 0, len(pc.Assignments)),
		}
		for _, ac := range pc.Assignments {
			page.Assignments = append(page.Assignments, assignmentFromConfig(ac))
		}
		pages = append(pages, page)
	}
	return NewWithPages(backends, strips, assignments, pages), nil
}

func assignmentFromConfig(ac config.AssignmentConfig) Assignment {
	params := make(map[surface.Role]string, len(ac.Params))
	for role, param := range ac.Params {
		params[surface.Role(role)] = param
	}
	return Assignment{
		Label:  ac.Label,
		Target: backend.TargetID(ac.Target),
		Params: params,
	}
}

func (r *Router) SetChangeCallback(fn func([]StripState, PageInfo)) {
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
					r.seedReadable(ctx)
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

func (r *Router) PageInfo() PageInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pageInfoLocked()
}

func (r *Router) ActivePage() bool {
	return r.PageInfo().Active
}

func (r *Router) OwnsStrip(strip int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.assignments[strip]
	return ok
}

func (r *Router) HasPageButton(role surface.Role) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pageIndexByButtonLocked(role) >= 0
}

func (r *Router) HandleGlobal(ctx context.Context, role surface.Role, pressed bool) bool {
	if !pressed {
		return false
	}
	r.mu.Lock()
	if idx := r.pageIndexByButtonLocked(role); idx >= 0 {
		oldAssignments := cloneAssignments(r.assignments)
		if r.activePage == idx {
			r.activePage = -1
			r.assignments = cloneAssignments(r.base)
		} else {
			r.activePage = idx
			r.pages[idx].Offset = clampOffset(r.pages[idx].Offset, len(r.pages[idx].Assignments), r.strips)
			r.assignments = r.pageWindowLocked(idx)
		}
		r.resetVisibleStateLocked()
		snap := r.snapshotLocked()
		r.mu.Unlock()
		r.clearDepartedFeedback(oldAssignments, snap)
		r.refreshVisible(ctx)
		r.notify()
		return true
	}
	if r.activePage >= 0 && isScrollRole(role) {
		oldAssignments := cloneAssignments(r.assignments)
		page := &r.pages[r.activePage]
		old := page.Offset
		switch role {
		case "up", "left":
			page.Offset--
		case "down", "right":
			page.Offset++
		}
		page.Offset = clampOffset(page.Offset, len(page.Assignments), r.strips)
		if page.Offset == old {
			r.mu.Unlock()
			return true
		}
		r.assignments = r.pageWindowLocked(r.activePage)
		r.resetVisibleStateLocked()
		snap := r.snapshotLocked()
		r.mu.Unlock()
		r.clearDepartedFeedback(oldAssignments, snap)
		r.refreshVisible(ctx)
		r.notify()
		return true
	}
	r.mu.Unlock()
	return false
}

func (r *Router) clearDepartedFeedback(old map[int]Assignment, snap []StripState) {
	r.mu.Lock()
	fw := r.feedback
	currentAssignments := cloneAssignments(r.assignments)
	r.mu.Unlock()
	if fw == nil {
		return
	}
	current := make(map[int]bool, len(snap))
	for _, s := range snap {
		current[s.Strip] = true
	}
	for strip, a := range old {
		if current[strip] && sameAssignment(a, currentAssignments[strip]) {
			continue
		}
		for role := range a.Params {
			fw.SetLED(strip, role, false)
		}
	}
}

func (r *Router) HandleEvent(ctx context.Context, ev surface.Event) bool {
	if ev.Strip < 0 {
		return false
	}
	if ev.Role == surface.RoleSolo && ev.Pressed {
		return r.ToggleSolo(ctx, ev.Strip) == nil
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

// ReassignStrip replaces the visible assignment target, used by legacy bind IPC
// while a router page is active. The page item is updated as well, so scrolling
// away and back preserves the ad-hoc binding.
func (r *Router) ReassignStrip(ctx context.Context, strip int, target backend.TargetID) error {
	r.mu.Lock()
	a, ok := r.assignments[strip]
	if !ok || r.activePage < 0 {
		r.mu.Unlock()
		return fmt.Errorf("router: strip %d is not router-owned", strip)
	}
	a.Target = target
	r.assignments[strip] = a
	idx := r.pages[r.activePage].Offset + strip
	if idx < len(r.pages[r.activePage].Assignments) {
		r.pages[r.activePage].Assignments[idx] = a
	}
	r.resetVisibleStateLocked()
	r.mu.Unlock()
	r.refreshVisible(ctx)
	r.notify()
	return nil
}

func (r *Router) ClearStrip(ctx context.Context, strip int) error {
	return r.ReassignStrip(ctx, strip, "")
}

func (r *Router) ToggleStripParam(ctx context.Context, strip int, role surface.Role) error {
	r.mu.Lock()
	a, ok := r.assignments[strip]
	param := a.Params[role]
	r.mu.Unlock()
	if !ok || param == "" {
		return fmt.Errorf("router: strip %d has no %s parameter", strip, role)
	}
	return r.ToggleParam(ctx, string(a.Target), param)
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
			st.group = info.Group
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
	info := r.pageInfoLocked()
	r.mu.Unlock()
	r.applyFeedback(snap)
	if cb != nil {
		cb(snap, info)
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

func (r *Router) refreshVisible(ctx context.Context) {
	r.refreshTargets(ctx)
	r.seedReadable(ctx)
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

func (r *Router) pageInfoLocked() PageInfo {
	if r.activePage < 0 || r.activePage >= len(r.pages) {
		return PageInfo{}
	}
	page := r.pages[r.activePage]
	labels := make([]string, len(page.Assignments))
	for i, a := range page.Assignments {
		labels[i] = a.Label
		if labels[i] == "" {
			labels[i] = string(a.Target)
		}
	}
	return PageInfo{Name: page.Name, Offset: page.Offset, Total: len(page.Assignments), Labels: labels, Active: true}
}

func (r *Router) pageIndexByButtonLocked(role surface.Role) int {
	for i, page := range r.pages {
		if page.Button == role {
			return i
		}
	}
	return -1
}

func (r *Router) pageWindowLocked(idx int) map[int]Assignment {
	page := r.pages[idx]
	out := make(map[int]Assignment)
	for strip := 0; strip < r.strips; strip++ {
		assignmentIdx := page.Offset + strip
		if assignmentIdx >= len(page.Assignments) {
			break
		}
		out[strip] = page.Assignments[assignmentIdx]
	}
	return out
}

func (r *Router) resetVisibleStateLocked() {
	for strip := range r.assignments {
		r.states[strip] = &assignState{
			specs:       make(map[string]backend.ParamSpec),
			params:      make(map[string]*paramRuntime),
			backendName: backendName(r.assignments[strip].Target),
			label:       r.assignments[strip].Label,
		}
	}
	for strip := range r.states {
		if _, ok := r.assignments[strip]; !ok {
			delete(r.states, strip)
		}
	}
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

func clampOffset(offset, total, strips int) int {
	max := total - strips
	if max < 0 {
		max = 0
	}
	return clampInt(offset, 0, max)
}

func isScrollRole(role surface.Role) bool {
	switch role {
	case "up", "down", "left", "right":
		return true
	default:
		return false
	}
}

func cloneAssignments(in map[int]Assignment) map[int]Assignment {
	if in == nil {
		return nil
	}
	out := make(map[int]Assignment, len(in))
	for strip, a := range in {
		params := make(map[surface.Role]string, len(a.Params))
		for role, param := range a.Params {
			params[role] = param
		}
		a.Params = params
		out[strip] = a
	}
	return out
}

func clonePages(in []Page) []Page {
	if in == nil {
		return nil
	}
	out := make([]Page, len(in))
	for i, page := range in {
		out[i] = page
		out[i].Assignments = make([]Assignment, len(page.Assignments))
		for j, a := range page.Assignments {
			params := make(map[surface.Role]string, len(a.Params))
			for role, param := range a.Params {
				params[role] = param
			}
			a.Params = params
			out[i].Assignments[j] = a
		}
	}
	return out
}

func sameAssignment(a, b Assignment) bool {
	if a.Label != b.Label || a.Target != b.Target || len(a.Params) != len(b.Params) {
		return false
	}
	for role, param := range a.Params {
		if b.Params[role] != param {
			return false
		}
	}
	return true
}
