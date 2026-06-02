//go:build integration_pipewire

package pipewire

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yfernandes/smc-mixer-tui/audio"
)

const integrationSampleRate = 48000

func TestCrossfaderBuildsSendBusGraphAndLeavesMastersIndependent(t *testing.T) {
	requireCommand(t, "pactl")
	requireCommand(t, "pw-dump")
	requireCommand(t, "pw-play")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tag := fmt.Sprintf("it_%d", time.Now().UnixNano())
	outA := "smc_" + tag + "_out_a"
	outB := "smc_" + tag + "_out_b"

	loadNullSink(t, ctx, outA)
	loadNullSink(t, ctx, outB)

	client := New()
	if err := client.SetSinkVolume(ctx, outA, 0.37); err != nil {
		t.Fatalf("set output A master volume: %v", err)
	}
	if err := client.SetSinkVolume(ctx, outB, 0.73); err != nil {
		t.Fatalf("set output B master volume: %v", err)
	}

	tone := filepath.Join(t.TempDir(), "tone.raw")
	writeS16Sine(t, tone, 440, 8*time.Second)

	play := startPlayback(t, ctx, tone, outA)
	defer stopProcess(play)

	stream := waitForPlaybackStream(t, ctx, client, uint32(play.Process.Pid))

	routing, err := client.SetupCrossfader(ctx, tag, stream.ID, stream.NodeName, outA, outB)
	if err != nil {
		t.Fatalf("setup crossfader: %v", err)
	}
	defer client.TeardownCrossfader(context.Background(), routing)

	waitForSinkInputOnSink(t, ctx, routing.StreamSI, routing.NullSinkName)

	assertGainSplit(t, ctx, client, routing, outA, outB, 1.0, 0.0, "hard left")
	assertGainSplit(t, ctx, client, routing, outA, outB, 1.0, 1.0, "center")
	assertGainSplit(t, ctx, client, routing, outA, outB, 0.0, 1.0, "hard right")
}

func assertGainSplit(t *testing.T, ctx context.Context, client *Client, routing *CrossfaderRouting, outA, outB string, gainA, gainB float64, label string) {
	t.Helper()

	if err := client.SetCrossfaderGains(ctx, routing, gainA, gainB); err != nil {
		t.Fatalf("%s: set gains: %v", label, err)
	}
	gotGainA := getSinkVolume(t, ctx, routing.GainAName)
	gotGainB := getSinkVolume(t, ctx, routing.GainBName)
	gotOutA := getSinkVolume(t, ctx, outA)
	gotOutB := getSinkVolume(t, ctx, outB)

	if math.Abs(gotGainA-gainA) > 0.01 {
		t.Fatalf("%s: gain A volume %.2f, want %.2f", label, gotGainA, gainA)
	}
	if math.Abs(gotGainB-gainB) > 0.01 {
		t.Fatalf("%s: gain B volume %.2f, want %.2f", label, gotGainB, gainB)
	}
	if math.Abs(gotOutA-0.37) > 0.01 {
		t.Fatalf("%s: output A master volume %.2f changed, want 0.37", label, gotOutA)
	}
	if math.Abs(gotOutB-0.73) > 0.01 {
		t.Fatalf("%s: output B master volume %.2f changed, want 0.73", label, gotOutB)
	}
}

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available: %v", name, err)
	}
}

func loadNullSink(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	out, err := exec.CommandContext(ctx, "pactl", "load-module", "module-null-sink", "sink_name="+name, "sink_properties=device.description="+name).CombinedOutput()
	if err != nil {
		t.Fatalf("load null sink %s: %v\n%s", name, err, out)
	}
	id := strings.TrimSpace(string(out))
	t.Cleanup(func() {
		_ = exec.Command("pactl", "unload-module", id).Run()
	})
}

func startPlayback(t *testing.T, ctx context.Context, rawPath, target string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(ctx, "pw-play",
		"--target", target,
		"--rate", strconv.Itoa(integrationSampleRate),
		"--channels", "2",
		"--format", "s16",
		"--raw",
		rawPath,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start pw-play: %v", err)
	}
	return cmd
}

func stopProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
	select {
	case <-waitProcess(cmd):
	case <-time.After(500 * time.Millisecond):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}

func waitProcess(cmd *exec.Cmd) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	return done
}

func waitForPlaybackStream(t *testing.T, ctx context.Context, client *Client, pid uint32) Stream {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		streams, err := client.ListStreams(ctx)
		if err == nil {
			for _, s := range streams {
				if s.Kind == audio.KindSource && s.PID == pid {
					return s
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("playback stream for pid %d not found", pid)
	return Stream{}
}

func waitForSinkInputOnSink(t *testing.T, ctx context.Context, index uint32, sinkName string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, err := sinkInputSinkName(ctx, index)
		if err == nil && got == sinkName {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	got, err := sinkInputSinkName(ctx, index)
	t.Fatalf("sink-input %d routed to %q, want %q (err=%v)", index, got, sinkName, err)
}

func sinkInputSinkName(ctx context.Context, index uint32) (string, error) {
	sinks, err := sinkNamesByID(ctx)
	if err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, "pactl", "list", "sink-inputs").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("pactl list sink-inputs: %w\n%s", err, out)
	}
	for _, block := range strings.Split(string(out), "\n\n") {
		if !strings.HasPrefix(block, fmt.Sprintf("Sink Input #%d\n", index)) {
			continue
		}
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "Sink: ") {
				continue
			}
			id := strings.TrimSpace(strings.TrimPrefix(line, "Sink: "))
			name, ok := sinks[id]
			if !ok {
				return "", fmt.Errorf("sink id %s not found", id)
			}
			return name, nil
		}
	}
	return "", fmt.Errorf("sink-input %d not found", index)
}

func sinkNamesByID(ctx context.Context) (map[string]string, error) {
	out, err := exec.CommandContext(ctx, "pactl", "list", "short", "sinks").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("pactl list short sinks: %w\n%s", err, out)
	}
	sinks := make(map[string]string)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			sinks[fields[0]] = fields[1]
		}
	}
	return sinks, nil
}

func getSinkVolume(t *testing.T, ctx context.Context, sinkName string) float64 {
	t.Helper()
	out, err := exec.CommandContext(ctx, "pactl", "get-sink-volume", sinkName).CombinedOutput()
	if err != nil {
		t.Fatalf("get sink volume %s: %v\n%s", sinkName, err, out)
	}
	return parseFirstVolumePercent(t, string(out))
}

func parseFirstVolumePercent(t *testing.T, out string) float64 {
	t.Helper()
	pct := strings.Index(out, "%")
	if pct < 0 {
		t.Fatalf("volume output missing percent: %q", out)
	}
	start := strings.LastIndex(out[:pct], "/")
	if start < 0 {
		t.Fatalf("volume output missing percent field: %q", out)
	}
	raw := strings.TrimSpace(out[start+1 : pct])
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		t.Fatalf("parse volume percent %q from %q: %v", raw, out, err)
	}
	return v / 100
}

func writeS16Sine(t *testing.T, path string, freq float64, duration time.Duration) {
	t.Helper()
	samples := int(duration.Seconds() * integrationSampleRate)
	data := make([]byte, samples*2*2)
	for i := 0; i < samples; i++ {
		v := int16(math.Sin(2*math.Pi*freq*float64(i)/integrationSampleRate) * 12000)
		off := i * 4
		binary.LittleEndian.PutUint16(data[off:], uint16(v))
		binary.LittleEndian.PutUint16(data[off+2:], uint16(v))
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write tone: %v", err)
	}
}
