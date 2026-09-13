package transport

import "io"

type MsgType uint8

const (
	MsgInvalid MsgType = 0x00

	// 0x01–0x0F — служебные транспорта
	MsgPing      MsgType = 0x01
	MsgPong      MsgType = 0x02
	MsgHandshake MsgType = 0x03

	// 0x10–0x1F — DHT
	MsgFindNode  MsgType = 0x10
	MsgFindValue MsgType = 0x11
	MsgStore     MsgType = 0x12

	// 0x20–0x2F — прикладной слой
	MsgText      MsgType = 0x20
	MsgFileMeta  MsgType = 0x21
	MsgFileChunk MsgType = 0x22
	MsgFileReq   MsgType = 0x23
)

type Frame struct {
	Type    MsgType
	Payload []byte
}

type Conn interface {
	ReadFrame() (Frame, error)
	WriteFrame(Frame) error
	RemoteAddr() string
	Close() error
}

type Listener interface {
	Accept() (Conn, error)
	Close() error
	Addr() string
}

type Transport interface {
	Listen(addr string) (Listener, error)
	Dial(addr string) (Conn, error)
}

var ErrClosed = io.ErrClosedPipe
