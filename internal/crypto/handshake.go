package crypto

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/vmihailenco/msgpack/v5"
)

const SessionIDLen = 16

const EphemeralKeyLen = 32

const IdentityKeyLen = ed25519.PublicKeySize

const RandomLen = 32

const ProtocolName = "p2pnode-ake-v1"

type SessionID [SessionIDLen]byte

func (s SessionID) String() string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(s)*2)
	for i, b := range s {
		out[i*2] = hexdigits[b>>4]
		out[i*2+1] = hexdigits[b&0x0F]
	}
	return string(out)
}

type HandshakeState struct {
	IdentityPub  ed25519.PublicKey
	IdentityPriv ed25519.PrivateKey

	EphemeralPub  []byte
	EphemeralPriv *ecdh.PrivateKey

	Random []byte

	IsInitiator bool

	SessionID SessionID
}

func NewHandshakeState(idPub ed25519.PublicKey, idPriv ed25519.PrivateKey, isInitiator bool) (*HandshakeState, error) {
	if len(idPub) != IdentityKeyLen {
		return nil, fmt.Errorf("crypto: bad identity pubkey size %d", len(idPub))
	}
	if len(idPriv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("crypto: bad identity privkey size %d", len(idPriv))
	}

	ephPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("crypto: ephemeral keygen: %w", err)
	}
	ephPub := ephPriv.PublicKey().Bytes()

	rnd, err := RandomBytes(RandomLen)
	if err != nil {
		return nil, err
	}

	return &HandshakeState{
		IdentityPub:   idPub,
		IdentityPriv:  idPriv,
		EphemeralPub:  ephPub,
		EphemeralPriv: ephPriv,
		Random:        rnd,
		IsInitiator:   isInitiator,
	}, nil
}

type HelloMsg struct {
	InitiatorID    [32]byte `msgpack:"initiator_id"`
	IdentityPubKey []byte   `msgpack:"identity_pubkey"`
	EphemeralPub   []byte   `msgpack:"ephemeral_pub"`
	Random         []byte   `msgpack:"random"`
}

type ReplyMsg struct {
	ResponderID    [32]byte `msgpack:"responder_id"`
	IdentityPubKey []byte   `msgpack:"identity_pubkey"`
	EphemeralPub   []byte   `msgpack:"ephemeral_pub"`
	Random         []byte   `msgpack:"random"`
	SessionID      [16]byte `msgpack:"session_id"`
	Signature      []byte   `msgpack:"signature"`
}

type ConfirmMsg struct {
	Signature []byte `msgpack:"signature"`
}

type transcriptHelloReply struct {
	Protocol       string   `msgpack:"protocol"`
	InitiatorID    [32]byte `msgpack:"initiator_id"`
	ResponderID    [32]byte `msgpack:"responder_id"`
	InitiatorIDPub []byte   `msgpack:"initiator_id_pub"`
	ResponderIDPub []byte   `msgpack:"responder_id_pub"`
	InitiatorEph   []byte   `msgpack:"initiator_eph"`
	ResponderEph   []byte   `msgpack:"responder_eph"`
	InitiatorRand  []byte   `msgpack:"initiator_rand"`
	ResponderRand  []byte   `msgpack:"responder_rand"`
	SessionID      [16]byte `msgpack:"session_id"`
}

type transcriptFull = transcriptHelloReply

func (h *HandshakeState) BuildHello(initiatorID [32]byte) ([]byte, error) {
	if !h.IsInitiator {
		return nil, errors.New("crypto: BuildHello called on responder")
	}
	msg := HelloMsg{
		InitiatorID:    initiatorID,
		IdentityPubKey: h.IdentityPub,
		EphemeralPub:   h.EphemeralPub,
		Random:         h.Random,
	}
	return msgpack.Marshal(&msg)
}

func (h *HandshakeState) ProcessHello(data []byte) (initiatorID [32]byte, initiatorPubKey, initiatorEph, initiatorRandom []byte, err error) {
	if h.IsInitiator {
		return initiatorID, nil, nil, nil, errors.New("crypto: ProcessHello called on initiator")
	}
	var msg HelloMsg
	if err := msgpack.Unmarshal(data, &msg); err != nil {
		return initiatorID, nil, nil, nil, fmt.Errorf("crypto: unmarshal hello: %w", err)
	}
	if len(msg.IdentityPubKey) != IdentityKeyLen {
		return initiatorID, nil, nil, nil, fmt.Errorf("crypto: bad hello identity pubkey size")
	}
	if len(msg.EphemeralPub) != EphemeralKeyLen {
		return initiatorID, nil, nil, nil, fmt.Errorf("crypto: bad hello ephemeral pubkey size")
	}
	if len(msg.Random) != RandomLen {
		return initiatorID, nil, nil, nil, fmt.Errorf("crypto: bad hello random size")
	}
	return msg.InitiatorID, msg.IdentityPubKey, msg.EphemeralPub, msg.Random, nil
}

func (h *HandshakeState) BuildReply(
	initiatorID [32]byte,
	initiatorIDPub, initiatorEph, initiatorRand []byte,
	responderID [32]byte,
) ([]byte, error) {
	if h.IsInitiator {
		return nil, errors.New("crypto: BuildReply called on initiator")
	}

	sidBytes, err := RandomBytes(SessionIDLen)
	if err != nil {
		return nil, err
	}
	var sid SessionID
	copy(sid[:], sidBytes)
	h.SessionID = sid

	tr := transcriptHelloReply{
		Protocol:       ProtocolName,
		InitiatorID:    initiatorID,
		ResponderID:    responderID,
		InitiatorIDPub: initiatorIDPub,
		ResponderIDPub: h.IdentityPub,
		InitiatorEph:   initiatorEph,
		ResponderEph:   h.EphemeralPub,
		InitiatorRand:  initiatorRand,
		ResponderRand:  h.Random,
		SessionID:      sid,
	}
	trBytes, err := msgpack.Marshal(&tr)
	if err != nil {
		return nil, fmt.Errorf("crypto: marshal transcript: %w", err)
	}

	sig := ed25519.Sign(h.IdentityPriv, trBytes)

	msg := ReplyMsg{
		ResponderID:    responderID,
		IdentityPubKey: h.IdentityPub,
		EphemeralPub:   h.EphemeralPub,
		Random:         h.Random,
		SessionID:      sid,
		Signature:      sig,
	}
	return msgpack.Marshal(&msg)
}

func (h *HandshakeState) ProcessReply(
	data []byte,
	initiatorID [32]byte,
	expectedResponderID [32]byte,
) (responderID [32]byte, responderIDPub, responderEph, responderRand []byte, err error) {
	if !h.IsInitiator {
		return responderID, nil, nil, nil, errors.New("crypto: ProcessReply called on responder")
	}
	var msg ReplyMsg
	if err := msgpack.Unmarshal(data, &msg); err != nil {
		return responderID, nil, nil, nil, fmt.Errorf("crypto: unmarshal reply: %w", err)
	}
	if msg.ResponderID != expectedResponderID {
		return responderID, nil, nil, nil, fmt.Errorf("crypto: responder id mismatch: got %x, want %x",
			msg.ResponderID[:4], expectedResponderID[:4])
	}
	if len(msg.IdentityPubKey) != IdentityKeyLen {
		return responderID, nil, nil, nil, fmt.Errorf("crypto: bad reply identity pubkey size")
	}
	if len(msg.EphemeralPub) != EphemeralKeyLen {
		return responderID, nil, nil, nil, fmt.Errorf("crypto: bad reply ephemeral pubkey size")
	}
	if len(msg.Random) != RandomLen {
		return responderID, nil, nil, nil, fmt.Errorf("crypto: bad reply random size")
	}

	tr := transcriptHelloReply{
		Protocol:       ProtocolName,
		InitiatorID:    initiatorID,
		ResponderID:    msg.ResponderID,
		InitiatorIDPub: h.IdentityPub,
		ResponderIDPub: msg.IdentityPubKey,
		InitiatorEph:   h.EphemeralPub,
		ResponderEph:   msg.EphemeralPub,
		InitiatorRand:  h.Random,
		ResponderRand:  msg.Random,
		SessionID:      msg.SessionID,
	}
	trBytes, err := msgpack.Marshal(&tr)
	if err != nil {
		return responderID, nil, nil, nil, fmt.Errorf("crypto: marshal transcript: %w", err)
	}

	if !ed25519.Verify(ed25519.PublicKey(msg.IdentityPubKey), trBytes, msg.Signature) {
		return responderID, nil, nil, nil, ErrHandshakeBadSignature
	}

	h.SessionID = SessionID(msg.SessionID)
	return msg.ResponderID, msg.IdentityPubKey, msg.EphemeralPub, msg.Random, nil
}

func (h *HandshakeState) BuildConfirm(
	initiatorID [32]byte,
	responderID [32]byte,
	responderIDPub, responderEph, responderRand []byte,
) ([]byte, error) {
	if !h.IsInitiator {
		return nil, errors.New("crypto: BuildConfirm called on responder")
	}
	tr := transcriptFull{
		Protocol:       ProtocolName,
		InitiatorID:    initiatorID,
		ResponderID:    responderID,
		InitiatorIDPub: h.IdentityPub,
		ResponderIDPub: responderIDPub,
		InitiatorEph:   h.EphemeralPub,
		ResponderEph:   responderEph,
		InitiatorRand:  h.Random,
		ResponderRand:  responderRand,
		SessionID:      h.SessionID,
	}
	trBytes, err := msgpack.Marshal(&tr)
	if err != nil {
		return nil, fmt.Errorf("crypto: marshal transcript: %w", err)
	}
	sig := ed25519.Sign(h.IdentityPriv, trBytes)
	msg := ConfirmMsg{Signature: sig}
	return msgpack.Marshal(&msg)
}

func (h *HandshakeState) ProcessConfirm(
	data []byte,
	initiatorID [32]byte,
	initiatorIDPub []byte,
	responderID [32]byte,
	initiatorEph, initiatorRand []byte,
) error {
	if h.IsInitiator {
		return errors.New("crypto: ProcessConfirm called on initiator")
	}
	var msg ConfirmMsg
	if err := msgpack.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("crypto: unmarshal confirm: %w", err)
	}
	tr := transcriptFull{
		Protocol:       ProtocolName,
		InitiatorID:    initiatorID,
		ResponderID:    responderID,
		InitiatorIDPub: initiatorIDPub,
		ResponderIDPub: h.IdentityPub,
		InitiatorEph:   initiatorEph,
		ResponderEph:   h.EphemeralPub,
		InitiatorRand:  initiatorRand,
		ResponderRand:  h.Random,
		SessionID:      h.SessionID,
	}
	trBytes, err := msgpack.Marshal(&tr)
	if err != nil {
		return fmt.Errorf("crypto: marshal transcript: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(initiatorIDPub), trBytes, msg.Signature) {
		return ErrHandshakeBadSignature
	}
	return nil
}

type SessionKeys struct {
	InitiatorToResponder []byte
	ResponderToInitiator []byte
	SessionID            SessionID
}

func DeriveKeys(
	selfEphPriv *ecdh.PrivateKey,
	peerEphPub []byte,
	selfIDPriv ed25519.PrivateKey,
	peerIDPub ed25519.PublicKey,
	transcriptBytes []byte,
	sessionID SessionID,
	isInitiator bool,
) (*SessionKeys, error) {
	peerEphKey, err := ecdh.X25519().NewPublicKey(peerEphPub)
	if err != nil {
		return nil, fmt.Errorf("crypto: peer ephemeral pubkey: %w", err)
	}

	peerIDAsXBytes, err := ed25519PublicKeyToX25519Bytes(peerIDPub)
	if err != nil {
		return nil, err
	}
	peerIDAsX, err := ecdh.X25519().NewPublicKey(peerIDAsXBytes)
	if err != nil {
		return nil, fmt.Errorf("crypto: peer id→x25519: %w", err)
	}

	selfIDAsXBytes, err := ed25519PrivateKeyToX25519Bytes(selfIDPriv)
	if err != nil {
		return nil, err
	}
	selfIDAsX, err := ecdh.X25519().NewPrivateKey(selfIDAsXBytes)
	if err != nil {
		return nil, fmt.Errorf("crypto: self id→x25519: %w", err)
	}

	dh_ee, err := selfEphPriv.ECDH(peerEphKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: dh_ee: %w", err)
	}

	var dh_es, dh_se []byte
	if isInitiator {
		dh_es, err = selfEphPriv.ECDH(peerIDAsX)
		if err != nil {
			return nil, fmt.Errorf("crypto: dh_es: %w", err)
		}
		dh_se, err = selfIDAsX.ECDH(peerEphKey)
		if err != nil {
			return nil, fmt.Errorf("crypto: dh_se: %w", err)
		}
	} else {
		dh_es, err = selfIDAsX.ECDH(peerEphKey)
		if err != nil {
			return nil, fmt.Errorf("crypto: dh_es(resp): %w", err)
		}
		dh_se, err = selfEphPriv.ECDH(peerIDAsX)
		if err != nil {
			return nil, fmt.Errorf("crypto: dh_se(resp): %w", err)
		}
	}

	dh_ss, err := selfIDAsX.ECDH(peerIDAsX)
	if err != nil {
		return nil, fmt.Errorf("crypto: dh_ss: %w", err)
	}

	ikm := make([]byte, 0, 32*4)
	ikm = append(ikm, dh_ee...)
	ikm = append(ikm, dh_es...)
	ikm = append(ikm, dh_se...)
	ikm = append(ikm, dh_ss...)

	salt := Sha256(transcriptBytes)

	kIR, err := HKDFDerive(ikm, salt, []byte("initiator→responder"), 32)
	if err != nil {
		return nil, err
	}
	kRI, err := HKDFDerive(ikm, salt, []byte("responder→initiator"), 32)
	if err != nil {
		return nil, err
	}

	return &SessionKeys{
		InitiatorToResponder: kIR,
		ResponderToInitiator: kRI,
		SessionID:            sessionID,
	}, nil
}

func TranscriptBytes(
	initiatorID, responderID [32]byte,
	initiatorIDPub, responderIDPub []byte,
	initiatorEph, responderEph []byte,
	initiatorRand, responderRand []byte,
	sessionID SessionID,
) ([]byte, error) {
	tr := transcriptHelloReply{
		Protocol:       ProtocolName,
		InitiatorID:    initiatorID,
		ResponderID:    responderID,
		InitiatorIDPub: initiatorIDPub,
		ResponderIDPub: responderIDPub,
		InitiatorEph:   initiatorEph,
		ResponderEph:   responderEph,
		InitiatorRand:  initiatorRand,
		ResponderRand:  responderRand,
		SessionID:      sessionID,
	}
	return msgpack.Marshal(&tr)
}

var ErrHandshakeBadSignature = errors.New("crypto: handshake signature verification failed")

func ed25519PublicKeyToX25519(pub ed25519.PublicKey) (*ecdh.PublicKey, error) {
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("crypto: bad ed25519 pubkey size")
	}
	return ecdh.X25519().NewPublicKey(pub)
}

func ed25519PrivateKeyToX25519(priv ed25519.PrivateKey) (*ecdh.PrivateKey, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("crypto: bad ed25519 privkey size")
	}
	seed := priv.Seed()
	return ecdh.X25519().NewPrivateKey(seed)
}
