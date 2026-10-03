package tunnel

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/vmihailenco/msgpack/v5"

	"p2pnode/internal/crypto"
	"p2pnode/internal/protocol"
)

type BuildConfig struct {
	BuildTimeout time.Duration
	MaxHops      uint8
	TTL          time.Duration
}

func DefaultBuildConfig() BuildConfig {
	return BuildConfig{
		BuildTimeout: 15 * time.Second,
		MaxHops:      3,
		TTL:          5 * time.Minute,
	}
}

type BuildCoordinator struct {
	LocalID      [32]byte
	LocalAddr    string
	LocalPubKey  ed25519.PublicKey
	LocalPrivKey ed25519.PrivateKey

	Dial     func(addr string) (RawConn, error)
	FindDest func(destID [32]byte) (pubkey ed25519.PublicKey, addr string, err error)

	BuildConfig

	Log Logger
}

type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Debug(msg string, args ...any)
}

type BuildResult struct {
	Tunnel *Tunnel
	Path   []Hop
	E2E    *E2ESession
	Conn   RawConn

	ACKCount int
}

func (bc *BuildCoordinator) Build(destID [32]byte, path []Hop) (*BuildResult, error) {
	if len(path) < 3 {
		return nil, fmt.Errorf("%w: path too short (%d)", ErrBuildFailed, len(path))
	}
	if path[0].Type != HopInitiator {
		return nil, fmt.Errorf("%w: first hop must be initiator", ErrBuildFailed)
	}
	if path[len(path)-1].Type != HopDest {
		return nil, fmt.Errorf("%w: last hop must be dest", ErrBuildFailed)
	}

	destPub, _, err := bc.FindDest(destID)
	if err != nil {
		return nil, fmt.Errorf("tunnel: find dest: %w", err)
	}

	tunnelID, err := NewID()
	if err != nil {
		return nil, err
	}

	ephPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("tunnel: e2e ephemeral: %w", err)
	}
	ephPub := ephPriv.PublicKey().Bytes()

	randomI, err := crypto.RandomBytes(32)
	if err != nil {
		return nil, err
	}

	fullPath := make([]string, len(path))
	for i, h := range path {
		fullPath[i] = h.Addr
	}

	relay1 := path[1]
	conn, err := bc.Dial(relay1.Addr)
	if err != nil {
		return nil, fmt.Errorf("%w: dial relay1: %v", ErrBuildFailed, err)
	}

	ttlSec := int64(bc.TTL.Seconds())
	if ttlSec <= 0 {
		ttlSec = int64(DefaultBuildConfig().TTL.Seconds())
	}

	expectedACKs := len(path) - 2

	buildPayload := protocol.TunnelBuildPayload{
		TunnelID:   tunnelID,
		DestID:     destID,
		FullPath:   fullPath,
		HopIndex:   1,
		MaxHops:    uint8(expectedACKs),
		TTLSec:     ttlSec,
		E2EPubKey:  ephPub,
		E2ERandom:  randomI,
		InitID:     bc.LocalID,
		InitPubKey: bc.LocalPubKey,
		PathSoFar:  [][32]byte{bc.LocalID},
	}
	buildBytes, err := protocol.Encode(&buildPayload)
	if err != nil {
		conn.Close()
		return nil, err
	}

	if err := conn.WriteFrame(protocol.Frame{
		Version: protocol.Version,
		Type:    protocol.MsgTunnelBuild,
		Payload: buildBytes,
	}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("%w: write build: %v", ErrBuildFailed, err)
	}

	deadline := time.Now().Add(bc.BuildTimeout)

	var (
		acks      = 0
		buildOK   *protocol.TunnelBuildOKPayload
		buildFail *protocol.TunnelBuildFailPayload
		readErr   error
	)

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			readErr = fmt.Errorf("build timeout")
			break
		}

		type readResult struct {
			f   protocol.Frame
			err error
		}
		ch := make(chan readResult, 1)
		go func() {
			f, err := conn.ReadFrame()
			ch <- readResult{f, err}
		}()

		var frame protocol.Frame
		select {
		case r := <-ch:
			if r.err != nil {
				readErr = r.err
				break
			}
			frame = r.f
		case <-time.After(remaining):
			readErr = fmt.Errorf("build timeout")
			break
		}
		if readErr != nil {
			break
		}

		switch frame.Type {
		case protocol.MsgTunnelBuildAck:
			var ack protocol.TunnelBuildAckPayload
			if err := protocol.Decode(frame.Payload, &ack); err != nil {
				continue
			}
			acks++
			if bc.Log != nil {
				bc.Log.Info("tunnel: relay build ack",
					"tunnel_id", tunnelID.Short(),
					"hop_index", ack.HopIndex,
					"hop_id", fmt.Sprintf("%x", ack.HopID[:4]),
					"got", acks,
					"want", expectedACKs,
				)
			}

		case protocol.MsgTunnelBuildOK:
			var ok protocol.TunnelBuildOKPayload
			if err := protocol.Decode(frame.Payload, &ok); err != nil {
				readErr = fmt.Errorf("decode build ok: %w", err)
				break
			}
			buildOK = &ok

		case protocol.MsgTunnelBuildFail:
			var fail protocol.TunnelBuildFailPayload
			_ = protocol.Decode(frame.Payload, &fail)
			buildFail = &fail

		default:
		}

		if buildFail != nil {
			break
		}
		if buildOK != nil && acks >= expectedACKs {
			break
		}
	}

	if readErr != nil {
		conn.Close()
		return nil, fmt.Errorf("%w: %v", ErrBuildFailed, readErr)
	}
	if buildFail != nil {
		conn.Close()
		return nil, fmt.Errorf("%w: %s (hop %x)",
			ErrBuildFailed, buildFail.Reason, buildFail.FailedHop[:4])
	}
	if buildOK == nil {
		conn.Close()
		return nil, fmt.Errorf("%w: no build ok", ErrBuildFailed)
	}
	if acks < expectedACKs {
		conn.Close()
		return nil, fmt.Errorf("%w: only %d/%d relay acks",
			ErrBuildFailed, acks, expectedACKs)
	}
	if buildOK.DestID != destID {
		conn.Close()
		return nil, fmt.Errorf("%w: dest mismatch", ErrBuildFailed)
	}

	transcript, err := tunnelTranscript(tunnelID, bc.LocalID, destID,
		ephPub, buildOK.DestE2EPubKey, randomI, buildOK.DestE2ERandom)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !ed25519.Verify(destPub, transcript, buildOK.Signature) {
		conn.Close()
		return nil, fmt.Errorf("%w: bad dest signature", ErrBuildFailed)
	}

	destEphPub, err := ecdh.X25519().NewPublicKey(buildOK.DestE2EPubKey)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("tunnel: dest eph pub: %w", err)
	}
	dh, err := ephPriv.ECDH(destEphPub)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("tunnel: ecdh: %w", err)
	}

	salt := crypto.Sha256(transcript)
	kID, err := crypto.HKDFDerive(dh, salt, []byte("initiator→dest"), 32)
	if err != nil {
		conn.Close()
		return nil, err
	}
	kDI, err := crypto.HKDFDerive(dh, salt, []byte("dest→initiator"), 32)
	if err != nil {
		conn.Close()
		return nil, err
	}

	e2e := &E2ESession{SendKey: kID, RecvKey: kDI}

	initHop := path[0]
	destHop := path[len(path)-1]

	cfg := DefaultConfig()
	cfg.MaxHops = bc.MaxHops
	cfg.TTL = bc.TTL

	tun := NewTunnel(tunnelID, initHop, destHop, path, cfg)
	tun.Activate(e2e)

	if bc.Log != nil {
		bc.Log.Info("tunnel: build complete",
			"tunnel_id", tunnelID.Short(),
			"hops", expectedACKs,
			"acks_received", acks,
		)
	}

	return &BuildResult{
		Tunnel:   tun,
		Path:     path,
		E2E:      e2e,
		Conn:     conn,
		ACKCount: acks,
	}, nil
}

func tunnelTranscript(
	tunnelID ID,
	initID, destID [32]byte,
	initEph, destEph, initRandom, destRandom []byte,
) ([]byte, error) {
	tr := struct {
		Protocol   string   `msgpack:"protocol"`
		TunnelID   ID       `msgpack:"tunnel_id"`
		InitID     [32]byte `msgpack:"init_id"`
		DestID     [32]byte `msgpack:"dest_id"`
		InitEphPub []byte   `msgpack:"init_eph_pub"`
		DestEphPub []byte   `msgpack:"dest_eph_pub"`
		InitRandom []byte   `msgpack:"init_random"`
		DestRandom []byte   `msgpack:"dest_random"`
	}{
		Protocol:   "p2pnode-tunnel-e2e-v1",
		TunnelID:   tunnelID,
		InitID:     initID,
		DestID:     destID,
		InitEphPub: initEph,
		DestEphPub: destEph,
		InitRandom: initRandom,
		DestRandom: destRandom,
	}
	return msgpack.Marshal(&tr)
}

var (
	ErrBadHop = errors.New("tunnel: bad hop")
)
