// Package atomicfile writes files without exposing partially written data.
//
// Every generated file (chat history, shoutbox page, disk usage fragment,
// station counter) is replaced through a temporary file in the same
// directory, so a crash or a full disk cannot leave a truncated file behind
// and readers never see a half written page.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write creates parent directories and atomically replaces path with data.
func Write(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("atomicfile: create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".luffy-*")
	if err != nil {
		return fmt.Errorf("atomicfile: temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("atomicfile: write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("atomicfile: close %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("atomicfile: rename %s: %w", path, err)
	}
	return nil
}

// WriteString is Write for string content.
func WriteString(path, content string) error {
	return Write(path, []byte(content))
}
