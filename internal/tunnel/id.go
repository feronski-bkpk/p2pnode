package tunnel

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

const IDLen = 16

type ID [IDLen]byte

func NewID() (ID, error) {
	var id ID
	if _, err := rand.Read(id[:]); err != nil {
		return id, fmt.Errorf("tunnel: random id: %w", err)
	}
	return id, nil
}

func MustNewID() ID {
	id, err := NewID()
	if err != nil {
		panic(err)
	}
	return id
}

func (id ID) String() string { return hex.EncodeToString(id[:]) }

func (id ID) Short() string { return hex.EncodeToString(id[:4]) }

func (id ID) IsZero() bool {
	for _, b := range id {
		if b != 0 {
			return false
		}
	}
	return true
}

func (id ID) Bytes() []byte { return id[:] }

func IDFromBytes(b []byte) (ID, error) {
	var id ID
	if len(b) != IDLen {
		return id, fmt.Errorf("tunnel: bad id length %d", len(b))
	}
	copy(id[:], b)
	return id, nil
}

func IDFromArray(a [16]byte) ID { return ID(a) }

func (id ID) Array() [16]byte { return [16]byte(id) }
