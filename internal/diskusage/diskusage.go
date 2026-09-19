// Package diskusage ports python_lib/diskusage.py and psutil.disk_usage.
//
// The original produced a Bootstrap progress bar in a static HTML fragment
// so the main page could show how full the shared drive is. Refresh requests
// were throttled to one every five minutes.
package diskusage

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// DefaultDelay matches the original `delay = 60*5`.
const DefaultDelay = 5 * time.Minute

// Usage is statfs output in bytes.
type Usage struct {
	Total uint64
	Used  uint64
	Free  uint64
}

// StatFS reports disk usage for path. Tests replace it.
type StatFS func(path string) (Usage, error)

// Generator writes the diskusage.html fragment.
type Generator struct {
	// Drive is the shared folder to measure.
	Drive string
	// Path is the generated HTML file.
	Path string
	// Delay is the refresh throttle.
	Delay time.Duration
	// Now can be replaced in tests.
	Now func() time.Time
	// Stat can be replaced in tests.
	Stat StatFS

	mu sync.Mutex
}

// New returns a generator with production defaults.
func New(drive, path string) *Generator {
	return &Generator{Drive: drive, Path: path, Delay: DefaultDelay, Now: time.Now, Stat: SystemStatFS}
}

// SystemStatFS implements StatFS with the statfs(2) system call.
func SystemStatFS(path string) (Usage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Usage{}, fmt.Errorf("diskusage: statfs %s: %w", path, err)
	}
	blockSize := uint64(st.Bsize)
	total := st.Blocks * blockSize
	free := st.Bavail * blockSize
	used := (st.Blocks - st.Bfree) * blockSize
	return Usage{Total: total, Used: used, Free: free}, nil
}

// Percent returns the used percentage as an integer, or 0 on error, exactly
// like the original FreeSpace helper despite its misleading name.
func (g *Generator) Percent() int {
	stat := g.Stat
	if stat == nil {
		stat = SystemStatFS
	}
	usage, err := stat(g.Drive)
	if err != nil || usage.Total == 0 {
		return 0
	}
	return int(100 * float64(usage.Used) / float64(usage.Total))
}

// Line renders the progress bar for the current usage.
func (g *Generator) Line() string {
	return PrepareLine(g.Percent())
}

// PrepareLine renders the exact progress bar markup from diskusage.py.
func PrepareLine(percent int) string {
	data := fmt.Sprintf("%d", percent)
	return "<div class='progress'><div class='progress-bar' role='progressbar' aria-valuenow='" + data +
		"' aria-valuemin='0' aria-valuemax='100' style='width: " + data +
		"%'><span class='sr-only'>" + data + "% Full</span></div></div>"
}

// Page wraps content in the original HTML frame.
func Page(content string) string {
	return "<html><head><meta http-equiv='cache-control' content='no-cache'>" +
		"<meta name='GENERATOR' content='disk usage'><title>DiskUsage Data</title></head><body>" +
		content + "</body></html>"
}

// Stale reports whether the generated file is missing or older than Delay.
// A fresh file throttles refresh requests, matching the original five
// minute guard.
func (g *Generator) Stale() (bool, error) {
	info, err := os.Stat(g.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("diskusage: stat %s: %w", g.Path, err)
	}
	age := g.now().Sub(info.ModTime())
	return age >= g.delay(), nil
}

// Refresh regenerates the file unless the throttle window is open. It
// returns true when the file was written.
func (g *Generator) Refresh(force bool) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !force {
		stale, err := g.Stale()
		if err != nil {
			return false, err
		}
		if !stale {
			return false, nil
		}
	}
	return true, g.writeLocked()
}

// Write always regenerates the file.
func (g *Generator) Write() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.writeLocked()
}

func (g *Generator) writeLocked() error {
	if err := os.MkdirAll(filepath.Dir(g.Path), 0o755); err != nil {
		return fmt.Errorf("diskusage: create dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(g.Path), ".luffy-du-*")
	if err != nil {
		return fmt.Errorf("diskusage: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(Page(g.Line())); err != nil {
		tmp.Close()
		return fmt.Errorf("diskusage: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("diskusage: close: %w", err)
	}
	if err := os.Rename(tmpName, g.Path); err != nil {
		return fmt.Errorf("diskusage: rename: %w", err)
	}
	return nil
}

func (g *Generator) delay() time.Duration {
	if g.Delay <= 0 {
		return DefaultDelay
	}
	return g.Delay
}

func (g *Generator) now() time.Time {
	if g.Now == nil {
		return time.Now()
	}
	return g.Now()
}
