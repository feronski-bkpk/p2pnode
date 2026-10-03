package tunnel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type Direction int

const (
	Incoming Direction = iota
	Outgoing
)

func (d Direction) String() string {
	switch d {
	case Incoming:
		return "incoming"
	case Outgoing:
		return "outgoing"
	default:
		return "unknown"
	}
}

const MessageIDLen = 16

type MessageID [MessageIDLen]byte

func NewMessageID() (MessageID, error) {
	var id MessageID
	if _, err := rand.Read(id[:]); err != nil {
		return id, fmt.Errorf("tunnel: random message id: %w", err)
	}
	return id, nil
}

func (m MessageID) String() string { return hex.EncodeToString(m[:]) }
func (m MessageID) Short() string  { return hex.EncodeToString(m[:4]) }

type Message struct {
	MessageID MessageID `json:"message_id"`
	FromID    [32]byte  `json:"from_id"`
	ToID      [32]byte  `json:"to_id"`
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
	Delivered bool      `json:"delivered"`
	Direction Direction `json:"direction"`
}

type MessageStore struct {
	mu       sync.RWMutex
	messages []Message
	seen     map[MessageID]bool
	maxSize  int
}

const DefaultMessageStoreSize = 1000

func NewMessageStore(maxSize int) *MessageStore {
	if maxSize <= 0 {
		maxSize = DefaultMessageStoreSize
	}
	return &MessageStore{
		messages: make([]Message, 0, maxSize),
		seen:     make(map[MessageID]bool, maxSize),
		maxSize:  maxSize,
	}
}

func (s *MessageStore) Add(m Message) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.seen[m.MessageID] {
		return false
	}
	s.seen[m.MessageID] = true
	s.messages = append(s.messages, m)

	if len(s.messages) > s.maxSize {
		old := s.messages[0]
		delete(s.seen, old.MessageID)
		s.messages = s.messages[1:]
	}
	return true
}

func (s *MessageStore) Seen(id MessageID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.seen[id]
}

func (s *MessageStore) MarkDelivered(id MessageID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.messages {
		if s.messages[i].MessageID == id {
			s.messages[i].Delivered = true
			return
		}
	}
}

func (s *MessageStore) All() []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Message, len(s.messages))
	copy(out, s.messages)
	return out
}

func (s *MessageStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.messages)
}

func (s *MessageStore) ExportJSON(path string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data := struct {
		Messages []Message `json:"messages"`
		Count    int       `json:"count"`
	}{
		Messages: s.messages,
		Count:    len(s.messages),
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("tunnel: marshal history: %w", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("tunnel: write history: %w", err)
	}
	return nil
}
