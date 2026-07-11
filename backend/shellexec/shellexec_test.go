package shellexec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yfernandes/smc-mixer-tui/backend"
	"github.com/yfernandes/smc-mixer-tui/config"
)

func TestRunCommandSubstitutesValueAndEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out")
	err := runCommand(context.Background(), config.ExecTargetConfig{
		Command: "printf '%s/%s' '{{value}}' \"$SMC_VALUE\" > " + path,
		Scale:   []float64{0, 100},
	}, 0.42)
	if err != nil {
		t.Fatalf("runCommand: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "42/42" {
		t.Fatalf("output = %q, want 42/42", got)
	}
}

func TestSetCoalescesLatestValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out")
	b := New(map[string]config.ExecTargetConfig{
		"brightness": {
			Command: "printf '%s\\n' '{{value}}' >> " + path,
			Scale:   []float64{0, 100},
		},
	})
	b.period = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, v := range []float64{0.1, 0.2, 0.3, 0.4} {
		if err := b.Set(ctx, backend.TargetID("exec:brightness"), "value", backend.Value{F: v}); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(path)
		if strings.TrimSpace(string(data)) == "40" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, _ := os.ReadFile(path)
	t.Fatalf("coalesced output = %q, want latest value 40", string(data))
}

func TestGetRunsReadCommandAndParsesFirstFloat(t *testing.T) {
	b := New(map[string]config.ExecTargetConfig{
		"brightness": {
			Command:     "true",
			ReadCommand: "printf 'brightness: 42 percent\\n'",
			Scale:       []float64{0, 100},
		},
	})
	got, known, err := b.Get(context.Background(), backend.TargetID("exec:brightness"), "value")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !known {
		t.Fatal("Get known = false, want true")
	}
	if got.F != 0.42 {
		t.Fatalf("Get value = %v, want 0.42", got.F)
	}
}
