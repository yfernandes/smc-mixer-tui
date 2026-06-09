package midi

import (
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SeqPort is an ALSA sequencer client:port address.
type SeqPort struct {
	Client int
	Port   int
}

// Bridge connects an ALSA sequencer port to a snd-virmidi device bidirectionally.
// DevPath is the corresponding raw MIDI device (e.g. /dev/snd/midiC2D0).
type Bridge struct {
	DevPath string
	src     SeqPort
	dst     SeqPort
}

// Close disconnects the aconnect bridge.
func (b *Bridge) Close() {
	src := fmt.Sprintf("%d:%d", b.src.Client, b.src.Port)
	dst := fmt.Sprintf("%d:%d", b.dst.Client, b.dst.Port)
	exec.Command("aconnect", "-d", src, dst).Run() //nolint:errcheck
	exec.Command("aconnect", "-d", dst, src).Run() //nolint:errcheck
}

// BridgeSequencerPort finds an ALSA sequencer port whose client name contains
// nameHint, loads snd-virmidi if needed, bridges them bidirectionally, and
// returns a Bridge. Call Bridge.Close when done.
func BridgeSequencerPort(nameHint string) (*Bridge, error) {
	src, err := findSeqPort(nameHint)
	if err != nil {
		log.Printf("BLE MIDI bridge: findSeqPort(%q): %v", nameHint, err)
		return nil, err
	}
	log.Printf("BLE MIDI bridge: found sequencer port %d:%d", src.Client, src.Port)

	dst, dstName, err := findVirmidiClient()
	if err != nil {
		log.Printf("BLE MIDI bridge: virmidi not found, trying modprobe: %v", err)
		if modErr := exec.Command("modprobe", "snd-virmidi").Run(); modErr != nil {
			return nil, fmt.Errorf("snd-virmidi unavailable (try: modprobe snd-virmidi): %w", modErr)
		}
		time.Sleep(300 * time.Millisecond)
		dst, dstName, err = findVirmidiClient()
		if err != nil {
			return nil, fmt.Errorf("VirMIDI not found after modprobe: %w", err)
		}
	}
	log.Printf("BLE MIDI bridge: found virmidi client %d:%d (%s)", dst.Client, dst.Port, dstName)

	devPath, err := virmidiDevPath(dstName)
	if err != nil {
		return nil, err
	}
	log.Printf("BLE MIDI bridge: raw device %s", devPath)

	srcAddr := fmt.Sprintf("%d:%d", src.Client, src.Port)
	dstAddr := fmt.Sprintf("%d:%d", dst.Client, dst.Port)

	// Input: BLE → virmidi (readable from raw device)
	if out, err := exec.Command("aconnect", srcAddr, dstAddr).CombinedOutput(); err != nil {
		if !strings.Contains(string(out), "already subscribed") {
			return nil, fmt.Errorf("aconnect %s %s: %w: %s", srcAddr, dstAddr, err, out)
		}
	}
	log.Printf("BLE MIDI bridge: connected %s → %s", srcAddr, dstAddr)
	// Output: virmidi → BLE (LED writes reach the device); best-effort
	exec.Command("aconnect", dstAddr, srcAddr).Run() //nolint:errcheck

	return &Bridge{DevPath: devPath, src: src, dst: dst}, nil
}

var (
	clientLineRe  = regexp.MustCompile(`^client (\d+): '([^']+)'`)
	portLineRe    = regexp.MustCompile(`^\s+(\d+) '`)
	virmidiNameRe = regexp.MustCompile(`Virtual Raw MIDI (\d+)-(\d+)`)
)

func aconnectList() (string, error) {
	out, err := exec.Command("aconnect", "-l").Output()
	if err != nil {
		return "", fmt.Errorf("aconnect -l: %w", err)
	}
	return string(out), nil
}

func findSeqPort(nameHint string) (SeqPort, error) {
	output, err := aconnectList()
	if err != nil {
		return SeqPort{}, err
	}
	hint := strings.ToLower(nameHint)
	var currentClient int
	var inMatch bool
	for _, line := range strings.Split(output, "\n") {
		if m := clientLineRe.FindStringSubmatch(line); m != nil {
			currentClient, _ = strconv.Atoi(m[1])
			inMatch = strings.Contains(strings.ToLower(m[2]), hint)
		} else if inMatch {
			if m := portLineRe.FindStringSubmatch(line); m != nil {
				port, _ := strconv.Atoi(m[1])
				return SeqPort{Client: currentClient, Port: port}, nil
			}
		}
	}
	return SeqPort{}, fmt.Errorf("no sequencer port matching %q", nameHint)
}

func findVirmidiClient() (SeqPort, string, error) {
	output, err := aconnectList()
	if err != nil {
		return SeqPort{}, "", err
	}
	var currentClient int
	var currentName string
	for _, line := range strings.Split(output, "\n") {
		if m := clientLineRe.FindStringSubmatch(line); m != nil {
			currentClient, _ = strconv.Atoi(m[1])
			currentName = m[2]
		} else if strings.Contains(strings.ToLower(currentName), "virtual raw midi") {
			if m := portLineRe.FindStringSubmatch(line); m != nil {
				port, _ := strconv.Atoi(m[1])
				return SeqPort{Client: currentClient, Port: port}, currentName, nil
			}
		}
	}
	return SeqPort{}, "", fmt.Errorf("no VirMIDI sequencer client found")
}

// virmidiDevPath derives the raw MIDI device path from a VirMIDI client name
// (e.g. "VirMIDI 2-0" → "/dev/snd/midiC2D0").
func virmidiDevPath(clientName string) (string, error) {
	m := virmidiNameRe.FindStringSubmatch(clientName)
	if m == nil {
		return "", fmt.Errorf("unexpected VirMIDI client name: %q", clientName)
	}
	return fmt.Sprintf("/dev/snd/midiC%sD%s", m[1], m[2]), nil
}
