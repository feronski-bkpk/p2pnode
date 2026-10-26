package filetransfer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeFileName_OK(t *testing.T) {
	ok := []string{
		"payload.bin",
		"my-file_v2.tar.gz",
		".hidden",
		"файл.txt",
		"a",
		strings.Repeat("a", 255),
		"file with spaces.txt",
	}
	for _, name := range ok {
		got, err := SanitizeFileName(name)
		if err != nil {
			t.Errorf("SanitizeFileName(%q) unexpected error: %v", name, err)
			continue
		}
		if got != name {
			t.Errorf("SanitizeFileName(%q) = %q, want %q", name, got, name)
		}
	}
}

func TestSanitizeFileName_Reject(t *testing.T) {
	cases := []struct {
		name string
		desc string
	}{
		{"", "empty"},
		{"../etc/passwd", "path traversal"},
		{"..\\..\\windows\\system32", "path traversal (backslash)"},
		{"..", "dotdot"},
		{".", "dot"},
		{"/etc/passwd", "absolute path"},
		{"/tmp/x", "absolute path"},
		{"C:\\Windows\\x", "drive letter"},
		{"\\\\server\\share", "UNC path"},
		{"a/b", "slash inside"},
		{"a\\b", "backslash inside"},
		{"file\x00.txt", "NUL byte"},
		{"file\x01.txt", "control char"},
		{"file\x7f.txt", "DEL char"},
		{"CON", "Windows reserved"},
		{"con", "Windows reserved (lower)"},
		{"NUL.txt", "Windows reserved with ext"},
		{"COM1", "Windows reserved"},
		{"LPT9", "Windows reserved"},
		{strings.Repeat("a", 256), "too long"},
	}
	for _, tc := range cases {
		if _, err := SanitizeFileName(tc.name); err == nil {
			t.Errorf("SanitizeFileName(%q [%s]) should fail, got nil", tc.name, tc.desc)
		}
	}
}

func TestUniqueDestPath_NoCollision(t *testing.T) {
	dir := t.TempDir()
	p, err := UniqueDestPath(dir, "payload.bin")
	if err != nil {
		t.Fatalf("UniqueDestPath: %v", err)
	}
	want := filepath.Join(dir, "payload.bin")
	if p != want {
		t.Fatalf("got %q, want %q", p, want)
	}
}

func TestUniqueDestPath_SuffixOnCollision(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(base, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := UniqueDestPath(dir, "payload.bin")
	if err != nil {
		t.Fatalf("UniqueDestPath: %v", err)
	}
	want := filepath.Join(dir, "payload.1.bin")
	if p != want {
		t.Fatalf("got %q, want %q", p, want)
	}

	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p2, err := UniqueDestPath(dir, "payload.bin")
	if err != nil {
		t.Fatalf("UniqueDestPath (2): %v", err)
	}
	want2 := filepath.Join(dir, "payload.2.bin")
	if p2 != want2 {
		t.Fatalf("got %q, want %q", p2, want2)
	}
}

func TestUniqueDestPath_NoExtension(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := UniqueDestPath(dir, "README")
	if err != nil {
		t.Fatalf("UniqueDestPath: %v", err)
	}
	want := filepath.Join(dir, "README.1")
	if p != want {
		t.Fatalf("got %q, want %q", p, want)
	}
}

func TestUniqueDestPath_EscapesOutDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := UniqueDestPath(dir, "../evil"); err == nil {
		t.Fatal("expected error for path escaping outDir")
	}
}
