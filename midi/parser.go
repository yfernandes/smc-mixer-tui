package midi

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"
)

// readMIDI parses a raw MIDI byte stream with running-status support.
// Real-time messages (0xf8–0xff) are silently ignored even when interleaved
// inside another message. SysEx messages are consumed and discarded.
func readMIDI(r io.Reader, out chan<- Msg) error {
	buf := bufio.NewReaderSize(r, 64)
	var status byte // running status

	nextByte := func() (byte, error) { return buf.ReadByte() }

	// nextDataByte skips real-time bytes (0xf8–0xff) that appear mid-message.
	nextDataByte := func() (byte, error) {
		for {
			b, err := buf.ReadByte()
			if err != nil {
				return 0, err
			}
			if b < 0xf8 {
				return b, nil
			}
		}
	}

	for {
		b, err := nextByte()
		if err != nil {
			return err
		}

		if b >= 0xf8 {
			continue
		}

		// SysEx: consume until End-of-Exclusive and clear running status.
		if b == 0xf0 {
			status = 0
			for {
				x, err := nextDataByte()
				if err != nil {
					return err
				}
				if x == 0xf7 {
					break
				}
			}
			continue
		}

		var raw [3]byte
		var data1 byte

		if b >= 0x80 {
			status = b
			n := dataBytes(status)
			if n <= 0 {
				continue
			}
			data1, err = nextDataByte()
			if err != nil {
				return err
			}
		} else {
			if status == 0 {
				continue
			}
			data1 = b
		}

		n := dataBytes(status)
		if n <= 0 {
			continue
		}
		raw[0] = status
		raw[1] = data1
		if n == 2 {
			raw[2], err = nextDataByte()
			if err != nil {
				return err
			}
		}

		if msg, ok := Classify(raw); ok {
			out <- msg
		}
	}
}

// dataBytes returns the number of data bytes that follow the given status byte.
func dataBytes(status byte) int {
	switch status >> 4 {
	case 0x8, 0x9, 0xa, 0xb, 0xe:
		return 2
	case 0xc, 0xd:
		return 1
	}
	switch status {
	case 0xf2:
		return 2
	case 0xf1, 0xf3:
		return 1
	}
	return 0
}

func isClosedErr(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, os.ErrClosed) ||
		strings.Contains(err.Error(), "use of closed") ||
		strings.Contains(err.Error(), "file already closed")
}
