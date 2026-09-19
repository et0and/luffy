// Package stations ports bin/station_cnt.sh, which counted associated WiFi
// clients every two minutes and wrote a small HTML snippet for the start
// page.
package stations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"luffy/internal/proc"
)

// Counter returns the number of associated stations.
type Counter interface {
	Count(ctx context.Context) (int, error)
}

// IWCounter counts stations with `iw dev <iface> station dump`.
type IWCounter struct {
	Interface string
	Runner    proc.Runner
	Timeout   time.Duration
}

// Count implements Counter.
func (c *IWCounter) Count(ctx context.Context) (int, error) {
	runner := c.Runner
	if runner == nil {
		runner = proc.Exec{}
	}
	iface := c.Interface
	if iface == "" {
		iface = "wlan0"
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	out, err := runner.Run(ctx, proc.Command{
		Name:    "iw",
		Args:    []string{"dev", iface, "station", "dump"},
		Timeout: timeout,
	})
	if err != nil {
		return 0, err
	}
	return ParseStationDump(out), nil
}

// ParseStationDump counts "Station" markers, matching
// `grep Station | wc -l`.
func ParseStationDump(output string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "Station") {
			count++
		}
	}
	return count
}

// Render produces the anchor tag from station_cnt.sh.
func Render(count int) string {
	return fmt.Sprintf("<a title='Counter is refreshed every 2 minutes, you need to refresh the page'>Currently there are %d connected clients</a>", count)
}

// Tracker periodically refreshes a generated station count file.
type Tracker struct {
	Counter  Counter
	Path     string
	Interval time.Duration

	mu    sync.Mutex
	count int
}

// NewTracker returns a tracker writing to path every interval.
func NewTracker(counter Counter, path string, interval time.Duration) *Tracker {
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	return &Tracker{Counter: counter, Path: path, Interval: interval}
}

// Refresh updates the cached count and writes the file.
func (t *Tracker) Refresh(ctx context.Context) error {
	count, err := t.Counter.Count(ctx)
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.count = count
	t.mu.Unlock()
	return writeFileAtomic(t.Path, []byte(Render(count)))
}

// Run refreshes immediately and then on every tick until ctx is cancelled.
// Count errors are ignored so a missing interface never stops the server.
func (t *Tracker) Run(ctx context.Context) error {
	_ = t.Refresh(ctx)
	ticker := time.NewTicker(t.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			_ = t.Refresh(ctx)
		}
	}
}

// Count returns the last known count.
func (t *Tracker) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.count
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("stations: create dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".luffy-cnt-*")
	if err != nil {
		return fmt.Errorf("stations: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("stations: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("stations: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("stations: rename: %w", err)
	}
	return nil
}
