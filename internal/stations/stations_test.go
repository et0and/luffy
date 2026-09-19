package stations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"luffy/internal/proc"
)

const sampleDump = `Station 00:11:22:33:44:55 (on wlan0)
	inactive time:	10 ms
	rx bytes:	1234
Station 66:77:88:99:aa:bb (on wlan0)
	inactive time:	20 ms
`

func TestParseStationDump(t *testing.T) {
	if got := ParseStationDump(sampleDump); got != 2 {
		t.Errorf("count = %d, want 2", got)
	}
	if got := ParseStationDump(""); got != 0 {
		t.Errorf("empty count = %d, want 0", got)
	}
	if got := ParseStationDump("no clients here\n"); got != 0 {
		t.Errorf("no station count = %d, want 0", got)
	}
}

func TestIWCounter(t *testing.T) {
	runner := proc.NewFake()
	runner.Outputs["iw"] = sampleDump
	c := &IWCounter{Interface: "wlan0", Runner: runner}
	count, err := c.Count(context.Background())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if !runner.Has("iw", "dev", "wlan0", "station", "dump") {
		t.Errorf("command not recorded: %v", runner.Lines())
	}
}

func TestIWCounterError(t *testing.T) {
	runner := proc.NewFake()
	runner.Errors["iw"] = errors.New("no such device")
	c := &IWCounter{Interface: "wlan0", Runner: runner}
	if _, err := c.Count(context.Background()); err == nil {
		t.Error("expected error when iw fails")
	}
}

func TestRender(t *testing.T) {
	got := Render(3)
	want := "<a title='Counter is refreshed every 2 minutes, you need to refresh the page'>Currently there are 3 connected clients</a>"
	if got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

type fixedCounter struct{ count int }

func (f fixedCounter) Count(context.Context) (int, error) { return f.count, nil }

func TestTrackerRefresh(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "station_cnt.txt")
	tracker := NewTracker(fixedCounter{count: 4}, out, time.Minute)
	if err := tracker.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Currently there are 4 connected clients") {
		t.Errorf("file = %q", data)
	}
	if tracker.Count() != 4 {
		t.Errorf("cached count = %d", tracker.Count())
	}
}

func TestTrackerRunStopsOnCancel(t *testing.T) {
	dir := t.TempDir()
	tracker := NewTracker(fixedCounter{count: 1}, filepath.Join(dir, "station_cnt.txt"), 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- tracker.Run(ctx) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("run returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tracker did not stop")
	}
}

type failingCounter struct{}

func (failingCounter) Count(context.Context) (int, error) { return 0, errors.New("boom") }

func TestTrackerRefreshError(t *testing.T) {
	tracker := NewTracker(failingCounter{}, filepath.Join(t.TempDir(), "cnt.txt"), time.Minute)
	if err := tracker.Refresh(context.Background()); err == nil {
		t.Error("expected refresh error")
	}
}
