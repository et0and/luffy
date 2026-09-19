package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
