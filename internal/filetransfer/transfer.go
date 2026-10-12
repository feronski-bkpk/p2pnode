package filetransfer

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type Sender interface {
	SendChunk(ctx context.Context, destID [32]byte, payload []byte) error
}

func SendFile(
	ctx context.Context,
	sender Sender,
	destID [32]byte,
	path string,
	chunkSize uint32,
	log *slog.Logger,
) (*Header, Progress, error) {

	header, chunks, errs, err := SplitFile(path, chunkSize)
	if err != nil {
		return nil, Progress{}, err
	}

	log.Info("filetransfer: sending",
		"file_id", header.FileID.Short(),
		"name", header.Name,
		"size", header.TotalSize,
		"chunks", header.TotalChunks,
		"chunk_size", header.ChunkSize)

	headerEnv, err := MarshalHeaderEnvelope(*header)
	if err != nil {
		return header, Progress{}, fmt.Errorf(
			"filetransfer: marshal header: %w", err)
	}

	headerSendErr := sender.SendChunk(ctx, destID, headerEnv)
	if headerSendErr != nil {
		return header, Progress{}, fmt.Errorf(
			"filetransfer: send header: %w", headerSendErr)
	}

	progress := Progress{
		FileID:      header.FileID,
		TotalChunks: header.TotalChunks,
	}

	start := time.Now()
	lastLogPct := -1

	maxRetries := 3

	for chunk := range chunks {
		pct := progress.Percent()
		if pct/10 > lastLogPct/10 {
			log.Info("filetransfer: progress",
				"file_id", header.FileID.Short(),
				"percent", pct,
				"acked", progress.AckedChunks,
				"total", progress.TotalChunks,
				"elapsed_ms", time.Since(start).Milliseconds())
			lastLogPct = pct
		}

		var lastErr error
		sent := false
		for attempt := 1; attempt <= maxRetries; attempt++ {
			payload, err := MarshalChunkEnvelope(chunk)
			if err != nil {
				return header, progress, fmt.Errorf(
					"filetransfer: encode chunk %d: %w", chunk.ChunkIndex, err)
			}

			chunkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = sender.SendChunk(chunkCtx, destID, payload)
			cancel()

			if err == nil {
				sent = true
				break
			}
			lastErr = err
			log.Warn("filetransfer: chunk retry",
				"file_id", header.FileID.Short(),
				"chunk_idx", chunk.ChunkIndex,
				"attempt", attempt,
				"err", err)
			time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
		}

		if !sent {
			return header, progress, fmt.Errorf(
				"filetransfer: chunk %d failed after %d attempts: %w",
				chunk.ChunkIndex, maxRetries, lastErr)
		}

		progress.AckedChunks++
		progress.BytesSent += uint64(len(chunk.Data))
	}

	select {
	case err, ok := <-errs:
		if ok && err != nil {
			return header, progress, err
		}
	default:
	}

	log.Info("filetransfer: done",
		"file_id", header.FileID.Short(),
		"acked", progress.AckedChunks,
		"total", progress.TotalChunks,
		"bytes", progress.BytesSent,
		"duration_ms", time.Since(start).Milliseconds(),
		"sha256", fmt.Sprintf("%x", header.SHA256))

	return header, progress, nil
}

func ReceiveFile(
	ctx context.Context,
	store *Store,
	header *Header,
	outDir string,
	log *slog.Logger,
) (string, string, error) {

	store.Init(header)
	log.Info("filetransfer: receiving",
		"file_id", header.FileID.Short(),
		"name", header.Name,
		"size", header.TotalSize,
		"chunks", header.TotalChunks)

	outPath := outDir + "/" + header.Name

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-ticker.C:
			done := store.Complete(header.FileID)
			if done == nil {
				continue
			}

			chunkCh := make(chan Chunk, len(done.Chunks))
			for i := uint32(0); i < header.TotalChunks; i++ {
				chunkCh <- done.Chunks[i]
			}
			close(chunkCh)

			sha, err := Reassemble(header, chunkCh, outPath)
			if err != nil {
				return "", "", err
			}

			log.Info("filetransfer: received",
				"file_id", header.FileID.Short(),
				"path", outPath,
				"sha256", sha)

			return outPath, sha, nil
		}
	}
}
