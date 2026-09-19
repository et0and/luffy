package shoutbox

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store persists shoutbox history. The format matches the original
// data.pso / chat file: newest HTML line first.
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore returns a store backed by path.
func NewStore(path string) *Store { return &Store{path: path} }

// Path returns the backing file path.
func (s *Store) Path() string { return s.path }

// Read returns the whole chat file. A missing file reads as empty.
func (s *Store) Read() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked()
}

func (s *Store) readLocked() (string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("shoutbox: read %s: %w", s.path, err)
	}
	return string(data), nil
}

// Prepend adds line at the top of the chat history and returns the new
// content. The original writeToDisk did the same, but without truncating;
// luffy writes atomically instead.
func (s *Store) Prepend(line string) (string, error) {
	if line == "" {
		return "", fmt.Errorf("shoutbox: refusing to write empty line")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.readLocked()
	if err != nil {
		return "", err
	}
	content := line + old
	if err := s.writeLocked(content); err != nil {
		return "", err
	}
	return content, nil
}

// Replace overwrites the chat history, used by RESET_CHAT and tests.
func (s *Store) Replace(content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeLocked(content)
}

// Reset writes the initial chat message, matching `cat chat_init.txt > CHATFILE`.
func (s *Store) Reset(initial string) error { return s.Replace(initial) }

// GeneratePage writes the full HTML frame to path.
func (s *Store) GeneratePage(htmlPath string) error {
	content, err := s.Read()
	if err != nil {
		return err
	}
	return writeAtomic(htmlPath, []byte(GeneratePage(content)))
}

func (s *Store) writeLocked(content string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("shoutbox: create dir: %w", err)
	}
	return writeAtomic(s.path, []byte(content))
}

// writeAtomic writes data through a temporary file in the same directory so
// a crash cannot leave a half written chat history behind.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".luffy-tmp-*")
	if err != nil {
		return fmt.Errorf("shoutbox: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("shoutbox: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("shoutbox: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("shoutbox: rename: %w", err)
	}
	return nil
}
