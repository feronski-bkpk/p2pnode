package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	Version uint8 = 1

	HeaderSize = 24

	MaxFramePayload = 65536

	RequestIDLen = 16
)

type RequestID [RequestIDLen]byte

func (r RequestID) String() string {
	return fmt.Sprintf("%x", r[:])
}

type Frame struct {
	Version   uint8
	Type      MsgType
	Flags     uint16
	RequestID RequestID
	Payload   []byte
}

var (
	ErrBadVersion    = errors.New("protocol: bad version")
	ErrBadType       = errors.New("protocol: unknown message type")
	ErrPayloadTooBig = errors.New("protocol: payload exceeds MaxFramePayload")
	ErrShortHeader   = errors.New("protocol: short header")
	ErrShortPayload  = errors.New("protocol: short payload")
)

func (f Frame) WriteTo(w io.Writer) (int64, error) {
	if f.Version != Version {
		return 0, fmt.Errorf("%w: %d", ErrBadVersion, f.Version)
	}
	if f.Type == MsgInvalid {
		return 0, fmt.Errorf("%w: 0x00", ErrBadType)
	}
	if len(f.Payload) > MaxFramePayload {
		return 0, fmt.Errorf("%w: %d > %d", ErrPayloadTooBig, len(f.Payload), MaxFramePayload)
	}

	var hdr [HeaderSize]byte
	hdr[0] = f.Version
	hdr[1] = byte(f.Type)
	binary.BigEndian.PutUint16(hdr[2:4], f.Flags)
	copy(hdr[4:4+RequestIDLen], f.RequestID[:])
	binary.BigEndian.PutUint32(hdr[20:24], uint32(len(f.Payload)))

	n, err := w.Write(hdr[:])
	if err != nil {
		return int64(n), fmt.Errorf("protocol: write header: %w", err)
	}
	total := int64(n)
	if len(f.Payload) > 0 {
		m, err := w.Write(f.Payload)
		total += int64(m)
		if err != nil {
			return total, fmt.Errorf("protocol: write payload: %w", err)
		}
	}
	return total, nil
}

func ReadFrame(r io.Reader) (Frame, error) {
	var hdr [HeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return Frame{}, io.EOF
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return Frame{}, fmt.Errorf("%w: header", ErrShortHeader)
		}
		return Frame{}, fmt.Errorf("protocol: read header: %w", err)
	}

	version := hdr[0]
	if version != Version {
		return Frame{}, fmt.Errorf("%w: %d", ErrBadVersion, version)
	}

	msgType := MsgType(hdr[1])
	if !isKnownType(msgType) {
		return Frame{}, fmt.Errorf("%w: 0x%02x", ErrBadType, uint8(msgType))
	}

	flags := binary.BigEndian.Uint16(hdr[2:4])

	var reqID RequestID
	copy(reqID[:], hdr[4:4+RequestIDLen])

	payloadLen := binary.BigEndian.Uint32(hdr[20:24])
	if payloadLen > MaxFramePayload {
		return Frame{}, fmt.Errorf("%w: %d > %d", ErrPayloadTooBig, payloadLen, MaxFramePayload)
	}

	payload := make([]byte, payloadLen)
	if payloadLen > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return Frame{}, fmt.Errorf("%w: payload", ErrShortPayload)
			}
			return Frame{}, fmt.Errorf("protocol: read payload: %w", err)
		}
	}

	return Frame{
		Version:   version,
		Type:      msgType,
		Flags:     flags,
		RequestID: reqID,
		Payload:   payload,
	}, nil
}

func isKnownType(t MsgType) bool {
	switch t {
	case MsgPing, MsgPong,
		MsgFindNodeRequest, MsgFindNodeResponse,
		MsgStoreRequest, MsgStoreResponse,
		MsgFindValueRequest, MsgFindValueResponse,
		MsgHandshakeHello, MsgHandshakeReply, MsgHandshakeConfirm,
		MsgError:
		return true
	}
	return false
}
