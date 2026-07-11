package shellexec

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/yfernandes/smc-mixer-tui/backend"
	"github.com/yfernandes/smc-mixer-tui/config"
)

const backendName = "exec"

type Backend struct {
	mu      sync.Mutex
	targets map[string]target
	workers map[workerKey]*worker
	period  time.Duration
}

type target struct {
	key string
	cfg config.ExecTargetConfig
}

type workerKey struct {
	target string
	param  string
}

type worker struct {
	ch chan backend.Value
}

func New(cfg map[string]config.ExecTargetConfig) *Backend {
	targets := make(map[string]target, len(cfg))
	for key, tc := range cfg {
		targets[key] = target{key: key, cfg: tc}
	}
	return &Backend{
		targets: targets,
		workers: make(map[workerKey]*worker),
		period:  20 * time.Millisecond,
	}
}

func (b *Backend) Name() string { return backendName }

func (b *Backend) Targets(context.Context) ([]backend.TargetInfo, error) {
	out := make([]backend.TargetInfo, 0, len(b.targets))
	for key, t := range b.targets {
		out = append(out, backend.TargetInfo{
			ID:    targetID(key),
			Label: t.cfg.Label,
			Params: []backend.ParamSpec{{
				ID:       "value",
				Kind:     backend.ParamContinuous,
				Readable: t.cfg.ReadCommand != "",
			}},
		})
	}
	return out, nil
}

func (b *Backend) Set(ctx context.Context, id backend.TargetID, param string, v backend.Value) error {
	if param == "" {
		param = "value"
	}
	if param != "value" {
		return fmt.Errorf("exec: unknown param %q", param)
	}
	t, ok := b.lookup(id)
	if !ok {
		return fmt.Errorf("exec: unknown target %q", id)
	}
	k := workerKey{target: t.key, param: param}
	b.mu.Lock()
	w := b.workers[k]
	if w == nil {
		w = &worker{ch: make(chan backend.Value, 1)}
		b.workers[k] = w
		go b.runWorker(ctx, t, w)
	}
	b.mu.Unlock()
	select {
	case <-w.ch:
	default:
	}
	w.ch <- v
	return nil
}

func (b *Backend) Get(context.Context, backend.TargetID, string) (backend.Value, bool, error) {
	return backend.Value{}, false, nil
}

func (b *Backend) runWorker(ctx context.Context, t target, w *worker) {
	ticker := time.NewTicker(b.period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			select {
			case v := <-w.ch:
				if err := runCommand(ctx, t.cfg, v.F); err != nil {
					fmt.Fprintf(os.Stderr, "shellexec %s: %v\n", t.key, err)
				}
			default:
			}
		}
	}
}

func (b *Backend) lookup(id backend.TargetID) (target, bool) {
	key := strings.TrimPrefix(string(id), backendName+":")
	t, ok := b.targets[key]
	return t, ok
}

func targetID(key string) backend.TargetID {
	return backend.TargetID(backendName + ":" + key)
}

func runCommand(ctx context.Context, cfg config.ExecTargetConfig, v float64) error {
	scaled := scaleValue(v, cfg.Scale)
	rendered := formatValue(scaled)
	command := strings.ReplaceAll(cfg.Command, "{{value}}", rendered)
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = append(os.Environ(), "SMC_VALUE="+rendered)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func scaleValue(v float64, scale []float64) float64 {
	if len(scale) != 2 {
		return v
	}
	return scale[0] + v*(scale[1]-scale[0])
}

func formatValue(v float64) string {
	if math.Abs(v-math.Round(v)) < 0.0000001 {
		return fmt.Sprintf("%.0f", math.Round(v))
	}
	return fmt.Sprintf("%.6g", v)
}
