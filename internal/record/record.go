package record

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/vmihailenco/msgpack/v5"
)

const NodeIDLen = 32

type NodeID [NodeIDLen]byte

func (id NodeID) String() string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(id)*2)
	for i, b := range id {
		out[i*2] = hexdigits[b>>4]
		out[i*2+1] = hexdigits[b&0x0F]
	}
	return string(out)
}

func (id NodeID) Short() string {
	s := id.String()
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func (id NodeID) IsZero() bool {
	for _, b := range id {
		if b != 0 {
			return false
		}
	}
	return true
}

func NodeIDFromPublicKey(pub ed25519.PublicKey) NodeID {
	return NodeID(sha256.Sum256(pub))
}

type NodeRecord struct {
	NodeID         NodeID    `msgpack:"node_id"`
	IdentityPubKey []byte    `msgpack:"identity_pubkey"`
	Addresses      []string  `msgpack:"addresses"`
	SequenceNumber uint64    `msgpack:"sequence_number"`
	IssuedAt       time.Time `msgpack:"issued_at"`
	ExpiresAt      time.Time `msgpack:"expires_at"`
	Alias          string    `msgpack:"alias,omitempty"`
	Signature      []byte    `msgpack:"signature"`
}

const MaxAddresses = 8

const MaxAliasLen = 64

var (
	ErrBadNodeID         = errors.New("record: node_id does not match pubkey")
	ErrBadPubKey         = errors.New("record: bad pubkey")
	ErrNoAddresses       = errors.New("record: no addresses")
	ErrTooManyAddresses  = errors.New("record: too many addresses")
	ErrBadAddress        = errors.New("record: bad address")
	ErrExpired           = errors.New("record: expired")
	ErrNotYetValid       = errors.New("record: not yet valid")
	ErrBadSignature      = errors.New("record: bad signature")
	ErrAliasTooLong      = errors.New("record: alias too long")
	ErrBadSequenceNumber = errors.New("record: bad sequence number")
)

func (r *NodeRecord) canonicalBytes() ([]byte, error) {
	tmp := struct {
		NodeID         NodeID    `msgpack:"node_id"`
		IdentityPubKey []byte    `msgpack:"identity_pubkey"`
		Addresses      []string  `msgpack:"addresses"`
		SequenceNumber uint64    `msgpack:"sequence_number"`
		IssuedAt       time.Time `msgpack:"issued_at"`
		ExpiresAt      time.Time `msgpack:"expires_at"`
		Alias          string    `msgpack:"alias,omitempty"`
	}{
		NodeID:         r.NodeID,
		IdentityPubKey: r.IdentityPubKey,
		Addresses:      r.Addresses,
		SequenceNumber: r.SequenceNumber,
		IssuedAt:       r.IssuedAt,
		ExpiresAt:      r.ExpiresAt,
		Alias:          r.Alias,
	}
	b, err := msgpack.Marshal(&tmp)
	if err != nil {
		return nil, fmt.Errorf("record: canonical encode: %w", err)
	}
	return b, nil
}

func (r *NodeRecord) CanonicalBytes() ([]byte, error) {
	return r.canonicalBytes()
}

func (r *NodeRecord) Sign(priv ed25519.PrivateKey) error {
	if len(priv) != ed25519.PrivateKeySize {
		return fmt.Errorf("%w: private key size %d", ErrBadPubKey, len(priv))
	}
	canonical, err := r.canonicalBytes()
	if err != nil {
		return err
	}
	r.Signature = ed25519.Sign(priv, canonical)
	return nil
}

func (r *NodeRecord) Validate(now time.Time) error {
	if len(r.IdentityPubKey) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: size %d", ErrBadPubKey, len(r.IdentityPubKey))
	}

	pub := ed25519.PublicKey(r.IdentityPubKey)
	derived := NodeIDFromPublicKey(pub)
	if derived != r.NodeID {
		return ErrBadNodeID
	}

	if len(r.Addresses) == 0 {
		return ErrNoAddresses
	}
	if len(r.Addresses) > MaxAddresses {
		return ErrTooManyAddresses
	}
	for _, a := range r.Addresses {
		if !validAddress(a) {
			return fmt.Errorf("%w: %q", ErrBadAddress, a)
		}
	}

	const clockSkewTolerance = 5 * time.Second

	if r.ExpiresAt.Before(now) {
		return ErrExpired
	}
	if r.IssuedAt.After(now.Add(clockSkewTolerance)) {
		return ErrNotYetValid
	}

	if len(r.Signature) != ed25519.SignatureSize {
		return fmt.Errorf("%w: size %d", ErrBadSignature, len(r.Signature))
	}
	canonical, err := r.canonicalBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, canonical, r.Signature) {
		return ErrBadSignature
	}

	if r.SequenceNumber == 0 {
		return ErrBadSequenceNumber
	}

	if len(r.Alias) > MaxAliasLen {
		return ErrAliasTooLong
	}

	return nil
}

func validAddress(a string) bool {
	if a == "" {
		return false
	}
	idx := -1
	for i := len(a) - 1; i >= 0; i-- {
		if a[i] == ':' {
			idx = i
			break
		}
	}
	if idx <= 0 || idx == len(a)-1 {
		return false
	}
	portStr := a[idx+1:]
	port := 0
	for _, ch := range portStr {
		if ch < '0' || ch > '9' {
			return false
		}
		port = port*10 + int(ch-'0')
		if port > 65535 {
			return false
		}
	}
	if port < 1 {
		return false
	}
	return true
}

func New(priv ed25519.PrivateKey, addresses []string, ttl time.Duration, seq uint64) (*NodeRecord, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: private key size %d", ErrBadPubKey, len(priv))
	}
	pub := priv.Public().(ed25519.PublicKey)
	now := time.Now()

	r := &NodeRecord{
		NodeID:         NodeIDFromPublicKey(pub),
		IdentityPubKey: pub,
		Addresses:      addresses,
		SequenceNumber: seq,
		IssuedAt:       now,
		ExpiresAt:      now.Add(ttl),
	}
	if err := r.Sign(priv); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *NodeRecord) Encode() ([]byte, error) {
	b, err := msgpack.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("record: encode: %w", err)
	}
	return b, nil
}

func Decode(data []byte) (*NodeRecord, error) {
	var r NodeRecord
	if err := msgpack.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("record: decode: %w", err)
	}
	return &r, nil
}

func IDFromHex(s string) (NodeID, error) {
	var id NodeID
	b, err := hex.DecodeString(s)
	if err != nil {
		return id, fmt.Errorf("record: hex decode: %w", err)
	}
	if len(b) != NodeIDLen {
		return id, fmt.Errorf("record: want %d bytes, got %d", NodeIDLen, len(b))
	}
	copy(id[:], b)
	return id, nil
}
