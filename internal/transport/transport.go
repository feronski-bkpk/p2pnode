package transport

import "io"

// MsgType — тип кадра. 1 байт, значения 0x00–0xFF.
// 0x00 зарезервирован под "invalid".
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

// Frame — единица обмена поверх транспорта.
type Frame struct {
	Type    MsgType
	Payload []byte
}

// Conn — абстракция соединения. Реализации: TCP (сейчас), UDP (потом).
// ReadFrame/WriteFrame атомарны на уровне кадра.
type Conn interface {
	ReadFrame() (Frame, error)
	WriteFrame(Frame) error
	RemoteAddr() string
	Close() error
}

// Listener — абстракция слушателя.
type Listener interface {
	Accept() (Conn, error)
	Close() error
	Addr() string
}

// Transport — фабрика соединений и слушателей.
type Transport interface {
	Listen(addr string) (Listener, error)
	Dial(addr string) (Conn, error)
}

// ErrClosed — соединение/слушатель закрыт.
var ErrClosed = io.ErrClosedPipe
