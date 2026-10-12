package tunnel

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/vmihailenco/msgpack/v5"

	"p2pnode/internal/filetransfer"
	"p2pnode/internal/protocol"
)

type Session struct {
	Tunnel *Tunnel
	Conn   RawConn
	E2E    *E2ESession

	pendingMu sync.Mutex
	pending   map[MessageID]chan struct{}

	onMessage func(Message)
	onClosed  func(*Session)

	onFileHeader func(*filetransfer.Header)
	onFileChunk  func(filetransfer.Chunk)

	log *slog.Logger

	closeOnce sync.Once
	closeCh   chan struct{}
}

func NewSession(t *Tunnel, conn RawConn, e2e *E2ESession, log *slog.Logger) *Session {
	if log == nil {
		log = slog.Default()
	}
	return &Session{
		Tunnel:  t,
		Conn:    conn,
		E2E:     e2e,
		pending: make(map[MessageID]chan struct{}),
		log:     log,
		closeCh: make(chan struct{}),
	}
}

func (s *Session) SetOnMessage(cb func(Message)) {
	s.onMessage = cb
}

func (s *Session) SetFileCallbacks(
	onHeader func(*filetransfer.Header),
	onChunk func(filetransfer.Chunk),
) {
	s.onFileHeader = onHeader
	s.onFileChunk = onChunk
}

func (s *Session) Start() {
	go s.readLoop()
}

func (s *Session) Close() {
	s.closeOnce.Do(func() {
		close(s.closeCh)
		closePayload, _ := protocol.Encode(&protocol.TunnelClosePayload{
			TunnelID: s.Tunnel.ID.Array(),
			Reason:   "session closed",
		})
		_ = s.Conn.WriteFrame(protocol.Frame{
			Version: protocol.Version,
			Type:    protocol.MsgTunnelClose,
			Payload: closePayload,
		})
		_ = s.Conn.Close()
		s.Tunnel.SetState(StateClosing)
		s.Tunnel.SetState(StateDead)

		s.pendingMu.Lock()
		for id, ch := range s.pending {
			close(ch)
			delete(s.pending, id)
		}
		s.pendingMu.Unlock()

		if s.onClosed != nil {
			s.onClosed(s)
		}
	})
}

func (s *Session) SetOnClosed(cb func(*Session)) {
	s.onClosed = cb
}

func (s *Session) IsClosed() bool {
	select {
	case <-s.closeCh:
		return true
	default:
		return false
	}
}

func (s *Session) SendMessage(text string, destID [32]byte, localID [32]byte, ackTimeout time.Duration) (MessageID, error) {
	msgID, err := NewMessageID()
	if err != nil {
		return MessageID{}, err
	}
	if err := s.SendMessageWithID(msgID, text, ackTimeout); err != nil {
		return MessageID{}, err
	}
	return msgID, nil
}

func (s *Session) SendMessageWithID(msgID MessageID, text string, ackTimeout time.Duration) error {
	if !s.Tunnel.State().CanSend() {
		return fmt.Errorf("%w: state=%v", ErrTunnelNotActive, s.Tunnel.State())
	}
	select {
	case <-s.closeCh:
		return ErrTunnelClosed
	default:
	}

	env := protocol.InnerEnvelope{
		InnerType: protocol.InnerAppText,
		InnerData: []byte(text),
	}
	envBuf, err := msgpack.Marshal(env)
	if err != nil {
		return fmt.Errorf("tunnel: marshal envelope: %w", err)
	}

	return s.sendEnvelopeBytes(msgID, envBuf, ackTimeout)
}

func (s *Session) SendBytesWithID(msgID MessageID, payload []byte, ackTimeout time.Duration) error {
	if !s.Tunnel.State().CanSend() {
		return fmt.Errorf("%w: state=%v", ErrTunnelNotActive, s.Tunnel.State())
	}
	select {
	case <-s.closeCh:
		return ErrTunnelClosed
	default:
	}
	return s.sendEnvelopeBytes(msgID, payload, ackTimeout)
}

func (s *Session) sendEnvelopeBytes(msgID MessageID, envelope []byte, ackTimeout time.Duration) error {
	counter := s.E2E.NextSendNonce()
	ct, err := sealE2E(s.E2E.SendKey, counter, envelope, s.Tunnel.ID[:])
	if err != nil {
		return fmt.Errorf("tunnel: seal e2e: %w", err)
	}

	dataPayload := protocol.TunnelDataPayload{
		TunnelID:   s.Tunnel.ID.Array(),
		MessageID:  [16]byte(msgID),
		Ciphertext: ct,
	}
	payload, err := protocol.Encode(&dataPayload)
	if err != nil {
		return err
	}

	ch := make(chan struct{})
	s.pendingMu.Lock()
	if _, exists := s.pending[msgID]; exists {
		s.pendingMu.Unlock()
		return fmt.Errorf("tunnel: duplicate pending msgID %s", msgID.Short())
	}
	s.pending[msgID] = ch
	s.pendingMu.Unlock()

	if err := s.Conn.WriteFrame(protocol.Frame{
		Version: protocol.Version,
		Type:    protocol.MsgTunnelData,
		Payload: payload,
	}); err != nil {
		s.pendingMu.Lock()
		delete(s.pending, msgID)
		s.pendingMu.Unlock()
		return fmt.Errorf("tunnel: write data: %w", err)
	}

	s.Tunnel.IncSent()
	s.Tunnel.MarkUsed()

	select {
	case <-ch:
		select {
		case <-s.closeCh:
			return ErrTunnelClosed
		default:
			return nil
		}
	case <-time.After(ackTimeout):
		s.pendingMu.Lock()
		delete(s.pending, msgID)
		s.pendingMu.Unlock()
		return fmt.Errorf("tunnel: ack timeout for %s", msgID.Short())
	case <-s.closeCh:
		return ErrTunnelClosed
	}
}

func (s *Session) readLoop() {
	for {
		select {
		case <-s.closeCh:
			return
		default:
		}

		frame, err := s.Conn.ReadFrame()
		if err != nil {
			s.log.Debug("tunnel: session read end",
				"tunnel_id", s.Tunnel.ID.Short(),
				"err", err)
			s.Tunnel.SetState(StateDegraded)
			s.closeOnce.Do(func() {
				close(s.closeCh)
				_ = s.Conn.Close()
				s.pendingMu.Lock()
				for id, ch := range s.pending {
					close(ch)
					delete(s.pending, id)
				}
				s.pendingMu.Unlock()

				if s.onClosed != nil {
					s.onClosed(s)
				}
			})
			return
		}

		switch frame.Type {
		case protocol.MsgTunnelBuildAck:
			s.log.Debug("tunnel: late build ack",
				"tunnel_id", s.Tunnel.ID.Short())

		case protocol.MsgTunnelAck:
			var ack protocol.TunnelAckPayload
			if err := protocol.Decode(frame.Payload, &ack); err != nil {
				s.log.Warn("tunnel: bad ack", "err", err)
				continue
			}
			msgID := MessageID(ack.MessageID)
			s.pendingMu.Lock()
			ch, ok := s.pending[msgID]
			if ok {
				delete(s.pending, msgID)
			}
			s.pendingMu.Unlock()
			if ok {
				close(ch)
			}

		case protocol.MsgTunnelData:
			var data protocol.TunnelDataPayload
			if err := protocol.Decode(frame.Payload, &data); err != nil {
				s.log.Warn("tunnel: bad data", "err", err)
				continue
			}
			s.handleIncomingData(&data)

		case protocol.MsgTunnelClose:
			s.log.Info("tunnel: close received", "tunnel_id", s.Tunnel.ID.Short())
			s.Tunnel.SetState(StateClosing)
			s.Tunnel.SetState(StateDead)
			s.closeOnce.Do(func() {
				close(s.closeCh)
				_ = s.Conn.Close()
				s.pendingMu.Lock()
				for id, ch := range s.pending {
					close(ch)
					delete(s.pending, id)
				}
				s.pendingMu.Unlock()
			})
			return

		case protocol.MsgTunnelBuildFail:
			var fail protocol.TunnelBuildFailPayload
			_ = protocol.Decode(frame.Payload, &fail)
			s.log.Warn("tunnel: build fail",
				"tunnel_id", s.Tunnel.ID.Short(),
				"reason", fail.Reason)
			s.Tunnel.SetState(StateDead)
			s.closeOnce.Do(func() {
				close(s.closeCh)
				_ = s.Conn.Close()
				s.pendingMu.Lock()
				for id, ch := range s.pending {
					close(ch)
					delete(s.pending, id)
				}
				s.pendingMu.Unlock()
			})
			return

		default:
			s.log.Debug("tunnel: unexpected frame", "type", frame.Type)
		}
	}
}

func (s *Session) handleIncomingData(data *protocol.TunnelDataPayload) {
	if len(data.Ciphertext) < 8+16 {
		s.log.Warn("tunnel: short ciphertext", "len", len(data.Ciphertext))
		return
	}
	nonce := data.Ciphertext[:8]
	ct := data.Ciphertext[8:]

	if err := s.E2E.CheckRecvNonce(bytesToUint64(nonce)); err != nil {
		s.log.Warn("tunnel: nonce error", "err", err)
		return
	}

	plaintext, err := openE2E(s.E2E.RecvKey, nonce, ct, data.TunnelID[:])
	if err != nil {
		s.log.Warn("tunnel: decrypt error", "err", err)
		return
	}
	s.Tunnel.IncRecv()

	msgID := MessageID(data.MessageID)

	var env protocol.InnerEnvelope
	if err := msgpack.Unmarshal(plaintext, &env); err != nil {
		s.log.Debug("tunnel: legacy raw text received", "err", err)
		s.dispatchMessage(msgID, string(plaintext))
		return
	}

	switch env.InnerType {
	case protocol.InnerAppText:
		s.dispatchMessage(msgID, string(env.InnerData))

	case protocol.InnerFileHeader:
		var h protocol.FileHeaderPayload
		if err := msgpack.Unmarshal(env.InnerData, &h); err != nil {
			s.log.Warn("tunnel: bad file header", "err", err)
			return
		}
		if s.onFileHeader != nil {
			s.onFileHeader(&filetransfer.Header{
				FileID:      filetransfer.FileID(h.FileID),
				Name:        h.Name,
				TotalSize:   h.TotalSize,
				TotalChunks: h.TotalChunks,
				SHA256:      h.SHA256,
				ChunkSize:   h.ChunkSize,
			})
		}

	case protocol.InnerFileChunk:
		var c protocol.FileChunkPayload
		if err := msgpack.Unmarshal(env.InnerData, &c); err != nil {
			s.log.Warn("tunnel: bad file chunk", "err", err)
			return
		}
		if s.onFileChunk != nil {
			s.onFileChunk(filetransfer.Chunk{
				FileID:      filetransfer.FileID(c.FileID),
				ChunkIndex:  c.ChunkIndex,
				ChunkSHA256: c.ChunkSHA256,
				Data:        c.Data,
			})
		}

	case protocol.InnerFileAck:
		s.log.Debug("tunnel: file ack inner", "msg_id", msgID.Short())

	case protocol.InnerFileDone:
		s.log.Info("tunnel: file done inner", "msg_id", msgID.Short())

	default:
		s.log.Warn("tunnel: unknown inner type",
			"inner_type", env.InnerType, "msg_id", msgID.Short())
	}
}

func (s *Session) dispatchMessage(msgID MessageID, text string) {
	msg := Message{
		MessageID: msgID,
		FromID:    s.Tunnel.Dest.NodeID,
		ToID:      s.Tunnel.Initiator.NodeID,
		Text:      text,
		Timestamp: time.Now(),
		Delivered: true,
		Direction: Incoming,
	}
	if s.onMessage != nil {
		s.onMessage(msg)
	}
}

func (s *Session) TunnelID() ID { return s.Tunnel.ID }
