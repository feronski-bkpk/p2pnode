package crypto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"p2pnode/internal/protocol"
	"p2pnode/internal/transport"
)

const (
	secureHeaderSize = SessionIDLen + AEADNonceLen + 4

	protocolHeaderAADLen = 20
)

var (
	ErrReplayDetected  = errors.New("crypto: replay detected")
	ErrSessionMismatch = errors.New("crypto: session_id mismatch")
)

type SecureConn struct {
	raw     transport.Conn
	session SessionID

	sendAEAD *AEAD
	recvAEAD *AEAD

	sendKey []byte
	recvKey []byte

	sendCounter uint64
	recvCounter uint64

	sendMu sync.Mutex
	recvMu sync.Mutex

	closed bool
	mu     sync.Mutex
}

func NewSecureConn(raw transport.Conn, sendKey, recvKey []byte, session SessionID) (*SecureConn, error) {
	if len(sendKey) != AEADKeyLen {
		return nil, fmt.Errorf("crypto: bad send key size %d", len(sendKey))
	}
	if len(recvKey) != AEADKeyLen {
		return nil, fmt.Errorf("crypto: bad recv key size %d", len(recvKey))
	}

	sendAEAD, err := NewAEAD(sendKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: send aead: %w", err)
	}
	recvAEAD, err := NewAEAD(recvKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: recv aead: %w", err)
	}

	sendCopy := append([]byte(nil), sendKey...)
	recvCopy := append([]byte(nil), recvKey...)

	return &SecureConn{
		raw:      raw,
		session:  session,
		sendAEAD: sendAEAD,
		recvAEAD: recvAEAD,
		sendKey:  sendCopy,
		recvKey:  recvCopy,
	}, nil
}

func (c *SecureConn) nonceFor(counter uint64) []byte {
	nonce := make([]byte, AEADNonceLen)
	copy(nonce[0:4], c.session[0:4])
	binary.BigEndian.PutUint64(nonce[4:12], counter)
	return nonce
}

func headerAAD(version uint8, msgType protocol.MsgType, flags uint16, reqID protocol.RequestID) []byte {
	aad := make([]byte, protocolHeaderAADLen)
	aad[0] = version
	aad[1] = byte(msgType)
	binary.BigEndian.PutUint16(aad[2:4], flags)
	copy(aad[4:20], reqID[:])
	return aad
}

func (c *SecureConn) WriteFrame(f protocol.Frame) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return transport.ErrClosed
	}
	c.mu.Unlock()

	c.sendMu.Lock()
	counter := c.sendCounter
	c.sendCounter++
	c.sendMu.Unlock()

	nonce := c.nonceFor(counter)
	aad := headerAAD(f.Version, f.Type, f.Flags, f.RequestID)

	ciphertext, err := c.sendAEAD.Seal(nonce, f.Payload, aad)
	if err != nil {
		return fmt.Errorf("crypto: seal: %w", err)
	}

	var hdr [secureHeaderSize]byte
	copy(hdr[0:SessionIDLen], c.session[:])
	copy(hdr[SessionIDLen:SessionIDLen+AEADNonceLen], nonce)
	binary.BigEndian.PutUint32(hdr[SessionIDLen+AEADNonceLen:], uint32(len(ciphertext)))

	combined := make([]byte, 0, secureHeaderSize+len(ciphertext))
	combined = append(combined, hdr[:]...)
	combined = append(combined, ciphertext...)

	if len(combined) > protocol.MaxFramePayload {
		return fmt.Errorf("crypto: combined frame too large: %d > %d",
			len(combined), protocol.MaxFramePayload)
	}

	out := protocol.Frame{
		Version:   f.Version,
		Type:      f.Type,
		Flags:     f.Flags,
		RequestID: f.RequestID,
		Payload:   combined,
	}
	return c.raw.WriteFrame(out)
}

func (c *SecureConn) ReadFrame() (protocol.Frame, error) {
	raw, err := c.raw.ReadFrame()
	if err != nil {
		return protocol.Frame{}, err
	}
	if len(raw.Payload) < secureHeaderSize {
		return protocol.Frame{}, fmt.Errorf("crypto: frame too short: %d", len(raw.Payload))
	}

	var session SessionID
	copy(session[:], raw.Payload[0:SessionIDLen])
	if session != c.session {
		return protocol.Frame{}, fmt.Errorf("%w: got %s, want %s",
			ErrSessionMismatch, session.String()[:8], c.session.String()[:8])
	}

	nonce := raw.Payload[SessionIDLen : SessionIDLen+AEADNonceLen]
	ciphertextLen := binary.BigEndian.Uint32(raw.Payload[SessionIDLen+AEADNonceLen : secureHeaderSize])
	if uint32(len(raw.Payload)-secureHeaderSize) != ciphertextLen {
		return protocol.Frame{}, fmt.Errorf("crypto: bad ciphertext length: %d vs %d",
			len(raw.Payload)-secureHeaderSize, ciphertextLen)
	}
	ciphertext := raw.Payload[secureHeaderSize:]

	counter := binary.BigEndian.Uint64(nonce[4:12])

	c.recvMu.Lock()
	if counter != c.recvCounter {
		c.recvMu.Unlock()
		if counter < c.recvCounter {
			return protocol.Frame{}, fmt.Errorf("%w: counter %d < %d",
				ErrReplayDetected, counter, c.recvCounter)
		}
		return protocol.Frame{}, fmt.Errorf(
			"crypto: unexpected nonce counter: got %d, want %d",
			counter, c.recvCounter)
	}
	c.recvCounter++
	c.recvMu.Unlock()

	aad := headerAAD(raw.Version, raw.Type, raw.Flags, raw.RequestID)
	plaintext, err := c.recvAEAD.Open(nonce, ciphertext, aad)
	if err != nil {
		return protocol.Frame{}, err
	}

	return protocol.Frame{
		Version:   raw.Version,
		Type:      raw.Type,
		Flags:     raw.Flags,
		RequestID: raw.RequestID,
		Payload:   plaintext,
	}, nil
}

func (c *SecureConn) RemoteAddr() string {
	return c.raw.RemoteAddr()
}

func (c *SecureConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true

	zeroBytes(c.sendKey)
	zeroBytes(c.recvKey)
	c.sendKey = nil
	c.recvKey = nil
	c.sendAEAD = nil
	c.recvAEAD = nil

	return c.raw.Close()
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
