package tcp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"p2pnode/internal/transport"
)

const (
	magic0 byte = 0x50
	magic1 byte = 0x32

	headerSize = 7

	MaxPayloadSize = 16 * 1024 * 1024
)

var (
	ErrBadMagic  = errors.New("tcp: bad magic")
	ErrBadType   = errors.New("tcp: bad msg type")
	ErrTooLarge  = errors.New("tcp: payload too large")
	ErrShortRead = errors.New("tcp: short read")
)

func WriteFrame(w io.Writer, f transport.Frame) error {
	if len(f.Payload) > MaxPayloadSize {
		return fmt.Errorf("%w: %d > %d", ErrTooLarge, len(f.Payload), MaxPayloadSize)
	}
	if f.Type == transport.MsgInvalid {
		return fmt.Errorf("%w: 0x00", ErrBadType)
	}

	var hdr [headerSize]byte
	hdr[0] = magic0
	hdr[1] = magic1
	hdr[2] = byte(f.Type)
	binary.BigEndian.PutUint32(hdr[3:7], uint32(len(f.Payload)))

	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("tcp: write header: %w", err)
	}
	if len(f.Payload) > 0 {
		if _, err := w.Write(f.Payload); err != nil {
			return fmt.Errorf("tcp: write payload: %w", err)
		}
	}
	return nil
}

func ReadFrame(r io.Reader) (transport.Frame, error) {
	var hdr [headerSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return transport.Frame{}, io.EOF
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return transport.Frame{}, fmt.Errorf("%w: header", ErrShortRead)
		}
		return transport.Frame{}, fmt.Errorf("tcp: read header: %w", err)
	}

	if hdr[0] != magic0 || hdr[1] != magic1 {
		return transport.Frame{}, fmt.Errorf("%w: %02x %02x", ErrBadMagic, hdr[0], hdr[1])
	}

	t := transport.MsgType(hdr[2])
	if t == transport.MsgInvalid {
		return transport.Frame{}, fmt.Errorf("%w: 0x00", ErrBadType)
	}

	n := binary.BigEndian.Uint32(hdr[3:7])
	if n > MaxPayloadSize {
		return transport.Frame{}, fmt.Errorf("%w: %d > %d", ErrTooLarge, n, MaxPayloadSize)
	}

	payload := make([]byte, n)
	if n > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return transport.Frame{}, fmt.Errorf("%w: payload: %v", ErrShortRead, err)
		}
	}

	return transport.Frame{Type: t, Payload: payload}, nil
}
