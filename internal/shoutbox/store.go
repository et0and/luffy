package shoutbox

import (
	"fmt"
	"os"
	"sync"

	"luffy/internal/atomicfile"
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
	return atomicfile.WriteString(htmlPath, GeneratePage(content))
}

func (s *Store) writeLocked(content string) error {
	return atomicfile.WriteString(s.path, content)
}
