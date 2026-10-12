package protocol

const FileIDLength = 16

const FileSHA256Length = 32

type InnerType uint8

const (
	InnerAppText InnerType = 0x01

	InnerFileHeader InnerType = 0x10

	InnerFileChunk InnerType = 0x11

	InnerFileAck InnerType = 0x12

	InnerFileDone InnerType = 0x13
)

func (t InnerType) String() string {
	switch t {
	case InnerAppText:
		return "APP_TEXT"
	case InnerFileHeader:
		return "FILE_HEADER"
	case InnerFileChunk:
		return "FILE_CHUNK"
	case InnerFileAck:
		return "FILE_ACK"
	case InnerFileDone:
		return "FILE_DONE"
	default:
		return "UNKNOWN"
	}
}

type InnerEnvelope struct {
	InnerType InnerType `msgpack:"inner_type"`
	InnerData []byte    `msgpack:"inner_data"`
}

type FileHeaderPayload struct {
	FileID      [FileIDLength]byte `msgpack:"file_id"`
	Name        string             `msgpack:"name"`
	TotalSize   uint64             `msgpack:"total_size"`
	TotalChunks uint32             `msgpack:"total_chunks"`
	SHA256      []byte             `msgpack:"sha256"`
	ChunkSize   uint32             `msgpack:"chunk_size"`
}

type FileChunkPayload struct {
	FileID      [FileIDLength]byte `msgpack:"file_id"`
	ChunkIndex  uint32             `msgpack:"chunk_index"`
	ChunkSHA256 []byte             `msgpack:"chunk_sha256"`
	Data        []byte             `msgpack:"data"`
}

type FileAckPayload struct {
	FileID     [FileIDLength]byte `msgpack:"file_id"`
	ChunkIndex uint32             `msgpack:"chunk_index"`
	OK         bool               `msgpack:"ok"`
	Error      string             `msgpack:"error,omitempty"`
}

type FileDonePayload struct {
	FileID  [FileIDLength]byte `msgpack:"file_id"`
	SHA256  []byte             `msgpack:"sha256"`
	Success bool               `msgpack:"success"`
	Error   string             `msgpack:"error,omitempty"`
}
