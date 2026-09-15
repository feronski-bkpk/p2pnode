package metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Exporter struct {
	Dir string
}

func NewExporter(dir string) (*Exporter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("metrics: mkdir %s: %w", dir, err)
	}
	return &Exporter{Dir: dir}, nil
}

func (e *Exporter) WriteJSON(name string, v any) (string, error) {
	if err := os.MkdirAll(e.Dir, 0o755); err != nil {
		return "", fmt.Errorf("metrics: mkdir: %w", err)
	}
	path := filepath.Join(e.Dir, name)

	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("metrics: marshal: %w", err)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return "", fmt.Errorf("metrics: write %s: %w", path, err)
	}
	return path, nil
}

func (e *Exporter) WriteJSONWithTimestamp(prefix string, v any) (string, error) {
	ts := time.Now().Format("20060102-150405.000")
	name := fmt.Sprintf("%s-%s.json", prefix, ts)
	return e.WriteJSON(name, v)
}
