package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"luffy/internal/config"
	"luffy/internal/diskusage"
	"luffy/internal/shoutbox"
	"luffy/internal/stations"
)

func uploadBody(t *testing.T, field, filename, content string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, writer.FormDataContentType()
}

type fakeCounter struct{ n int }

func (f fakeCounter) Count(context.Context) (int, error) { return f.n, nil }

func testServer(t *testing.T) (*Server, *config.Config, string) {
	t.Helper()
	base := t.TempDir()
	cfg := config.FromValues(config.Values{
		"PIRATEBOX_FOLDER": base,
		"WWW_FOLDER":       filepath.Join(base, "www"),
		"WWW_CONTENT":      filepath.Join(base, "www", "content"),
		"SHARE_FOLDER":     filepath.Join(base, "share"),
		"UPLOADFOLDER":     filepath.Join(base, "share", "Shared"),
		"CHATFILE":         filepath.Join(base, "www", "cgi-bin", "data.pso"),
		"GEN_CHATFILE":     filepath.Join(base, "www", "chat_content.html"),
		"GEN_DISKFILE":     filepath.Join(base, "www", "diskusage.html"),
		"PBX_JSON_FILE":    filepath.Join(base, "www", "piratebox_config.json"),
		"HOST":             "piratebox.lan",
		"HTTP_PORT":        "8001",
		"GLOBAL_DEST":      "[ #255.255.255.255# ]",
	})
	for _, dir := range []string{cfg.WwwFolder, cfg.UploadFolder, cfg.WwwContent, cfg.ShareFolder} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	store := shoutbox.NewStore(cfg.ChatFile)
	du := diskusage.New(cfg.UploadFolder, cfg.GenDiskFile)
	du.Stat = func(string) (diskusage.Usage, error) {
		return diskusage.Usage{Total: 100, Used: 25}, nil
	}
	tracker := stations.NewTracker(fakeCounter{n: 2}, filepath.Join(cfg.WwwFolder, "station_cnt.txt"), time.Minute)
	srv := New(Options{
		Cfg:       cfg,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version:   "test-version",
		Shoutbox:  store,
		DiskUsage: du,
		Stations:  tracker,
		Now:       func() time.Time { return time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC) },
	})
	return srv, cfg, base
}

func do(t *testing.T, srv *Server, method, target string, body io.Reader, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, target, body)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestRootRedirectPage(t *testing.T) {
	srv, _, _ := testServer(t)
	rec := do(t, srv, http.MethodGet, "/", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "http://piratebox.lan:8001/content") {
		t.Errorf("redirect target missing: %s", body)
	}
}

func TestRootServesInstalledIndex(t *testing.T) {
	srv, cfg, _ := testServer(t)
	indexPath := filepath.Join(cfg.WwwFolder, "index.html")
	if err := os.WriteFile(indexPath, []byte("<html>custom index</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := do(t, srv, http.MethodGet, "/", nil, nil)
	if !strings.Contains(rec.Body.String(), "custom index") {
		t.Errorf("installed index not served: %s", rec.Body.String())
	}
}

func TestUnknownPathGetsCaptiveRedirect(t *testing.T) {
	srv, _, _ := testServer(t)
	rec := do(t, srv, http.MethodGet, "/some/unknown/page", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/content") {
		t.Errorf("redirect body missing: %s", rec.Body.String())
	}
}

func TestContentServesEmbeddedAssetsWithoutFolder(t *testing.T) {
	srv, cfg, _ := testServer(t)
	if err := os.RemoveAll(cfg.WwwContent); err != nil {
		t.Fatal(err)
	}
	rec := do(t, srv, http.MethodGet, "/content/", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "PirateBox") {
		t.Errorf("embedded index missing: %s", rec.Body.String())
	}
	css := do(t, srv, http.MethodGet, "/content/css/page_style.css", nil, nil)
	if css.Code != http.StatusOK || !strings.Contains(css.Body.String(), "body") {
		t.Errorf("embedded css not served: %d", css.Code)
	}
}

func TestContentServesOnDiskFolder(t *testing.T) {
	srv, cfg, _ := testServer(t)
	if err := os.WriteFile(filepath.Join(cfg.WwwContent, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := do(t, srv, http.MethodGet, "/content/hello.txt", nil, nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "hi" {
		t.Errorf("content file = %d %q", rec.Code, rec.Body.String())
	}
}

func TestSharedListingAndDownloads(t *testing.T) {
	srv, cfg, _ := testServer(t)
	if err := os.WriteFile(filepath.Join(cfg.UploadFolder, "shared.txt"), []byte("file data"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := do(t, srv, http.MethodGet, "/Shared/", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "shared.txt") {
		t.Fatalf("listing = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, srv, http.MethodGet, "/Shared/shared.txt", nil, nil)
	if rec.Body.String() != "file data" {
		t.Errorf("download = %q", rec.Body.String())
	}
}

func TestShoutboxWriteAndRead(t *testing.T) {
	srv, cfg, _ := testServer(t)
	form := url.Values{"name": {"Ana"}, "data": {"hello <world>"}, "color": {"blue"}, "timestamp": {"1758283200"}}
	rec := do(t, srv, http.MethodPost, "/cgi-bin/psowrte.py", nil, form)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ok") {
		t.Fatalf("write = %d %s", rec.Code, rec.Body.String())
	}
	data, err := os.ReadFile(cfg.ChatFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<date>12:00:00</date>") {
		t.Errorf("server timestamp missing: %s", data)
	}
	if !strings.Contains(string(data), "hello &lt;world&gt;") {
		t.Errorf("escaping missing: %s", data)
	}
	rec = do(t, srv, http.MethodGet, "/chat_content.html", nil, nil)
	if !strings.Contains(rec.Body.String(), "hello &lt;world&gt;") {
		t.Errorf("read page missing message: %s", rec.Body.String())
	}
}

func TestShoutboxDisabled(t *testing.T) {
	srv, cfg, _ := testServer(t)
	cfg.ShoutboxEnabled = false
	rec := do(t, srv, http.MethodPost, "/cgi-bin/psowrte.py", nil, url.Values{"data": {"x"}})
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if _, err := os.Stat(cfg.ChatFile); err == nil {
		t.Error("chat file should not be written when disabled")
	}
}

func TestShoutboxGlobalChatBroadcastsOnly(t *testing.T) {
	srv, cfg, _ := testServer(t)
	cfg.GlobalChat = true
	cfg.GlobalDest = []string{"192.168.77.255"}
	fake := &captureSender{}
	srv.opts.Broadcaster = &shoutbox.Broadcaster{Destinations: cfg.GlobalDest, Sender: fake}
	rec := do(t, srv, http.MethodPost, "/cgi-bin/psowrte.py", nil, url.Values{"name": {"a"}, "data": {"broadcast me"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(fake.messages) != 1 {
		t.Fatalf("broadcasts = %d", len(fake.messages))
	}
	msg, err := shoutbox.Parse(fake.messages[0])
	if err != nil {
		t.Fatalf("parse broadcast: %v", err)
	}
	if content, _ := msg.Content(); !strings.Contains(content, "broadcast me") {
		t.Errorf("broadcast content = %q", content)
	}
	if _, err := os.Stat(cfg.ChatFile); err == nil {
		t.Error("global chat mode must not write locally")
	}
}

type captureSender struct {
	messages []string
}

func (c *captureSender) Send(destination string, port int, payload []byte) error {
	c.messages = append(c.messages, string(payload))
	return nil
}

func TestDiskUsageFlow(t *testing.T) {
	srv, cfg, _ := testServer(t)
	rec := do(t, srv, http.MethodGet, "/diskusage.html", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "aria-valuenow='25'") {
		t.Fatalf("diskusage read = %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(cfg.GenDiskFile); err != nil {
		t.Errorf("generated file missing: %v", err)
	}
	rec = do(t, srv, http.MethodPost, "/cgi-bin/diskwrite.py", nil, url.Values{})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ok") {
		t.Errorf("diskwrite = %d %s", rec.Code, rec.Body.String())
	}
}

func TestStationCount(t *testing.T) {
	srv, _, _ := testServer(t)
	// Prime the tracker without running the goroutine.
	if err := srv.opts.Stations.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := do(t, srv, http.MethodGet, "/station_cnt.txt", nil, nil)
	if !strings.Contains(rec.Body.String(), "Currently there are 2 connected clients") {
		t.Errorf("station count = %s", rec.Body.String())
	}
}

func TestConfigJSON(t *testing.T) {
	srv, cfg, _ := testServer(t)
	if err := os.WriteFile(cfg.VersionFile, []byte("piratebox-ws_1.1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := do(t, srv, http.MethodGet, "/piratebox_config.json", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v: %s", err, rec.Body.String())
	}
	root, ok := payload["piratebox"].(map[string]any)
	if !ok {
		t.Fatalf("piratebox key missing: %s", rec.Body.String())
	}
	module := root["module"].(map[string]any)
	upload := module["upload"].(map[string]any)
	if upload["status"] != true || upload["upload_style"] != "droopy" || upload["droopy_port"] != "8080" {
		t.Errorf("upload module = %v", upload)
	}
	shoutboxModule := module["shoutbox"].(map[string]any)
	if shoutboxModule["status"] != true {
		t.Errorf("shoutbox module = %v", shoutboxModule)
	}
	if module["version"] != "piratebox-ws_1.1.4" {
		t.Errorf("version = %v", module["version"])
	}
}

func TestCaptivePortalEndpoints(t *testing.T) {
	srv, _, _ := testServer(t)
	rec := do(t, srv, http.MethodGet, "/generate_204", nil, nil)
	if rec.Code != http.StatusNoContent {
		t.Errorf("generate_204 status = %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("generate_204 body = %q", rec.Body.String())
	}
	rec = do(t, srv, http.MethodGet, "/ncsi.txt", nil, nil)
	if rec.Body.String() != "Microsoft NCSI" {
		t.Errorf("ncsi = %q", rec.Body.String())
	}
	for _, path := range []string{"/hotspot-detect.html", "/library/test/success.html"} {
		rec = do(t, srv, http.MethodGet, path, nil, nil)
		if !strings.Contains(rec.Body.String(), "Success") {
			t.Errorf("%s = %s", path, rec.Body.String())
		}
	}
}

func TestIOSCaptiveUserAgent(t *testing.T) {
	srv, _, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/anything/at/all", nil)
	req.Header.Set("User-Agent", "CaptiveNetworkSupport/1.0 wispr")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Success") {
		t.Errorf("iOS probe = %s", rec.Body.String())
	}
}

func TestBoardRoute(t *testing.T) {
	srv, _, _ := testServer(t)
	form := url.Values{"author": {"Alice"}, "subject": {"Hi"}, "body": {"Body"}}
	rec := do(t, srv, http.MethodPost, "/board/?new=thread", nil, form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("new thread status = %d: %s", rec.Code, rec.Body.String())
	}
	rec = do(t, srv, http.MethodGet, "/board/", nil, nil)
	if !strings.Contains(rec.Body.String(), "Hi") {
		t.Errorf("board index missing thread: %s", rec.Body.String())
	}
	// The legacy cgi-bin path also works.
	rec = do(t, srv, http.MethodGet, "/cgi-bin/forest.py", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Hi") {
		t.Errorf("forest.py route = %d", rec.Code)
	}
}

func TestUploadServer(t *testing.T) {
	srv, cfg, _ := testServer(t)
	body, contentType := uploadBody(t, "upfile", "note.txt", "uploaded via droopy port")
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	srv.uploadHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d: %s", rec.Code, rec.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(cfg.UploadFolder, "note.txt"))
	if err != nil || string(data) != "uploaded via droopy port" {
		t.Errorf("uploaded file = %q, %v", data, err)
	}
}

func TestHostHeaderIncludesPort(t *testing.T) {
	srv, cfg, _ := testServer(t)
	cfg.HTTPPort = 8001
	if got := srv.hostHeader(); got != "piratebox.lan:8001" {
		t.Errorf("hostHeader = %q", got)
	}
	cfg.HTTPPort = 80
	if got := srv.hostHeader(); got != "piratebox.lan" {
		t.Errorf("hostHeader = %q", got)
	}
}

func TestListenAndServeShutdown(t *testing.T) {
	srv, _, _ := testServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	// Port 0 lets the kernel choose a free port.
	go func() { done <- srv.ListenAndServe(ctx, "127.0.0.1:0") }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("server error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
}

func TestDiskUsageReadFallbackWhenFileMissing(t *testing.T) {
	srv, cfg, _ := testServer(t)
	// Remove the generated file after priming so the read path falls back.
	if err := srv.opts.DiskUsage.Write(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cfg.GenDiskFile); err != nil {
		t.Fatal(err)
	}
	// Make the generator unable to write by pointing it at an invalid path.
	srv.opts.DiskUsage.Path = filepath.Join(cfg.GenDiskFile, "not-a-dir", "out.html")
	rec := do(t, srv, http.MethodGet, "/diskusage.html", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "progress") {
		t.Errorf("fallback body = %s", rec.Body.String())
	}
}

func TestDiskUsageWriteMethodNotAllowed(t *testing.T) {
	srv, _, _ := testServer(t)
	rec := do(t, srv, http.MethodGet, "/cgi-bin/diskwrite.py", nil, nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestShoutboxWriteMethodNotAllowed(t *testing.T) {
	srv, _, _ := testServer(t)
	rec := do(t, srv, http.MethodGet, "/cgi-bin/psowrte.py", nil, nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestShoutboxReadError(t *testing.T) {
	srv, cfg, _ := testServer(t)
	// A directory where the chat file belongs makes Read fail.
	if err := os.MkdirAll(cfg.ChatFile, 0o755); err != nil {
		t.Fatal(err)
	}
	rec := do(t, srv, http.MethodGet, "/chat_content.html", nil, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestShoutboxDefaultsForMissingFields(t *testing.T) {
	srv, cfg, _ := testServer(t)
	rec := do(t, srv, http.MethodPost, "/cgi-bin/psowrte.py", nil, url.Values{})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	data, _ := os.ReadFile(cfg.ChatFile)
	if !strings.Contains(string(data), "&nbsp;") {
		t.Errorf("defaults not applied: %s", data)
	}
	if !strings.Contains(string(data), "class='def'") {
		t.Errorf("default color not applied: %s", data)
	}
}

func TestShoutboxWriteError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	srv, cfg, _ := testServer(t)
	// A directory where the chat file belongs makes the write fail.
	if err := os.MkdirAll(cfg.ChatFile, 0o755); err != nil {
		t.Fatal(err)
	}
	rec := do(t, srv, http.MethodPost, "/cgi-bin/psowrte.py", nil, url.Values{"data": {"x"}})
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestShoutboxGlobalChatDoesNotStoreLocally(t *testing.T) {
	srv, cfg, _ := testServer(t)
	cfg.GlobalChat = true
	cfg.GlobalDest = []string{"255.255.255.255"}
	fake := &captureSender{}
	srv.opts.Broadcaster = &shoutbox.Broadcaster{Destinations: cfg.GlobalDest, Sender: fake}
	rec := do(t, srv, http.MethodPost, "/cgi-bin/psowrte.py", nil, url.Values{"data": {"x"}})
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
	if len(fake.messages) != 1 {
		t.Errorf("broadcasts = %d, want 1", len(fake.messages))
	}
	if _, err := os.Stat(cfg.ChatFile); err == nil {
		t.Error("global chat mode should not write locally")
	}
}

func TestNewInstallsDefaultBroadcaster(t *testing.T) {
	srv := New(Options{})
	if srv.opts.Broadcaster == nil {
		t.Fatal("New should install a broadcaster")
	}
	if srv.opts.Broadcaster.Port != shoutbox.Port {
		t.Errorf("broadcast port = %d", srv.opts.Broadcaster.Port)
	}
}

func TestConfigJSONDefaultsModuleFiles(t *testing.T) {
	srv, _, _ := testServer(t)
	data, err := srv.ConfigJSON(ConfigModules{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "modules/chat.html") || !strings.Contains(string(data), "modules/upload.html") {
		t.Errorf("default module files missing: %s", data)
	}
}

func TestVersionFallsBackToBuildVersion(t *testing.T) {
	srv, cfg, _ := testServer(t)
	os.Remove(cfg.VersionFile)
	if got := srv.version(); got != "test-version" {
		t.Errorf("version = %q", got)
	}
	// An empty version file also falls back.
	if err := os.WriteFile(cfg.VersionFile, []byte("\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := srv.version(); got != "test-version" {
		t.Errorf("version with blank file = %q", got)
	}
}

func TestListenAddrHelpers(t *testing.T) {
	srv, cfg, _ := testServer(t)
	cfg.HTTPPort = 8080
	cfg.DroopyPort = "9090"
	if got := srv.ListenAddr(); got != ":8080" {
		t.Errorf("ListenAddr = %q", got)
	}
	if got := srv.UploadListenAddr(); got != ":9090" {
		t.Errorf("UploadListenAddr = %q", got)
	}
	cfg.HTTPAddr = "127.0.0.1"
	if got := srv.ListenAddr(); got != "127.0.0.1:8080" {
		t.Errorf("ListenAddr with host = %q", got)
	}
}

func TestListenAndServeUploadShutdown(t *testing.T) {
	srv, _, _ := testServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServeUpload(ctx, "127.0.0.1:0") }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("upload server error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("upload server did not stop")
	}
}

func TestBoardWithoutTrailingSlashRedirects(t *testing.T) {
	srv, _, _ := testServer(t)
	rec := do(t, srv, http.MethodGet, "/board", nil, nil)
	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("status = %d, want 301", rec.Code)
	}
}

func TestSharedWithoutTrailingSlashRedirects(t *testing.T) {
	srv, _, _ := testServer(t)
	rec := do(t, srv, http.MethodGet, "/Shared", nil, nil)
	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("status = %d, want 301", rec.Code)
	}
}

func TestUploadHandlerChmodParsing(t *testing.T) {
	srv, cfg, _ := testServer(t)
	cfg.DroopyChmod = "0640"
	h := srv.uploadHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
	// Invalid chmod values are ignored rather than fatal.
	cfg.DroopyChmod = "not-octal"
	if h := srv.uploadHandler(); h == nil {
		t.Error("handler should still be built")
	}
}

func TestUploadHandlerUserDirsFromConfig(t *testing.T) {
	srv, cfg, _ := testServer(t)
	cfg.DroopyUserDir = " -u True "
	h := srv.uploadHandler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), "advanced options") {
		t.Errorf("user dir form missing: %s", rec.Body.String())
	}
}

func TestServerDefaultsWithoutOptions(t *testing.T) {
	// New must fill in every dependency so a bare call works.
	srv := New(Options{Cfg: nil, Logger: nil})
	if srv.opts.Cfg == nil || srv.opts.Logger == nil || srv.opts.Shoutbox == nil ||
		srv.opts.Forum == nil || srv.opts.DiskUsage == nil || srv.opts.ContentFS == nil {
		t.Errorf("New did not fill defaults: %+v", srv.opts)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/content/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("default server status = %d", rec.Code)
	}
}

func TestCaptivePortalCaseInsensitive(t *testing.T) {
	srv, _, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Header.Set("User-Agent", "CAPTIVENETWORKSUPPORT/1.0")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Success") {
		t.Errorf("uppercase agent not detected: %s", rec.Body.String())
	}
}

func TestConfigJSONDisabled(t *testing.T) {
	srv, cfg, _ := testServer(t)
	cfg.PbxJSONGeneration = false
	rec := do(t, srv, http.MethodGet, "/piratebox_config.json", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when generation is disabled", rec.Code)
	}
}

func TestDiskUsageFallbackMatchesGeneratedFrame(t *testing.T) {
	srv, cfg, _ := testServer(t)
	srv.opts.DiskUsage.Path = filepath.Join(cfg.GenDiskFile, "impossible", "out.html")
	rec := do(t, srv, http.MethodGet, "/diskusage.html", nil, nil)
	body := rec.Body.String()
	if !strings.Contains(body, "GENERATOR") || !strings.Contains(body, "aria-valuenow") {
		t.Errorf("fallback should use the same frame as the generated page: %s", body)
	}
}
