package dht

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

const (
	// длина ID в байтах
	IDLen = 32
	// длина ID в битах.
	IDBits = IDLen * 8
)

type ID [IDLen]byte

func RandomID() (ID, error) {
	var id ID
	if _, err := rand.Read(id[:]); err != nil {
		return ID{}, fmt.Errorf("dht: random id: %w", err)
	}
	return id, nil
}

func IDFromBytes(b []byte) ID {
	return sha256.Sum256(b)
}

func IDFromHex(s string) (ID, error) {
	var id ID
	b, err := hex.DecodeString(s)
	if err != nil {
		return id, fmt.Errorf("dht: hex decode: %w", err)
	}
	if len(b) != IDLen {
		return id, fmt.Errorf("dht: want %d bytes, got %d", IDLen, len(b))
	}
	copy(id[:], b)
	return id, nil
}

func (id ID) String() string { return hex.EncodeToString(id[:]) }

func (id ID) IsZero() bool {
	for _, b := range id {
		if b != 0 {
			return false
		}
	}
	return true
}

func Distance(a, b ID) ID {
	var d ID
	for i := range d {
		d[i] = a[i] ^ b[i]
	}
	return d
}

func Compare(a, b ID) int {
	for i := 0; i < IDLen; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func CloserTo(target, a, b ID) bool {
	for i := 0; i < IDLen; i++ {
		da := target[i] ^ a[i]
		db := target[i] ^ b[i]
		if da != db {
			return da < db
		}
	}
	return false
}

func CommonPrefixLen(a, b ID) int {
	for i := 0; i < IDLen; i++ {
		x := a[i] ^ b[i]
		if x == 0 {
			continue
		}
		for bit := 7; bit >= 0; bit-- {
			if x&(1<<bit) != 0 {
				return i*8 + (7 - bit)
			}
		}
	}
	return IDBits
}

var ErrSameID = errors.New("dht: same id")
