// Package forum ports forest.py, the flat-file PHP-free forum that shipped
// with PirateBox.
//
// The on-disk format is unchanged so boards created by the Python version
// keep working:
//
//	index.txt      hash\tdate\treplies\tlast_reply\tauthor\tsubject
//	<thread hash>  hash\tsubject
//	               date\tauthor\tbody
//
// The second column of a post line is the body encoded with Python 2's
// string_escape codec.
package forum

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Errors carry the exact HTML snippets forest.py printed.
var (
	ErrInvalidThread = errors.New(`<h3 class="error">Invalid Thread Specified</h3>`)
	ErrNoSubject     = errors.New(`<h3 class="error">No Subject Given</h3>`)
	ErrNoBody        = errors.New(`<h3 class="error">No body text!</h3>`)
)

// Config controls a board. The zero value is not usable; use Defaults.
type Config struct {
	DataPath         string
	CGIURL           string
	CSSPath          string
	BoardTitle       string
	BoardDescription string
	AnonAuthor       string
	IndexPageSize    int
	ThreadPageSize   int
	MaxAuthorLen     int
	MaxSubjectLen    int
	MaxBodyLen       int
	Now              func() time.Time
}

// Defaults returns the forest.py settings with DataPath applied.
func Defaults(dataPath string) Config {
	return Config{
		DataPath:   dataPath,
		CGIURL:     "/board/",
		CSSPath:    "/content/css/forest.css",
		BoardTitle: "Luffy Board",
		BoardDescription: "Luffy Board. Put media reviews or questions here.<br>\n" +
			`<A HREF="http://luffy.lan">Click here to go back to the main site</a> `,
		AnonAuthor:     "Anonymous Coward",
		IndexPageSize:  20,
		ThreadPageSize: 20,
		MaxAuthorLen:   20,
		MaxSubjectLen:  100,
		MaxBodyLen:     10000,
		Now:            time.Now,
	}
}

// DateLayout is the Go equivalent of forest.py's '%d %b %Y %H:%M:%S'.
const DateLayout = "02 Jan 2006 15:04:05"

// Summary is one row of the thread index.
type Summary struct {
	Hash      string
	Date      string
	Replies   string
	LastReply string
	Author    string
	Subject   string
}

// Post is a single message inside a thread.
type Post struct {
	Date   string
	Author string
	Body   string
}

// Thread is a thread with a page of its posts.
type Thread struct {
	Hash    string
	Subject string
	Posts   []Post
	Total   int
}

// Store reads and writes a board directory.
type Store struct {
	cfg Config
	mu  sync.Mutex
}

// New returns a store for cfg.
func New(cfg Config) *Store {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Store{cfg: cfg}
}

// Config returns the store configuration.
func (s *Store) Config() Config { return s.cfg }

func (s *Store) indexPath() string { return filepath.Join(s.cfg.DataPath, "index.txt") }

// ValidHash reports whether hash names an existing thread file. It mirrors
// forest.is_valid_hash: only alphanumeric names are considered.
func (s *Store) ValidHash(hash string) bool {
	if hash == "" {
		return false
	}
	for _, r := range hash {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	info, err := os.Stat(filepath.Join(s.cfg.DataPath, hash))
	return err == nil && info.Mode().IsRegular()
}

// ListThreads returns one page of the index plus the total thread count.
func (s *Store) ListThreads(offset int) ([]Summary, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lines, err := s.readIndexLocked()
	if err != nil {
		return nil, 0, err
	}
	total := len(lines)
	summaries := make([]Summary, 0, len(lines))
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) != 6 {
			continue
		}
		summaries = append(summaries, Summary{
			Hash: parts[0], Date: parts[1], Replies: parts[2],
			LastReply: parts[3], Author: parts[4], Subject: parts[5],
		})
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(summaries) {
		return []Summary{}, total, nil
	}
	end := offset + s.cfg.IndexPageSize
	if end > len(summaries) {
		end = len(summaries)
	}
	return summaries[offset:end], total, nil
}

// Thread returns one thread with a page of its posts.
func (s *Store) Thread(hash string, offset int) (*Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ValidHash(hash) {
		return nil, ErrInvalidThread
	}
	data, err := os.ReadFile(filepath.Join(s.cfg.DataPath, hash))
	if err != nil {
		return nil, fmt.Errorf("forum: read thread: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 {
		return nil, ErrInvalidThread
	}
	header := lines[0]
	subject := header
	if _, after, ok := strings.Cut(header, "\t"); ok {
		subject = after
	}
	body := lines[1:]
	total := len(body)
	if offset < 0 {
		offset = 0
	}
	if offset > len(body) {
		// An offset past the end is a valid empty page, like Python's
		// posts[offset:offset+20] slicing. Never index out of range.
		offset = len(body)
	}
	posts := make([]Post, 0, s.cfg.ThreadPageSize)
	for _, line := range body[offset:] {
		if len(posts) >= s.cfg.ThreadPageSize {
			break
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			continue
		}
		posts = append(posts, Post{
			Date:   parts[0],
			Author: parts[1],
			Body:   StringUnescape(parts[2]),
		})
	}
	return &Thread{Hash: hash, Subject: strings.TrimSpace(subject), Posts: posts, Total: total}, nil
}

// NewThread creates a thread and its first post, returning the thread hash.
func (s *Store) NewThread(author, subject, body string) (string, error) {
	if subject == "" {
		return "", ErrNoSubject
	}
	if body == "" {
		return "", ErrNoBody
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cleanSubject := StripHTML(strings.ReplaceAll(subject, "\t", " "))
	hash, err := s.updateThreadLocked(author, &cleanSubject, "")
	if err != nil {
		return "", err
	}
	if err := s.newPostLocked(author, &cleanSubject, body, hash); err != nil {
		return "", err
	}
	return hash, nil
}

// Reply appends a post to an existing thread.
func (s *Store) Reply(hash, author, body string) error {
	if body == "" {
		return ErrNoBody
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ValidHash(hash) {
		return ErrInvalidThread
	}
	cleanAuthor := strings.ReplaceAll(author, "\t", " ")
	if _, err := s.updateThreadLocked(cleanAuthor, nil, hash); err != nil {
		return err
	}
	return s.newPostLocked(cleanAuthor, nil, body, hash)
}

func (s *Store) readIndexLocked() ([]string, error) {
	data, err := os.ReadFile(s.indexPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("forum: read index: %w", err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

// updateThreadLocked mirrors forest.update_thread.
func (s *Store) updateThreadLocked(author string, subject *string, key string) (string, error) {
	now := s.cfg.Now().Format(DateLayout)
	cleanAuthor := ProcessAuthor(author, s.cfg.MaxAuthorLen)

	rowHash := key
	if rowHash == "" {
		sum := md5.Sum([]byte(now + cleanAuthor + deref(subject)))
		rowHash = hex.EncodeToString(sum[:])
	}

	threads, err := s.readIndexLocked()
	if err != nil {
		return "", err
	}

	newThreads := make([]string, 0, len(threads)+1)
	if key == "" {
		newThreads = append(newThreads, strings.Join(
			[]string{rowHash, now, "0", "-", cleanAuthor, deref(subject)}, "\t"))
	}
	for _, thread := range threads {
		if strings.HasPrefix(thread, rowHash) {
			parts := strings.Split(strings.TrimSpace(thread), "\t")
			if len(parts) != 6 {
				continue
			}
			replies, err := strconv.Atoi(parts[2])
			if err != nil {
				replies = 0
			}
			updated := strings.Join([]string{
				parts[0], parts[1], strconv.Itoa(replies + 1), now, parts[4], parts[5],
			}, "\t")
			newThreads = slices.Insert(newThreads, 0, updated)
			continue
		}
		newThreads = append(newThreads, strings.TrimSpace(thread))
	}

	if err := os.MkdirAll(s.cfg.DataPath, 0o755); err != nil {
		return "", fmt.Errorf("forum: create data dir: %w", err)
	}
	if err := os.WriteFile(s.indexPath(), []byte(strings.Join(newThreads, "\n")), 0o644); err != nil {
		return "", fmt.Errorf("forum: write index: %w", err)
	}
	return rowHash, nil
}

// newPostLocked mirrors forest.new_post.
func (s *Store) newPostLocked(author string, subject *string, body, key string) error {
	cleanAuthor := ProcessAuthor(author, s.cfg.MaxAuthorLen)
	cleanSubject := ProcessSubject(subject, s.cfg.MaxSubjectLen)
	cleanBody := ProcessBody(body, s.cfg.MaxBodyLen)
	date := s.cfg.Now().Format(DateLayout)

	path := filepath.Join(s.cfg.DataPath, key)
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		header := key + "\t" + cleanSubject + "\n"
		if err := os.WriteFile(path, []byte(header), 0o644); err != nil {
			return fmt.Errorf("forum: create thread: %w", err)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("forum: append post: %w", err)
	}
	defer f.Close()
	line := date + "\t" + cleanAuthor + "\t" + cleanBody + "\n"
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("forum: write post: %w", err)
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
