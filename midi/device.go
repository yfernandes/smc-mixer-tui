package midi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FindDevice returns the path of the first ALSA raw MIDI device whose card name
// contains nameHint (case-insensitive). Virtual (snd-virmidi) devices are always
// excluded. Pass an empty hint to return the first hardware device found.
func FindDevice(nameHint string) (string, error) {
	all, err := filepath.Glob("/dev/snd/midiC*D0")
	if err != nil || len(all) == 0 {
		return "", fmt.Errorf("no MIDI devices in /dev/snd")
	}

	cards, _ := os.ReadFile("/proc/asound/cards")
	lines := strings.Split(string(cards), "\n")

	// Exclude snd-virmidi devices so they are never mistaken for hardware.
	var hw []string
	for _, m := range all {
		var cardNum int
		fmt.Sscanf(filepath.Base(m), "midiC%dD0", &cardNum)
		prefix := fmt.Sprintf(" %d [", cardNum)
		for _, line := range lines {
			if strings.HasPrefix(line, prefix) && !strings.Contains(strings.ToLower(line), "virmidi") {
				hw = append(hw, m)
				break
			}
		}
	}
	if len(hw) == 0 {
		return "", fmt.Errorf("no hardware MIDI devices in /dev/snd")
	}

	if nameHint == "" || len(hw) == 1 {
		return hw[0], nil
	}

	hint := strings.ToLower(nameHint)
	for _, m := range hw {
		var cardNum int
		fmt.Sscanf(filepath.Base(m), "midiC%dD0", &cardNum)
		prefix := fmt.Sprintf(" %d [", cardNum)
		for _, line := range lines {
			if strings.HasPrefix(line, prefix) && strings.Contains(strings.ToLower(line), hint) {
				return m, nil
			}
		}
	}
	return "", fmt.Errorf("no MIDI device matching %q", nameHint)
}
