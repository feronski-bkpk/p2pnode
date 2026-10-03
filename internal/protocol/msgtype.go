package protocol

import "fmt"

type MsgType uint8

const (
	MsgInvalid MsgType = 0x00

	MsgPing             MsgType = 0x01
	MsgPong             MsgType = 0x02
	MsgFindNodeRequest  MsgType = 0x03
	MsgFindNodeResponse MsgType = 0x04

	MsgStoreRequest      MsgType = 0x05
	MsgStoreResponse     MsgType = 0x06
	MsgFindValueRequest  MsgType = 0x07
	MsgFindValueResponse MsgType = 0x08

	MsgHandshakeHello   MsgType = 0x09
	MsgHandshakeReply   MsgType = 0x0A
	MsgHandshakeConfirm MsgType = 0x0B

	MsgTunnelBuild     MsgType = 0x0C
	MsgTunnelBuildOK   MsgType = 0x0D
	MsgTunnelBuildFail MsgType = 0x0E
	MsgTunnelData      MsgType = 0x0F
	MsgTunnelAck       MsgType = 0x10
	MsgTunnelClose     MsgType = 0x11
	MsgTunnelBuildAck  MsgType = 0x12

	MsgError MsgType = 0x7F
)

func (t MsgType) String() string {
	switch t {
	case MsgPing:
		return "PING"
	case MsgPong:
		return "PONG"
	case MsgFindNodeRequest:
		return "FIND_NODE_REQUEST"
	case MsgFindNodeResponse:
		return "FIND_NODE_RESPONSE"
	case MsgStoreRequest:
		return "STORE_REQUEST"
	case MsgStoreResponse:
		return "STORE_RESPONSE"
	case MsgFindValueRequest:
		return "FIND_VALUE_REQUEST"
	case MsgFindValueResponse:
		return "FIND_VALUE_RESPONSE"
	case MsgHandshakeHello:
		return "HANDSHAKE_HELLO"
	case MsgHandshakeReply:
		return "HANDSHAKE_REPLY"
	case MsgHandshakeConfirm:
		return "HANDSHAKE_CONFIRM"
	case MsgTunnelBuild:
		return "TUNNEL_BUILD"
	case MsgTunnelBuildOK:
		return "TUNNEL_BUILD_OK"
	case MsgTunnelBuildFail:
		return "TUNNEL_BUILD_FAIL"
	case MsgTunnelBuildAck:
		return "TUNNEL_BUILD_ACK"
	case MsgTunnelData:
		return "TUNNEL_DATA"
	case MsgTunnelAck:
		return "TUNNEL_ACK"
	case MsgTunnelClose:
		return "TUNNEL_CLOSE"
	case MsgError:
		return "ERROR"
	default:
		return fmt.Sprintf("MSG(0x%02x)", uint8(t))
	}
}

func (t MsgType) IsRequest() bool {
	switch t {
	case MsgPing, MsgFindNodeRequest, MsgStoreRequest, MsgFindValueRequest,
		MsgHandshakeHello, MsgHandshakeReply, MsgHandshakeConfirm,
		MsgTunnelBuild, MsgTunnelData, MsgTunnelClose:
		return true
	}
	return false
}

func (t MsgType) IsResponse() bool {
	switch t {
	case MsgPong, MsgFindNodeResponse, MsgStoreResponse, MsgFindValueResponse,
		MsgTunnelBuildOK, MsgTunnelBuildFail, MsgTunnelBuildAck, MsgTunnelAck:
		return true
	}
	return false
}
