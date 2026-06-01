package midi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FindDevice returns the path of the first ALSA raw MIDI device whose card name
// contains nameHint (case-insensitive). Pass an empty hint to return the first
// device found unconditionally.
func FindDevice(nameHint string) (string, error) {
	matches, err := filepath.Glob("/dev/snd/midiC*D0")
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no MIDI devices in /dev/snd")
	}
	if nameHint == "" || len(matches) == 1 {
		return matches[0], nil
	}

	cards, _ := os.ReadFile("/proc/asound/cards")
	hint := strings.ToLower(nameHint)
	lines := strings.Split(string(cards), "\n")

	for _, m := range matches {
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
