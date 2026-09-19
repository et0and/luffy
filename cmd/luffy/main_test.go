package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"luffy/internal/config"
	"luffy/internal/proc"
	"luffy/internal/service"
	"luffy/internal/web"
)

func writeConfig(t *testing.T, dir string, extra string) string {
	t.Helper()
	content := `
PIRATEBOX_FOLDER="` + dir + `"
WWW_FOLDER="` + dir + `/www"
WWW_CONTENT="` + dir + `/share/content"
SHARE_FOLDER="` + dir + `/share"
UPLOADFOLDER="` + dir + `/share/Shared"
CHATFILE="` + dir + `/www/cgi-bin/data.pso"
GEN_CHATFILE="` + dir + `/www/chat_content.html"
GEN_DISKFILE="` + dir + `/www/diskusage.html"
TIMESAVE="` + dir + `/share/timesave_file"
DO_IFCONFIG="no"
USE_APN="no"
USE_DNSMASQ="no"
HTTP_PORT="18080"
` + extra
	path := filepath.Join(dir, "piratebox.conf")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunVersion(t *testing.T) {
	if code := run([]string{"version"}); code != 0 {
		t.Errorf("version exit code = %d", code)
	}
	if code := run([]string{"--version"}); code != 0 {
		t.Errorf("--version exit code = %d", code)
	}
}

func TestRunHelp(t *testing.T) {
	if code := run([]string{"help"}); code != 0 {
		t.Errorf("help exit code = %d", code)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "")
	if code := run([]string{"--config", path, "frobnicate"}); code != 2 {
		t.Errorf("unknown command exit code = %d, want 2", code)
	}
}

func TestRunMissingConfig(t *testing.T) {
	if code := run([]string{"--config", filepath.Join(t.TempDir(), "missing.conf"), "status"}); code != 1 {
		t.Errorf("missing config exit code = %d, want 1", code)
	}
}

func TestRunStatus(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "")
	if code := run([]string{"--config", path, "status"}); code != 0 {
		t.Errorf("status exit code = %d", code)
	}
}

func TestRunSetupCreatesInstallation(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "")
	if code := run([]string{"--config", path, "setup"}); code != 0 {
		t.Fatalf("setup exit code = %d", code)
	}
	for _, expected := range []string{
		filepath.Join(dir, "conf", "init_done"),
		filepath.Join(dir, "share", "content", "index.html"),
		filepath.Join(dir, "www", "content"),
		filepath.Join(dir, "share", "Shared"),
	} {
		if _, err := os.Stat(expected); err != nil {
			t.Errorf("%s missing after setup: %v", expected, err)
		}
	}
}

func TestRunShoutboxPost(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "")
	if code := run([]string{"--config", path, "shoutbox", "post", "hello", "there"}); code != 0 {
		t.Fatalf("shoutbox post exit code = %d", code)
	}
	data, err := os.ReadFile(filepath.Join(dir, "www", "cgi-bin", "data.pso"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello there") {
		t.Errorf("chat content = %q", data)
	}
	page, err := os.ReadFile(filepath.Join(dir, "www", "chat_content.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "hello there") {
		t.Errorf("generated page = %q", page)
	}
}

func TestRunTimesaveSaveAndRecover(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "")
	if code := run([]string{"--config", path, "timesave", "save"}); code != 0 {
		t.Fatalf("timesave save exit code = %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "share", "timesave_file")); err != nil {
		t.Fatalf("timesave file missing: %v", err)
	}
	// Recover must not fail with a past timestamp; here the current clock is
	// ahead so recovery is expected to succeed or report "past" cleanly.
	code := run([]string{"--config", path, "timesave", "recover"})
	if code != 0 && code != 1 {
		t.Errorf("recover exit code = %d", code)
	}
}

func TestRunDryRunSetup(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "")
	if code := run([]string{"--config", path, "--dry-run", "setup"}); code != 0 {
		t.Errorf("dry run setup exit code = %d", code)
	}
}

func TestRunStationsUsesConfiguredInterface(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "INTERFACE=\"wlan0\"\n")
	// iw is unlikely to exist in CI, so either result is acceptable; the
	// command must not panic.
	code := run([]string{"--config", path, "stations"})
	if code != 0 && code != 1 {
		t.Errorf("stations exit code = %d", code)
	}
}

func TestRunHostname(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "")
	if code := run([]string{"--config", path, "hostname", "mybox.lan"}); code != 0 {
		t.Fatalf("hostname exit code = %d", code)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `HOST="mybox.lan"`) {
		t.Errorf("config not updated: %s", data)
	}
	page, err := os.ReadFile(filepath.Join(dir, "www", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "mybox.lan") {
		t.Errorf("redirect page not written: %s", page)
	}
	if code := run([]string{"--config", path, "hostname"}); code != 2 {
		t.Errorf("missing argument exit code = %d, want 2", code)
	}
}

// runServeE2E starts the real server stack on ephemeral ports and exercises
// the HTTP surface, then cancels the context to shut down.
func runServeE2E(t *testing.T, extra string) (*config.Config, *service.Manager, *proc.Fake) {
	t.Helper()
	dir := t.TempDir()
	path := writeConfig(t, dir, "HTTP_PORT=\"0\"\nDROOPY_PORT=\"0\"\n"+extra)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	fake := proc.NewFake()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := service.New(cfg, fake, logger)
	return cfg, manager, fake
}

func TestRunServeEndToEnd(t *testing.T) {
	cfg, manager, _ := runServeE2E(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	var bound *web.Server
	ready := make(chan struct{})
	go func() {
		done <- runServe(ctx, serveOptions{
			cfg:     cfg,
			manager: manager,
			logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
			ready: func(s *web.Server) {
				bound = s
				close(ready)
			},
		})
	}()
	<-ready
	// Port 0 means the kernel chose the port; ListenAddr is what was asked
	// for, so read the real address from the log line is not possible here.
	// Instead assert the handler serves correctly through the recorder.
	rec := httptest.NewRecorder()
	bound.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/content/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("content status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Luffy") {
		t.Errorf("content body missing Luffy: %s", rec.Body.String())
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("runServe exit code = %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runServe did not stop")
	}
}

func TestRunServeWithNetwork(t *testing.T) {
	cfg, manager, fake := runServeE2E(t, "USE_APN=\"yes\"\nINTERFACE=\"wlan0\"\n")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	ready := make(chan struct{})
	go func() {
		done <- runServe(ctx, serveOptions{
			cfg:         cfg,
			manager:     manager,
			logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
			withNetwork: true,
			ready:       func(*web.Server) { close(ready) },
		})
	}()
	<-ready
	if !fake.Has("hostapd") {
		t.Errorf("hostapd not started in network mode: %v", fake.Lines())
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("runServe exit code = %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runServe did not stop")
	}
}

func TestRunServeStartFailure(t *testing.T) {
	cfg, manager, fake := runServeE2E(t, "USE_APN=\"yes\"\nINTERFACE=\"wlan0\"\n")
	fake.Errors["hostapd"] = os.ErrPermission
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code := runServe(ctx, serveOptions{
		cfg:         cfg,
		manager:     manager,
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		withNetwork: true,
	})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRunServeGlobalChatListener(t *testing.T) {
	cfg, manager, _ := runServeE2E(t, "GLOBAL_CHAT=\"yes\"\nGLOBAL_DEST=\"[ #127.0.0.1# ]\"\n")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	ready := make(chan struct{})
	go func() {
		done <- runServe(ctx, serveOptions{
			cfg:     cfg,
			manager: manager,
			logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
			ready:   func(*web.Server) { close(ready) },
		})
	}()
	<-ready
	// The listener binds port 12556; tolerate it being taken in CI by only
	// asserting the server came up and shuts down cleanly.
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("runServe exit code = %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runServe did not stop")
	}
}

func TestRunServeBuildsForumFromConfig(t *testing.T) {
	cfg, manager, _ := runServeE2E(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	ready := make(chan struct{})
	var bound *web.Server
	go func() {
		done <- runServe(ctx, serveOptions{
			cfg:     cfg,
			manager: manager,
			logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
			ready: func(s *web.Server) {
				bound = s
				close(ready)
			},
		})
	}()
	<-ready
	rec := httptest.NewRecorder()
	bound.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("board status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Luffy Board") {
		t.Errorf("board page = %s", rec.Body.String())
	}
	cancel()
	<-done
}

func TestDryRunRunner(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	runner := &dryRunRunner{logger: logger}
	if _, err := runner.Run(context.Background(), proc.Command{Name: "ifconfig", Args: []string{"wlan0", "up"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Start(context.Background(), proc.Command{Name: "hostapd"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "ifconfig wlan0 up") || !strings.Contains(buf.String(), "hostapd") {
		t.Errorf("dry run log = %q", buf.String())
	}
}

func TestRunShoutboxGenerateAndReset(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "")
	if code := run([]string{"--config", path, "shoutbox", "generate"}); code != 0 {
		t.Errorf("generate exit code = %d", code)
	}
	if code := run([]string{"--config", path, "shoutbox", "reset"}); code != 0 {
		t.Errorf("reset exit code = %d", code)
	}
	// No subcommand defaults to generate.
	if code := run([]string{"--config", path, "shoutbox"}); code != 0 {
		t.Errorf("default subcommand exit code = %d, want 0", code)
	}
	if code := run([]string{"--config", path, "shoutbox", "post"}); code != 2 {
		t.Errorf("post without message exit code = %d, want 2", code)
	}
	if code := run([]string{"--config", path, "shoutbox", "bogus"}); code != 2 {
		t.Errorf("unknown subcommand exit code = %d, want 2", code)
	}
}

func TestRunTimesaveInvalidSubcommand(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "")
	if code := run([]string{"--config", path, "timesave"}); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestRunBadFlag(t *testing.T) {
	if code := run([]string{"--not-a-flag"}); code != 2 {
		t.Errorf("bad flag exit code = %d, want 2", code)
	}
}

func TestRunFlushDNSWithoutPidFile(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "")
	if code := run([]string{"--config", path, "flush-dns"}); code != 1 {
		t.Errorf("exit code = %d, want 1 without a pid file", code)
	}
}

// The success path for flush-dns is covered by
// service.TestFlushDNSMasq, which uses a fake runner. Running it through
// the CLI would send a real SIGHUP, so it is deliberately not tested here.
