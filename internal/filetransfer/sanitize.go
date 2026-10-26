package filetransfer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxFileNameLen = 255

var reservedWindowsNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true,
	"COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true,
	"LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

func SanitizeFileName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("filetransfer: empty file name")
	}
	if strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("filetransfer: file name contains NUL byte")
	}
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") {
		return "", fmt.Errorf("filetransfer: absolute path not allowed: %q", name)
	}
	if len(name) >= 2 && name[1] == ':' {
		return "", fmt.Errorf("filetransfer: drive-letter path not allowed: %q", name)
	}
	if strings.ContainsAny(name, "/\\") {
		return "", fmt.Errorf("filetransfer: path separators not allowed: %q", name)
	}
	if name == "." || name == ".." {
		return "", fmt.Errorf("filetransfer: reserved name: %q", name)
	}
	if len(name) > maxFileNameLen {
		return "", fmt.Errorf("filetransfer: file name too long: %d > %d",
			len(name), maxFileNameLen)
	}
	base := name
	if i := strings.IndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	if reservedWindowsNames[strings.ToUpper(base)] {
		return "", fmt.Errorf("filetransfer: reserved Windows name: %q", name)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("filetransfer: control character in name: %q", name)
		}
	}
	return name, nil
}

func UniqueDestPath(outDir, name string) (string, error) {
	const maxUniqueAttempts = 100

	cleanDir := filepath.Clean(outDir)
	joined := filepath.Join(cleanDir, name)
	cleanPath := filepath.Clean(joined)

	prefix := cleanDir + string(os.PathSeparator)
	if !strings.HasPrefix(cleanPath+string(os.PathSeparator), prefix) &&
		cleanPath != cleanDir {
		return "", fmt.Errorf("filetransfer: path escapes outDir: %q -> %q",
			name, cleanPath)
	}

	if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
		return cleanPath, nil
	} else if err != nil {
		return "", fmt.Errorf("filetransfer: stat %q: %w", cleanPath, err)
	}

	ext := filepath.Ext(name)
	stem := name[:len(name)-len(ext)]
	for i := 1; i <= maxUniqueAttempts; i++ {
		candidate := filepath.Join(cleanDir, fmt.Sprintf("%s.%d%s", stem, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", fmt.Errorf("filetransfer: stat %q: %w", candidate, err)
		}
	}
	return "", fmt.Errorf("filetransfer: too many collisions for %q (>= %d)",
		name, maxUniqueAttempts)
}
