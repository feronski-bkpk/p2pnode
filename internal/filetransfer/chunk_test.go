package filetransfer

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
)

func TestSplitFile_Single(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bin")
	data := []byte("hello world")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	header, chunks, errs, err := SplitFile(path, 1024)
	if err != nil {
		t.Fatalf("SplitFile: %v", err)
	}

	if header.TotalSize != uint64(len(data)) {
		t.Fatalf("size: got %d want %d", header.TotalSize, len(data))
	}
	if header.TotalChunks != 1 {
		t.Fatalf("chunks: got %d want 1", header.TotalChunks)
	}

	sum := sha256.Sum256(data)
	if !bytes.Equal(header.SHA256, sum[:]) {
		t.Fatalf("file sha256 mismatch")
	}

	var got []byte
	for c := range chunks {
		got = append(got, c.Data...)
	}
	if err := <-errs; err != nil {
		t.Fatalf("errs: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Fatalf("data mismatch: got %q want %q", got, data)
	}
}

func TestSplitFile_Multi(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bin")
	data := make([]byte, 100_000)
	for i := range data {
		data[i] = byte(i % 256)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	header, chunks, errs, err := SplitFile(path, 32768)
	if err != nil {
		t.Fatalf("SplitFile: %v", err)
	}

	wantChunks := uint32(4)
	if header.TotalChunks != wantChunks {
		t.Fatalf("chunks: got %d want %d", header.TotalChunks, wantChunks)
	}

	var got []byte
	for c := range chunks {
		got = append(got, c.Data...)
	}
	if err := <-errs; err != nil {
		t.Fatalf("errs: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Fatalf("data mismatch")
	}
}

func TestReassemble(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.bin")
	dstPath := filepath.Join(dir, "dst.bin")

	data := []byte("test data for reassemble")
	if err := os.WriteFile(srcPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	header, chunks, _, err := SplitFile(srcPath, 8)
	if err != nil {
		t.Fatal(err)
	}

	gotSHA, err := Reassemble(header, chunks, dstPath)
	if err != nil {
		t.Fatalf("Reassemble: %v", err)
	}

	wantSHA := string(header.SHA256)
	_ = wantSHA

	wantHex := ""
	{
		sum := sha256.Sum256(data)
		wantHex = fmtHex(sum[:])
	}
	if gotSHA != wantHex {
		t.Fatalf("sha256: got %s want %s", gotSHA, wantHex)
	}

	reassembled, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reassembled, data) {
		t.Fatalf("data mismatch: got %q want %q", reassembled, data)
	}
}

func TestReassemble_MissingChunk(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.bin")
	if err := os.WriteFile(srcPath, []byte("test data for missing chunk"), 0o644); err != nil {
		t.Fatal(err)
	}

	header, chunks, _, err := SplitFile(srcPath, 4)
	if err != nil {
		t.Fatal(err)
	}

	filtered := make(chan Chunk)
	go func() {
		defer close(filtered)
		i := uint32(0)
		for c := range chunks {
			if i != 1 {
				filtered <- c
			}
			i++
		}
	}()

	_, err = Reassemble(header, filtered, filepath.Join(dir, "out.bin"))
	if err == nil {
		t.Fatal("expected error for missing chunk")
	}
}

func TestStore_Dedup(t *testing.T) {
	store := NewStore()

	id, _ := NewFileID()
	h := &Header{FileID: id, TotalChunks: 3, ChunkSize: 32}
	store.Init(h)

	c := Chunk{FileID: id, ChunkIndex: 1, Data: []byte("a")}

	if !store.AddChunk(c) {
		t.Fatal("first add should return true")
	}
	if store.AddChunk(c) {
		t.Fatal("second add should return false (dedup)")
	}
	if store.Count() != 1 {
		t.Fatalf("count: %d", store.Count())
	}
}

func TestStore_Complete(t *testing.T) {
	store := NewStore()

	id, _ := NewFileID()
	h := &Header{FileID: id, TotalChunks: 2, ChunkSize: 32}
	store.Init(h)

	store.AddChunk(Chunk{FileID: id, ChunkIndex: 0, Data: []byte("a")})
	if store.Complete(id) != nil {
		t.Fatal("should not be complete yet")
	}

	store.AddChunk(Chunk{FileID: id, ChunkIndex: 1, Data: []byte("b")})
	if store.Complete(id) == nil {
		t.Fatal("should be complete")
	}
}

func fmtHex(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = digits[v>>4]
		out[i*2+1] = digits[v&0x0f]
	}
	return string(out)
}
