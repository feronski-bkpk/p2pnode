package protocol

import (
	"crypto/ed25519"
	"fmt"

	"github.com/vmihailenco/msgpack/v5"
)

type Contact struct {
	NodeID            [32]byte `msgpack:"node_id"`
	IdentityAlgorithm string   `msgpack:"identity_algorithm"`
	IdentityPublicKey []byte   `msgpack:"identity_public_key"`
	Host              string   `msgpack:"host"`
	Port              uint16   `msgpack:"port"`
}

func ValidateContact(c Contact) error {
	if len(c.IdentityPublicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("protocol: bad pubkey size: %d", len(c.IdentityPublicKey))
	}
	if c.IdentityAlgorithm != "ed25519" {
		return fmt.Errorf("protocol: unsupported identity_algorithm: %s", c.IdentityAlgorithm)
	}
	if c.Host == "" {
		return fmt.Errorf("protocol: empty host")
	}
	if c.Port == 0 {
		return fmt.Errorf("protocol: zero port")
	}
	return nil
}

type PingPayload struct {
	Sender      Contact `msgpack:"sender"`
	TimestampMs uint64  `msgpack:"timestamp_ms"`
}

type PongPayload struct {
	Responder            Contact `msgpack:"responder"`
	PingTimestampMs      uint64  `msgpack:"ping_timestamp_ms"`
	ResponderTimestampMs uint64  `msgpack:"responder_timestamp_ms"`
}

type FindNodeRequestPayload struct {
	Sender       Contact  `msgpack:"sender"`
	TargetNodeID [32]byte `msgpack:"target_node_id"`
}

type FindNodeResponsePayload struct {
	Responder    Contact   `msgpack:"responder"`
	TargetNodeID [32]byte  `msgpack:"target_node_id"`
	Contacts     []Contact `msgpack:"contacts"`
}

type ErrorPayload struct {
	Code    string `msgpack:"code"`
	Message string `msgpack:"message"`
}

func Encode(v any) ([]byte, error) {
	b, err := msgpack.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("protocol: msgpack encode: %w", err)
	}
	return b, nil
}

func Decode(data []byte, v any) error {
	if err := msgpack.Unmarshal(data, v); err != nil {
		return fmt.Errorf("protocol: msgpack decode: %w", err)
	}
	return nil
}
