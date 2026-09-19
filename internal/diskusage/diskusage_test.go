package diskusage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrepareLineExact(t *testing.T) {
	got := PrepareLine(15)
	want := "<div class='progress'><div class='progress-bar' role='progressbar' aria-valuenow='15' aria-valuemin='0' aria-valuemax='100' style='width: 15%'><span class='sr-only'>15% Full</span></div></div>"
	if got != want {
		t.Errorf("PrepareLine = %q, want %q", got, want)
	}
}

func TestPageFrame(t *testing.T) {
	page := Page("CONTENT")
	for _, want := range []string{"<html>", "disk usage", "DiskUsage Data", "CONTENT", "</body></html>"} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %q: %s", want, page)
		}
	}
}

func TestPercent(t *testing.T) {
	g := New("/share", "/tmp/du.html")
	g.Stat = func(string) (Usage, error) {
		return Usage{Total: 100, Used: 15, Free: 85}, nil
	}
	if got := g.Percent(); got != 15 {
		t.Errorf("Percent = %d, want 15", got)
	}
	g.Stat = func(string) (Usage, error) { return Usage{}, os.ErrPermission }
	if got := g.Percent(); got != 0 {
		t.Errorf("Percent on error = %d, want 0", got)
	}
	g.Stat = func(string) (Usage, error) { return Usage{Total: 0}, nil }
	if got := g.Percent(); got != 0 {
		t.Errorf("Percent with zero total = %d, want 0", got)
	}
}

func TestRefreshThrottle(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "diskusage.html")
	g := New("/share", out)
	g.Stat = func(string) (Usage, error) { return Usage{Total: 100, Used: 50}, nil }

	written, err := g.Refresh(false)
	if err != nil || !written {
		t.Fatalf("first refresh written=%v err=%v", written, err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "aria-valuenow='50'") {
		t.Errorf("generated content wrong: %s", data)
	}

	written, err = g.Refresh(false)
	if err != nil || written {
		t.Errorf("fresh file should throttle refresh, written=%v err=%v", written, err)
	}

	old := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(out, old, old); err != nil {
		t.Fatal(err)
	}
	written, err = g.Refresh(false)
	if err != nil || !written {
		t.Errorf("stale file should refresh, written=%v err=%v", written, err)
	}

	written, err = g.Refresh(true)
	if err != nil || !written {
		t.Errorf("forced refresh should always write, written=%v err=%v", written, err)
	}
}

func TestWriteCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "nested", "dir", "diskusage.html")
	g := New("/share", out)
	g.Stat = func(string) (Usage, error) { return Usage{Total: 100, Used: 1}, nil }
	if err := g.Write(); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output missing: %v", err)
	}
}
