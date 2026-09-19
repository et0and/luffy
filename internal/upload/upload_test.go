package upload

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func multipartBody(t *testing.T, files map[string]string, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for name, content := range files {
		part, err := writer.CreateFormFile("upfile", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, writer.FormDataContentType()
}

func postFiles(t *testing.T, h *Handler, files map[string]string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	body, contentType := multipartBody(t, files, fields)
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestGetForm(t *testing.T) {
	h := New(t.TempDir())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`name="upfile"`, `multiple`, "Send a file", "Send"} {
		if !strings.Contains(body, want) {
			t.Errorf("form missing %q: %s", want, body)
		}
	}
}

func TestUploadSingleFile(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)
	rec := postFiles(t, h, map[string]string{"holiday.jpg": "jpeg-bytes"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "File received!") {
		t.Errorf("missing success message: %s", rec.Body.String())
	}
	// The content type must not mark the main page as a problem.
	data, err := os.ReadFile(filepath.Join(dir, "holiday.jpg"))
	if err != nil {
		t.Fatalf("file missing: %v", err)
	}
	if string(data) != "jpeg-bytes" {
		t.Errorf("content = %q", data)
	}
}

func TestUploadMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)
	rec := postFiles(t, h, map[string]string{"a.txt": "A", "b.txt": "B", "c.txt": "C"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s missing: %v", name, err)
		}
	}
}

func TestUploadSpecialCharacters(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)
	name := "café über 中文.txt"
	rec := postFiles(t, h, map[string]string{name: "data"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
		t.Fatalf("special character file missing: %v", err)
	}
}

func TestUploadCollisionNumbering(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)
	if err := os.WriteFile(filepath.Join(dir, "photo.png"), []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	postFiles(t, h, map[string]string{"photo.png": "second"}, nil)
	postFiles(t, h, map[string]string{"photo.png": "third"}, nil)

	first, _ := os.ReadFile(filepath.Join(dir, "photo.png"))
	second, err := os.ReadFile(filepath.Join(dir, "photo-1.png"))
	if err != nil {
		t.Fatalf("photo-1.png missing: %v", err)
	}
	third, err := os.ReadFile(filepath.Join(dir, "photo-2.png"))
	if err != nil {
		t.Fatalf("photo-2.png missing: %v", err)
	}
	if string(first) != "first" || string(second) != "second" || string(third) != "third" {
		t.Errorf("collision handling wrong: %q %q %q", first, second, third)
	}
}

func TestUploadBlocksIndexHTML(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)
	for _, name := range []string{"index.html", "INDEX.HTM", "index.php"} {
		rec := postFiles(t, h, map[string]string{name: "evil"}, nil)
		if rec.Code != http.StatusSeeOther {
			t.Errorf("%s status = %d, want 303", name, rec.Code)
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s should not be written", name)
		}
	}
}

func TestUploadStripsPaths(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)
	// Browsers sometimes send full paths.
	rec := postFiles(t, h, map[string]string{"../../etc/passwd": "stolen"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "passwd")); err != nil {
		t.Fatalf("sanitized file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "passwd")); err == nil {
		t.Error("file escaped the upload directory")
	}
}

func TestUploadSizeLimit(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)
	h.MaxBytes = 16
	big := strings.Repeat("x", 1024)
	rec := postFiles(t, h, map[string]string{"big.bin": big}, nil)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "big.bin")); err == nil {
		t.Error("oversized file should not be written")
	}
}

func TestUploadFileMode(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)
	h.FileMode = 0o600
	postFiles(t, h, map[string]string{"secret.txt": "s"}, nil)
	info, err := os.Stat(filepath.Join(dir, "secret.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestBaseName(t *testing.T) {
	cases := map[string]string{
		"plain.txt":              "plain.txt",
		"../../etc/passwd":       "passwd",
		`C:\Users\bob\photo.jpg`: "photo.jpg",
		"a/b/c.txt":              "c.txt",
		"..":                     "",
		".":                      "",
	}
	for in, want := range cases {
		if got := BaseName(in); got != want {
			t.Errorf("BaseName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUserDirs(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)
	h.UserDirs = true
	h.PasswordFile = filepath.Join(t.TempDir(), "passwd.json")

	rec := postFiles(t, h, map[string]string{"note.txt": "hi"}, map[string]string{"subdir": "alice", "secret": "pw"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "alice", "note.txt")); err != nil {
		t.Fatalf("user dir file missing: %v", err)
	}

	// Wrong password is rejected.
	rec = postFiles(t, h, map[string]string{"note2.txt": "hi"}, map[string]string{"subdir": "alice", "secret": "wrong"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("wrong password status = %d, want 403", rec.Code)
	}

	// No subdir lands in the anonymous folder.
	rec = postFiles(t, h, map[string]string{"anon.txt": "hi"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("anon status = %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "anonymous", "anon.txt")); err != nil {
		t.Fatalf("anonymous dir missing: %v", err)
	}

	// Invalid characters are rejected like droopy did.
	rec = postFiles(t, h, map[string]string{"x.txt": "hi"}, map[string]string{"subdir": "../evil"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("invalid subdir status = %d, want 403", rec.Code)
	}
}

func TestPreferredLanguage(t *testing.T) {
	cases := map[string]string{
		"":                        "en",
		"de-DE,de;q=0.9,en;q=0.8": "de",
		"fr;q=0.4,ru;q=0.9":       "ru",
		"xx-YY,zz":                "en",
		"pt-BR,pt;q=0.9":          "pt-br",
	}
	for header, want := range cases {
		if got := preferredLanguage(header); got != want {
			t.Errorf("preferredLanguage(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestLocalizedForm(t *testing.T) {
	h := New(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "de")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Datei senden") {
		t.Errorf("German form missing: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "xx")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Send a file") {
		t.Errorf("fallback form missing: %s", rec.Body.String())
	}
}

func TestPublishedFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b.txt", "A.txt", "sub"} {
		path := filepath.Join(dir, name)
		if name == "sub" {
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	names, err := PublishedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "A.txt" || names[1] != "b.txt" {
		t.Errorf("names = %v", names)
	}
	missing, err := PublishedFiles(filepath.Join(dir, "nope"))
	if err != nil || len(missing) != 0 {
		t.Errorf("missing dir = %v, %v", missing, err)
	}
}

func TestUploadHeadRequest(t *testing.T) {
	h := New(t.TempDir())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("HEAD status = %d", rec.Code)
	}
}

func TestUploadMethodNotAllowed(t *testing.T) {
	h := New(t.TempDir())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestUploadPostWithoutFileField(t *testing.T) {
	h := New(t.TempDir())
	body, contentType := multipartBody(t, nil, map[string]string{"other": "value"})
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "File received!") {
		t.Errorf("expected success page: %s", rec.Body.String())
	}
}

func TestUploadBlockedAlongsideValidFile(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)
	rec := postFiles(t, h, map[string]string{"index.html": "evil", "good.txt": "fine"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err == nil {
		t.Error("index.html should be blocked")
	}
	if _, err := os.Stat(filepath.Join(dir, "good.txt")); err != nil {
		t.Errorf("good.txt should be saved: %v", err)
	}
}

func TestUploadEmptyFilenameIsSkipped(t *testing.T) {
	h := New(t.TempDir())
	body, contentType := multipartBody(t, nil, nil)
	// Create a part with an empty filename.
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("upfile", "")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("data"))
	writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestUploadWriteError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	dir := t.TempDir()
	h := New(dir)
	// Make the upload directory read only so file creation fails.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	rec := postFiles(t, h, map[string]string{"x.txt": "data"}, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestUploadMessageShown(t *testing.T) {
	h := New(t.TempDir())
	h.Message = "Drop your files here"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), "Drop your files here") {
		t.Errorf("message missing: %s", rec.Body.String())
	}
}

func TestUploadMessageEscaped(t *testing.T) {
	h := New(t.TempDir())
	h.Message = `<script>alert(1)</script>`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(rec.Body.String(), "<script>alert(1)</script>") {
		t.Errorf("message not escaped: %s", rec.Body.String())
	}
}

func TestUploadLoggerFallback(t *testing.T) {
	h := New(t.TempDir())
	if h.log() == nil {
		t.Error("log() should never return nil")
	}
}

func TestUploadDefaultFileMode(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)
	h.FileMode = 0 // exercise the default branch
	postFiles(t, h, map[string]string{"f.txt": "x"}, nil)
	if _, err := os.Stat(filepath.Join(dir, "f.txt")); err != nil {
		t.Errorf("file missing: %v", err)
	}
}

func TestUserDirsPasswordFilePersistence(t *testing.T) {
	dir := t.TempDir()
	passFile := filepath.Join(t.TempDir(), "nested", "passwd.json")
	h := New(dir)
	h.UserDirs = true
	h.PasswordFile = passFile

	postFiles(t, h, map[string]string{"a.txt": "1"}, map[string]string{"subdir": "bob", "secret": "pw"})
	if _, err := os.Stat(passFile); err != nil {
		t.Fatalf("password file not written: %v", err)
	}
	// A second handler reading the same file accepts the same password.
	h2 := New(t.TempDir())
	h2.UserDirs = true
	h2.PasswordFile = passFile
	rec := postFiles(t, h2, map[string]string{"b.txt": "2"}, map[string]string{"subdir": "bob", "secret": "pw"})
	if rec.Code != http.StatusOK {
		t.Errorf("stored password rejected: %d", rec.Code)
	}
}

func TestUserDirsCorruptPasswordFile(t *testing.T) {
	dir := t.TempDir()
	passFile := filepath.Join(t.TempDir(), "passwd.json")
	if err := os.WriteFile(passFile, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := New(dir)
	h.UserDirs = true
	h.PasswordFile = passFile
	// Corrupt file means no stored passwords; the directory is created anew.
	rec := postFiles(t, h, map[string]string{"a.txt": "1"}, map[string]string{"subdir": "carol", "secret": "pw"})
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestUserDirsAnonymousWithoutSubdir(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)
	h.UserDirs = true
	h.PasswordFile = filepath.Join(t.TempDir(), "passwd.json")
	rec := postFiles(t, h, map[string]string{"anon.txt": "x"}, map[string]string{"subdir": "anonymous", "secret": "ignored"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "anonymous", "anon.txt")); err != nil {
		t.Errorf("anonymous file missing: %v", err)
	}
}

func TestSafeDirName(t *testing.T) {
	cases := map[string]string{
		"alice":      "alice",
		"alice bob":  "alice bob",
		"a-b_c(d)%":  "a-b_c(d)%",
		"bad/name":   "badname",
		"bad..name":  "badname",
		"  spaces  ": "spaces",
		"":           "",
		"///":        "",
		// Uppercase is stripped; the caller lowercases first, like droopy.
		"ALICE": "",
	}
	for in, want := range cases {
		got, _ := safeDirName(in)
		if got != want {
			t.Errorf("safeDirName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUniquePathWithExtensionlessName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := UniquePath(dir, "README")
	if filepath.Base(got) != "README-1" {
		t.Errorf("UniquePath = %q", got)
	}
}

func TestPublishedFilesMissingDirectory(t *testing.T) {
	names, err := PublishedFiles(filepath.Join(t.TempDir(), "missing"))
	if err != nil || len(names) != 0 {
		t.Errorf("PublishedFiles = %v, %v", names, err)
	}
}
