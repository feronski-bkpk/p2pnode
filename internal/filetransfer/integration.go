package filetransfer

import (
	"fmt"

	"github.com/vmihailenco/msgpack/v5"

	"p2pnode/internal/protocol"
)

func MarshalChunkEnvelope(c Chunk) ([]byte, error) {
	chunkBytes, err := msgpack.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("filetransfer: marshal chunk: %w", err)
	}
	env := protocol.InnerEnvelope{
		InnerType: protocol.InnerFileChunk,
		InnerData: chunkBytes,
	}
	return msgpack.Marshal(env)
}

func MarshalHeaderEnvelope(h Header) ([]byte, error) {
	headerBytes, err := msgpack.Marshal(h)
	if err != nil {
		return nil, fmt.Errorf("filetransfer: marshal header: %w", err)
	}
	env := protocol.InnerEnvelope{
		InnerType: protocol.InnerFileHeader,
		InnerData: headerBytes,
	}
	return msgpack.Marshal(env)
}

func UnmarshalHeader(buf []byte) (Header, error) {
	var h Header
	if err := msgpack.Unmarshal(buf, &h); err != nil {
		return h, fmt.Errorf("filetransfer: unmarshal header: %w", err)
	}
	return h, nil
}

func UnmarshalChunk(buf []byte) (Chunk, error) {
	var c Chunk
	if err := msgpack.Unmarshal(buf, &c); err != nil {
		return c, fmt.Errorf("filetransfer: unmarshal chunk: %w", err)
	}
	return c, nil
}
