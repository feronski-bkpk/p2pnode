package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"p2pnode/internal/identity"
)

type ID = identity.NodeID

func IDFromBytes(b []byte) ID {
	return ID(sha256.Sum256(b))
}

func IDFromHex(s string) (ID, error) {
	var id ID
	b, err := hex.DecodeString(s)
	if err != nil {
		return id, fmt.Errorf("routing: hex decode: %w", err)
	}
	if len(b) != len(id) {
		return id, fmt.Errorf("routing: want %d bytes, got %d", len(id), len(b))
	}
	copy(id[:], b)
	return id, nil
}

func Distance(a, b ID) ID {
	var d ID
	for i := range d {
		d[i] = a[i] ^ b[i]
	}
	return d
}

func CloserTo(target, a, b ID) bool {
	for i := range target {
		da := target[i] ^ a[i]
		db := target[i] ^ b[i]
		if da != db {
			return da < db
		}
	}
	return false
}

func Compare(a, b ID) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func CommonPrefixLen(a, b ID) int {
	for i := range a {
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
	return len(a) * 8
}

var ErrSameID = errors.New("routing: same id")
