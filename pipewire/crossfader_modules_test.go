package pipewire

import "testing"

func TestStaleCrossfaderModulesMatchesOnlyTagModules(t *testing.T) {
	names := newCrossfaderSetup("ch0", "", "").names
	modules := []PulseModule{
		{ID: 1, Args: "sink_name=smc_ch0_void"},
		{ID: 2, Args: "source=smc_ch0_gain_a.monitor sink=sink_a"},
		{ID: 3, Args: "sink_name=smc_ch1_void"},
		{ID: 4, Args: "module unrelated"},
	}

	got := staleCrossfaderModules(modules, names)

	if !sameUint32s(got, []uint32{1, 2}) {
		t.Fatalf("staleCrossfaderModules() = %v, want [1 2]", got)
	}
}

func TestCrossfaderModuleLoadStepsOrder(t *testing.T) {
	setup := newCrossfaderSetup("ch2", "sink_a", "sink_b")
	steps := setup.moduleLoadSteps()

	if len(steps) != 7 {
		t.Fatalf("moduleLoadSteps() len = %d, want 7", len(steps))
	}
	wantLabels := []string{
		"null sink",
		"gain sink A",
		"gain sink B",
		"loopback A",
		"loopback B",
		"loopback 2A",
		"loopback 2B",
	}
	for i, want := range wantLabels {
		if steps[i].label != want {
			t.Fatalf("step %d label = %q, want %q", i, steps[i].label, want)
		}
	}
	if !contains(steps[5].args, "source=smc_ch2_gain_a.monitor sink=sink_a") {
		t.Fatalf("loopback 2A args = %q", steps[5].args)
	}
	if !contains(steps[6].args, "source=smc_ch2_gain_b.monitor sink=sink_b") {
		t.Fatalf("loopback 2B args = %q", steps[6].args)
	}
}
