package node

import (
	"crypto/sha256"
	"strings"
	"unicode"

	"p2pnode/internal/record"
	"p2pnode/internal/store"
)

const NodeKeyPrefix = "node:"

const AliasKeyPrefix = "alias:"

func NodeKeyForID(id record.NodeID) store.ID {
	h := sha256.New()
	h.Write([]byte(NodeKeyPrefix))
	h.Write(id[:])
	var out store.ID
	copy(out[:], h.Sum(nil))
	return out
}

func AliasKeyForName(name string) store.ID {
	normalized := normalizeAlias(name)
	h := sha256.New()
	h.Write([]byte(AliasKeyPrefix))
	h.Write([]byte(normalized))
	var out store.ID
	copy(out[:], h.Sum(nil))
	return out
}

func normalizeAlias(name string) string {
	name = strings.ToLower(name)

	name = strings.TrimSpace(name)

	var b strings.Builder
	prevSpace := false
	for _, r := range name {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
			continue
		}
		if !unicode.IsPrint(r) {
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return b.String()
}
