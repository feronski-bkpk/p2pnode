package filetransfer

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
)

type FileID [16]byte

func NewFileID() (FileID, error) {
	var id FileID
	if _, err := rand.Read(id[:]); err != nil {
		return id, fmt.Errorf("filetransfer: rand: %w", err)
	}
	return id, nil
}

func (id FileID) Short() string {
	return fmt.Sprintf("%x", id[:4])
}

func (id FileID) String() string {
	return fmt.Sprintf("%x", id[:])
}

const ChunkSize = 32 * 1024

const MaxFileSize = 100 * 1024 * 1024

type Header struct {
	FileID      FileID `msgpack:"file_id"`
	Name        string `msgpack:"name"`
	TotalSize   uint64 `msgpack:"total_size"`
	TotalChunks uint32 `msgpack:"total_chunks"`
	SHA256      []byte `msgpack:"sha256"`
	ChunkSize   uint32 `msgpack:"chunk_size"`
}

type Chunk struct {
	FileID      FileID `msgpack:"file_id"`
	ChunkIndex  uint32 `msgpack:"chunk_index"`
	ChunkSHA256 []byte `msgpack:"chunk_sha256"`
	Data        []byte `msgpack:"data"`
}

type Ack struct {
	FileID     FileID `msgpack:"file_id"`
	ChunkIndex uint32 `msgpack:"chunk_index"`
	OK         bool   `msgpack:"ok"`
	Error      string `msgpack:"error,omitempty"`
}

type Done struct {
	FileID  FileID `msgpack:"file_id"`
	SHA256  []byte `msgpack:"sha256"`
	Success bool   `msgpack:"success"`
	Error   string `msgpack:"error,omitempty"`
}

type Progress struct {
	FileID      FileID
	TotalChunks uint32
	AckedChunks uint32
	BytesSent   uint64
}

func (p Progress) Percent() int {
	if p.TotalChunks == 0 {
		return 0
	}
	return int(p.AckedChunks * 100 / p.TotalChunks)
}

func EncodeAck(a Ack) []byte {
	buf := make([]byte, 21)
	copy(buf[0:16], a.FileID[:])
	binary.BigEndian.PutUint32(buf[16:20], a.ChunkIndex)
	if a.OK {
		buf[20] = 1
	}
	return buf
}

func DecodeAck(buf []byte) (Ack, error) {
	if len(buf) < 21 {
		return Ack{}, fmt.Errorf("filetransfer: ack too short: %d", len(buf))
	}
	var a Ack
	copy(a.FileID[:], buf[0:16])
	a.ChunkIndex = binary.BigEndian.Uint32(buf[16:20])
	a.OK = buf[20] == 1
	return a, nil
}
