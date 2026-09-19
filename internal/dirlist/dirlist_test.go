package dirlist

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "HEADER.txt"), "<head>Custom Header</head>")
	mustWrite(t, filepath.Join(root, "README.txt"), "<footer>Custom Footer</footer>")
	mustWrite(t, filepath.Join(root, "movie.mp4"), "video")
	mustWrite(t, filepath.Join(root, "notes.txt"), "notes")
	mustWrite(t, filepath.Join(root, ".secret"), "hidden")
	mustWrite(t, filepath.Join(root, "archive.tar.gz"), "archive")
	if err := os.Mkdir(filepath.Join(root, "photos"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "photos", "beach.jpg"), "jpeg")
	return root
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, h *Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestListingContents(t *testing.T) {
	root := setupTree(t)
	h := New(root, "/content")
	rec := get(t, h, "/content/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Index of /content/",
		"Custom Header", "Custom Footer",
		"photos/", "movie.mp4", "notes.txt", "archive.tar.gz",
		"Directory", "video/mp4", "text/plain", "application/x-tgz",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("listing missing %q", want)
		}
	}
	if strings.Contains(body, ".secret") {
		t.Error("hidden file should not be listed")
	}
	if strings.Contains(body, "HEADER.txt") || strings.Contains(body, "README.txt") {
		t.Error("HEADER.txt/README.txt should be hidden from the listing")
	}
}

func TestListingSortsDirectoriesFirst(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "zzzdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "aaa.txt"), "a")
	h := New(root, "/Shared")
	body := get(t, h, "/Shared/").Body.String()
	dirIdx := strings.Index(body, "zzzdir")
	fileIdx := strings.Index(body, "aaa.txt")
	if dirIdx < 0 || fileIdx < 0 || dirIdx > fileIdx {
		t.Errorf("directories must sort first: dir=%d file=%d", dirIdx, fileIdx)
	}
}

func TestRootHasNoParentLink(t *testing.T) {
	root := setupTree(t)
	h := New(root, "/content")
	if strings.Contains(get(t, h, "/content/").Body.String(), "Parent Directory") {
		t.Error("root listing should not have a parent link")
	}
	if !strings.Contains(get(t, h, "/content/photos/").Body.String(), "Parent Directory") {
		t.Error("subdirectory listing should have a parent link")
	}
}

func TestFileServing(t *testing.T) {
	root := setupTree(t)
	h := New(root, "/Shared")
	rec := get(t, h, "/Shared/notes.txt")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.String() != "notes" {
		t.Errorf("body = %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content type = %q", ct)
	}
	// Range requests work through ServeContent.
	req := httptest.NewRequest(http.MethodGet, "/Shared/notes.txt", nil)
	req.Header.Set("Range", "bytes=0-2")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "not" {
		t.Errorf("range response = %d %q", rec.Code, rec.Body.String())
	}
}

func TestDirectoryRedirect(t *testing.T) {
	root := setupTree(t)
	h := New(root, "/content")
	rec := get(t, h, "/content/photos")
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/content/photos/" {
		t.Errorf("location = %q", loc)
	}
}

func TestIndexFilePreferredOverListing(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "index.html"), "<html>welcome</html>")
	h := New(root, "/content")
	body := get(t, h, "/content/").Body.String()
	if !strings.Contains(body, "welcome") || strings.Contains(body, "Directory Listing") {
		t.Errorf("index.html should win: %s", body)
	}
}

func TestTraversalBlocked(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "ok.txt"), "ok")
	outside := filepath.Join(filepath.Dir(root), "outside.txt")
	mustWrite(t, outside, "secret")

	h := New(root, "/content")
	for _, target := range []string{
		"/content/../outside.txt",
		"/content/%2e%2e/outside.txt",
		"/content/..%2foutside.txt",
		"/content/../../etc/passwd",
	} {
		rec := get(t, h, target)
		if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "secret") {
			t.Errorf("traversal succeeded for %s", target)
		}
	}
}

func TestNotFound(t *testing.T) {
	root := setupTree(t)
	h := New(root, "/content")
	if rec := get(t, h, "/content/missing.txt"); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestWrongPrefix(t *testing.T) {
	root := setupTree(t)
	h := New(root, "/content")
	if rec := get(t, h, "/other/notes.txt"); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestSymlinkedRoot(t *testing.T) {
	// PirateBox puts the real content on the USB stick and symlinks it into
	// the web folder.
	real := t.TempDir()
	mustWrite(t, filepath.Join(real, "file.txt"), "usb content")
	parent := t.TempDir()
	link := filepath.Join(parent, "content")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	h := New(link, "/content")
	rec := get(t, h, "/content/file.txt")
	if rec.Code != http.StatusOK || rec.Body.String() != "usb content" {
		t.Errorf("symlinked root failed: %d %q", rec.Code, rec.Body.String())
	}
}

func TestContentType(t *testing.T) {
	cases := map[string]string{
		"a.tar.gz":  "application/x-tgz",
		"a.mp3":     "audio/mpeg",
		"a.conf":    "text/plain",
		"a.unknown": "application/octet-stream",
		"a.html":    "text/html",
	}
	for name, want := range cases {
		if got := ContentType(name); !strings.HasPrefix(got, want) {
			t.Errorf("ContentType(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestSizeLabels(t *testing.T) {
	cases := []struct {
		size int64
		want string
	}{
		{10, "10B"},
		{2048, "2.0K"},
		{2 * 1024 * 1024, "2.0M"},
	}
	for _, c := range cases {
		if got := sizeLabel(false, c.size); got != c.want {
			t.Errorf("sizeLabel(%d) = %q, want %q", c.size, got, c.want)
		}
	}
	if got := sizeLabel(true, 0); got != "- &nbsp;" {
		t.Errorf("dir size label = %q", got)
	}
}
