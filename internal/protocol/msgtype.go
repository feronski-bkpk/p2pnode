package protocol

import "fmt"

type MsgType uint8

const (
	MsgInvalid MsgType = 0x00

	MsgPing             MsgType = 0x01
	MsgPong             MsgType = 0x02
	MsgFindNodeRequest  MsgType = 0x03
	MsgFindNodeResponse MsgType = 0x04

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
	case MsgError:
		return "ERROR"
	default:
		return fmt.Sprintf("MSG(0x%02x)", uint8(t))
	}
}

func (t MsgType) IsRequest() bool {
	switch t {
	case MsgPing, MsgFindNodeRequest:
		return true
	}
	return false
}

func (t MsgType) IsResponse() bool {
	switch t {
	case MsgPong, MsgFindNodeResponse:
		return true
	}
	return false
}
