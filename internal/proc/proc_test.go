package proc

import (
	"context"
	"errors"
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
	if names := fake.SortedNames(); len(names) != 3 {
		t.Errorf("names = %v", names)
	}
}
