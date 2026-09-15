package routing

import (
	"fmt"
	"time"

	"p2pnode/internal/identity"
	"p2pnode/internal/protocol"
)

type Contact struct {
	NodeID            ID
	IdentityAlgorithm string
	IdentityPublicKey []byte
	Host              string
	Port              uint16

	LastSeenMs     uint64
	LastVerifiedMs uint64
}

func FromProtocol(p protocol.Contact) Contact {
	now := uint64(time.Now().UnixMilli())
	return Contact{
		NodeID:            ID(p.NodeID),
		IdentityAlgorithm: p.IdentityAlgorithm,
		IdentityPublicKey: p.IdentityPublicKey,
		Host:              p.Host,
		Port:              p.Port,
		LastSeenMs:        now,
		LastVerifiedMs:    0,
	}
}

func (c Contact) ToProtocol() protocol.Contact {
	var id [32]byte
	copy(id[:], c.NodeID[:])
	return protocol.Contact{
		NodeID:            id,
		IdentityAlgorithm: c.IdentityAlgorithm,
		IdentityPublicKey: c.IdentityPublicKey,
		Host:              c.Host,
		Port:              c.Port,
	}
}

func (c Contact) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func (c *Contact) Touch() {
	c.LastSeenMs = uint64(time.Now().UnixMilli())
}

func (c *Contact) MarkVerified() {
	now := uint64(time.Now().UnixMilli())
	c.LastSeenMs = now
	c.LastVerifiedMs = now
}

func (c Contact) Validate() error {
	p := c.ToProtocol()
	if err := protocol.ValidateContact(p); err != nil {
		return fmt.Errorf("routing: contact invalid: %w", err)
	}
	derived := identity.NodeIDFromPublicKey(c.IdentityPublicKey)
	if derived != c.NodeID {
		return fmt.Errorf("routing: node_id does not match pubkey")
	}
	return nil
}

func (c Contact) SameIdentity(other Contact) bool {
	if c.NodeID != other.NodeID {
		return false
	}
	return bytesEqual(c.IdentityPublicKey, other.IdentityPublicKey)
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
