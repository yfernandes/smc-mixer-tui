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
	stream := newMIDIStream(r)
	for {
		raw, err := stream.nextRawMessage()
		if err != nil {
			return err
		}
		if msg, ok := Classify(raw); ok {
			out <- msg
		}
	}
}

type midiStream struct {
	buf    *bufio.Reader
	status byte // running status
}

func newMIDIStream(r io.Reader) *midiStream {
	return &midiStream{buf: bufio.NewReaderSize(r, 64)}
}

func (s *midiStream) nextRawMessage() ([3]byte, error) {
	for {
		raw, ok, err := s.readRawMessage()
		if err != nil {
			return [3]byte{}, err
		}
		if ok {
			return raw, nil
		}
	}
}

func (s *midiStream) readRawMessage() ([3]byte, bool, error) {
	for {
		b, err := s.buf.ReadByte()
		if err != nil {
			return [3]byte{}, false, err
		}

		if b >= 0xf8 {
			continue
		}

		if b == 0xf0 {
			if err := s.skipSysEx(); err != nil {
				return [3]byte{}, false, err
			}
			continue
		}

		return s.messageFromLeadingByte(b)
	}
}

func (s *midiStream) skipSysEx() error {
	s.status = 0
	for {
		x, err := s.nextDataByte()
		if err != nil {
			return err
		}
		if x == 0xf7 {
			return nil
		}
	}
}

func (s *midiStream) messageFromLeadingByte(b byte) ([3]byte, bool, error) {
	var raw [3]byte
	var data1 byte

	if b >= 0x80 {
		s.status = b
		if dataBytes(s.status) <= 0 {
			return raw, false, nil
		}
		var err error
		data1, err = s.nextDataByte()
		if err != nil {
			return raw, false, err
		}
	} else {
		if s.status == 0 {
			return raw, false, nil
		}
		data1 = b
	}

	n := dataBytes(s.status)
	if n <= 0 {
		return raw, false, nil
	}
	raw[0] = s.status
	raw[1] = data1
	if n == 2 {
		var err error
		raw[2], err = s.nextDataByte()
		if err != nil {
			return raw, false, err
		}
	}
	return raw, true, nil
}

// nextDataByte skips real-time bytes (0xf8–0xff) that appear mid-message.
func (s *midiStream) nextDataByte() (byte, error) {
	for {
		b, err := s.buf.ReadByte()
		if err != nil {
			return 0, err
		}
		if b < 0xf8 {
			return b, nil
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
