package filetransfer

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

func SplitFile(path string, chunkSize uint32) (*Header, <-chan Chunk, <-chan error, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("filetransfer: open: %w", err)
	}

	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, nil, fmt.Errorf("filetransfer: stat: %w", err)
	}
	size := stat.Size()

	if size > MaxFileSize {
		f.Close()
		return nil, nil, nil, fmt.Errorf("filetransfer: file too large: %d > %d",
			size, MaxFileSize)
	}

	if chunkSize == 0 {
		chunkSize = ChunkSize
	}

	fileID, err := NewFileID()
	if err != nil {
		f.Close()
		return nil, nil, nil, err
	}

	totalChunks := uint32((size + int64(chunkSize) - 1) / int64(chunkSize))
	if totalChunks == 0 {
		totalChunks = 1
	}

	fileHash := sha256.New()
	if _, err := io.Copy(fileHash, f); err != nil {
		f.Close()
		return nil, nil, nil, fmt.Errorf("filetransfer: hash: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, nil, nil, fmt.Errorf("filetransfer: seek: %w", err)
	}

	header := &Header{
		FileID:      fileID,
		Name:        stat.Name(),
		TotalSize:   uint64(size),
		TotalChunks: totalChunks,
		SHA256:      fileHash.Sum(nil),
		ChunkSize:   chunkSize,
	}

	chunks := make(chan Chunk)
	errs := make(chan error, 1)

	go func() {
		defer f.Close()
		defer close(chunks)
		defer close(errs)

		buf := make([]byte, chunkSize)
		for idx := uint32(0); idx < totalChunks; idx++ {
			n, err := io.ReadFull(f, buf)
			if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
				errs <- fmt.Errorf("filetransfer: read chunk %d: %w", idx, err)
				return
			}
			if n == 0 && idx < totalChunks-1 {
				errs <- fmt.Errorf("filetransfer: unexpected empty chunk %d", idx)
				return
			}

			data := make([]byte, n)
			copy(data, buf[:n])
			chunkHash := sha256.Sum256(data)

			chunks <- Chunk{
				FileID:      fileID,
				ChunkIndex:  idx,
				ChunkSHA256: chunkHash[:],
				Data:        data,
			}
		}
	}()

	return header, chunks, errs, nil
}

func Reassemble(header *Header, chunkCh <-chan Chunk, outPath string) (string, error) {
	if header == nil {
		return "", fmt.Errorf("filetransfer: nil header")
	}

	chunks := make(map[uint32]Chunk, header.TotalChunks)
	expected := int(header.TotalChunks)

	for c := range chunkCh {
		if c.FileID != header.FileID {
			return "", fmt.Errorf("filetransfer: file_id mismatch: %s != %s",
				c.FileID.Short(), header.FileID.Short())
		}
		if c.ChunkIndex >= header.TotalChunks {
			return "", fmt.Errorf("filetransfer: chunk index out of range: %d >= %d",
				c.ChunkIndex, header.TotalChunks)
		}
		sum := sha256.Sum256(c.Data)
		if !equalBytes(sum[:], c.ChunkSHA256) {
			return "", fmt.Errorf("filetransfer: chunk %d sha256 mismatch",
				c.ChunkIndex)
		}
		chunks[c.ChunkIndex] = c
	}

	if len(chunks) != expected {
		return "", fmt.Errorf("filetransfer: missing chunks: got %d, want %d",
			len(chunks), expected)
	}

	hasher := sha256.New()
	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("filetransfer: create %s: %w", outPath, err)
	}
	defer f.Close()

	for i := uint32(0); i < header.TotalChunks; i++ {
		c, ok := chunks[i]
		if !ok {
			return "", fmt.Errorf("filetransfer: missing chunk %d", i)
		}
		if _, err := f.Write(c.Data); err != nil {
			return "", fmt.Errorf("filetransfer: write chunk %d: %w", i, err)
		}
		hasher.Write(c.Data)
	}

	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("filetransfer: sync: %w", err)
	}

	actual := hasher.Sum(nil)
	if !equalBytes(actual, header.SHA256) {
		return "", fmt.Errorf("filetransfer: total sha256 mismatch: %x != %x",
			actual, header.SHA256)
	}

	return fmt.Sprintf("%x", actual), nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
