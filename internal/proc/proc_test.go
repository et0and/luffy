package proc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExecRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell required")
	}
	out, err := (Exec{}).Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "echo hello"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(out) != "hello" {
		t.Errorf("output = %q", out)
	}
}

func TestExecRunIncludesOutputInError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell required")
	}
	_, err := (Exec{}).Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "echo boom >&2; exit 3"}})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %v", err)
	}
}

func TestExecRunTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell required")
	}
	_, err := (Exec{}).Run(context.Background(), Command{
		Name:    "sh",
		Args:    []string{"-c", "sleep 5"},
		Timeout: 50 * time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %v", err)
	}
}

func TestFakeRunner(t *testing.T) {
	fake := NewFake()
	fake.Outputs["iw"] = "Station a\nStation b\n"
	out, err := fake.Run(context.Background(), Command{Name: "iw", Args: []string{"dev", "wlan0", "station", "dump"}})
	if err != nil || out != fake.Outputs["iw"] {
		t.Fatalf("fake run = %q, %v", out, err)
	}
	if !fake.Has("iw", "station", "dump") {
		t.Errorf("Has failed: %v", fake.Lines())
	}
	if fake.Has("iw", "never-run") {
		t.Error("Has should be false")
	}
	fake.Errors["hostapd"] = errors.New("missing")
	if _, err := fake.Start(context.Background(), Command{Name: "hostapd"}); err == nil {
		t.Error("expected fake start error")
	}
	pid, err := fake.Start(context.Background(), Command{Name: "dnsmasq"})
	if err != nil || pid == 0 {
		t.Errorf("fake start = %d, %v", pid, err)
	}
	if names := fake.Names(); len(names) != 3 {
		t.Errorf("names = %v", names)
	}
}

func TestExecStartDetachesProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix process groups required")
	}
	marker := filepath.Join(t.TempDir(), "marker.txt")
	pid, err := (Exec{}).Start(context.Background(), Command{
		Name: "sh",
		Args: []string{"-c", "sleep 0.2; echo done > " + marker},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("pid = %d", pid)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("background process did not run to completion")
}

func TestExecStartWithLogDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell required")
	}
	logDir := t.TempDir()
	pid, err := (Exec{LogDir: logDir}).Start(context.Background(), Command{
		Name: "sh",
		Args: []string{"-c", "echo logged-output"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("pid = %d", pid)
	}
	logFile := filepath.Join(logDir, "sh.log")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(logFile)
		if err == nil && strings.Contains(string(data), "logged-output") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("process output was not written to the log file")
}

func TestExecStartMissingBinary(t *testing.T) {
	if _, err := (Exec{}).Start(context.Background(), Command{Name: "definitely-not-a-real-binary-xyz"}); err == nil {
		t.Error("expected start error for missing binary")
	}
}

func TestExecRunWithEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell required")
	}
	out, err := (Exec{}).Run(context.Background(), Command{
		Name: "sh",
		Args: []string{"-c", "echo $LUFFY_TEST_VAR"},
		Env:  []string{"LUFFY_TEST_VAR=hello-env"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(out) != "hello-env" {
		t.Errorf("output = %q", out)
	}
}

func TestExecRunWithWorkingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell required")
	}
	dir := t.TempDir()
	out, err := (Exec{}).Run(context.Background(), Command{Name: "pwd", Dir: dir})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// macOS reports /private/var for /var symlinks.
	if !strings.Contains(out, filepath.Base(dir)) {
		t.Errorf("output = %q, want it to contain %q", out, filepath.Base(dir))
	}
}

func TestLogPathStripsDirectories(t *testing.T) {
	got := logPath("/tmp/logs", Command{Name: "/usr/sbin/hostapd", Args: []string{"-x"}})
	if got != filepath.Join("/tmp/logs", "hostapd.log") {
		t.Errorf("logPath = %q", got)
	}
}
