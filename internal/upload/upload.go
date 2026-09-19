// Package upload ports droopy, the file upload page PirateBox embedded in
// an iframe.
//
// Behaviour kept from the original:
//
//   - the form field is upfile and multiple files are accepted
//   - browser supplied paths are reduced to their base name
//   - existing files are never overwritten: name-1.ext, name-2.ext ...
//   - index.html, index.htm and index.php uploads are redirected to /
//   - an optional subdirectory feature creates per-user folders
//   - uploaded files get the configured chmod mode
package upload

import (
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BlockedNames mirrors the droopy index.* blacklist.
var BlockedNames = map[string]bool{
	"index.html": true,
	"index.htm":  true,
	"index.php":  true,
}

// Handler serves the upload form and receives files.
type Handler struct {
	Dir          string
	MaxBytes     int64
	FileMode     os.FileMode
	Message      string
	UserDirs     bool
	PasswordFile string
	Logger       *slog.Logger
}

// New returns a handler with the droopy defaults.
func New(dir string) *Handler {
	return &Handler{Dir: dir, MaxBytes: 5 << 20, FileMode: 0o644, PasswordFile: "/tmp/luffy-upload-passwd.json"}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		h.servePage(w, r, http.StatusOK, pageMain, "")
	case http.MethodPost:
		h.handlePost(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) handlePost(w http.ResponseWriter, r *http.Request) {
	if h.MaxBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, h.MaxBytes)
	}
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		h.log().Warn("upload rejected", "error", err)
		h.servePage(w, r, http.StatusRequestEntityTooLarge, pageError, "problem")
		return
	}
	subdir := ""
	if h.UserDirs {
		var ok bool
		subdir, ok = h.resolveUserDir(r)
		if !ok {
			h.servePage(w, r, http.StatusForbidden, pageError, "subdirerror")
			return
		}
	}

	files := r.MultipartForm.File["upfile"]
	if len(files) == 0 {
		// Accept any field name as a fallback for hand written clients.
		for _, group := range r.MultipartForm.File {
			files = append(files, group...)
		}
	}

	blocked := false
	saved := 0
	for _, header := range files {
		name := BaseName(header.Filename)
		if name == "" {
			continue
		}
		if BlockedNames[strings.ToLower(name)] {
			blocked = true
			continue
		}
		dir := h.Dir
		if subdir != "" {
			dir = filepath.Join(h.Dir, subdir)
		}
		target, err := h.saveFile(dir, name, header)
		if err != nil {
			h.log().Error("upload failed", "file", name, "error", err)
			h.servePage(w, r, http.StatusInternalServerError, pageError, "problem")
			return
		}
		h.log().Info("received file", "file", target)
		saved++
	}

	if blocked && saved == 0 {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	h.servePage(w, r, http.StatusOK, pageSuccess, "")
}

// saveFile writes one uploaded file and returns the final path.
func (h *Handler) saveFile(dir, name string, header *multipart.FileHeader) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("upload: create dir: %w", err)
	}
	target := UniquePath(dir, name)
	src, err := header.Open()
	if err != nil {
		return "", fmt.Errorf("upload: open part: %w", err)
	}
	defer src.Close()
	mode := h.FileMode
	if mode == 0 {
		mode = 0o644
	}
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return "", fmt.Errorf("upload: create %s: %w", target, err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(target)
		return "", fmt.Errorf("upload: write %s: %w", target, err)
	}
	if err := dst.Close(); err != nil {
		return "", fmt.Errorf("upload: close %s: %w", target, err)
	}
	if h.FileMode != 0 {
		// OpenFile applies umask, chmod does not.
		if err := os.Chmod(target, h.FileMode); err != nil {
			return target, nil
		}
	}
	return target, nil
}

// BaseName reduces a browser supplied path to a safe file name. Droopy ran
// posix, mac and nt basename over the path; replacing backslashes covers
// the practical cases.
func BaseName(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.ReplaceAll(path, ":", "/")
	base := path
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	base = strings.TrimSpace(base)
	if base == "." || base == ".." {
		return ""
	}
	return base
}

// UniquePath returns dir/name, or dir/name-1.ext, dir/name-2.ext ... when
// the file already exists, matching droopy's collision handling.
func UniquePath(dir, name string) string {
	candidate := filepath.Join(dir, name)
	if !exists(candidate) {
		return candidate
	}
	ext := filepath.Ext(name)
	root := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s-%d%s", root, i, ext))
		if !exists(candidate) {
			return candidate
		}
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// PublishedFiles lists uploadable files sorted case-insensitively, like
// droopy's --dl mode. Temporary files are skipped.
func PublishedFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return names, nil
}

func (h *Handler) log() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}
