// Package hooks ports the bin/hooks/*.sh extension points.
//
// Users could drop executable scripts into /opt/piratebox/bin/hooks to run
// code before and after start, stop and install. Luffy runs the same files
// with the configuration file path as the first argument, so existing hooks
// keep working.
package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"luffy/internal/proc"
)

// Names of the hook stages, matching the original file names.
const (
	PreInit            = "hook_pre_init.sh"
	PostInit           = "hook_post_init.sh"
	PreOpenwrtInit     = "hook_pre_openwrt_init.sh"
	PirateboxStart     = "hook_piratebox_start.sh"
	PirateboxStartDone = "hook_piratebox_start_done.sh"
	PirateboxStop      = "hook_piratebox_stop.sh"
	PirateboxStopDone  = "hook_piratebox_stop_done.sh"
)

// Runner executes hook scripts.
type Runner struct {
	Dir        string
	ConfigPath string
	Runner     proc.Runner
	Logger     *slog.Logger
	Timeout    time.Duration
	// IncludeFiles also runs user supplied *.sh files in the hook directory
	// in alphabetical order. The original init script only called the fixed
	// names, but people commonly added their own.
	IncludeFiles bool
}

// New returns a runner for the hook directory.
func New(dir, configPath string) *Runner {
	return &Runner{Dir: dir, ConfigPath: configPath, Timeout: 60 * time.Second}
}

// Stages lists the hooks that exist in the directory.
func (r *Runner) Stages() ([]string, error) {
	entries, err := os.ReadDir(r.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("hooks: read %s: %w", r.Dir, err)
	}
	var found []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sh") {
			continue
		}
		found = append(found, entry.Name())
	}
	sort.Strings(found)
	return found, nil
}

// Run executes one hook. A missing hook is not an error, matching the
// original shell behaviour. A failing hook stops the sequence.
func (r *Runner) Run(ctx context.Context, name string) error {
	path := filepath.Join(r.Dir, name)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("hooks: stat %s: %w", path, err)
	}
	if info.IsDir() {
		return nil
	}
	runner := r.Runner
	if runner == nil {
		runner = proc.Exec{}
	}
	args := []string{}
	if r.ConfigPath != "" {
		args = append(args, r.ConfigPath)
	}
	output, err := runner.Run(ctx, proc.Command{
		Name:    "/bin/sh",
		Args:    append([]string{path}, args...),
		Timeout: r.Timeout,
	})
	if r.Logger != nil {
		for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
			if line == "" {
				continue
			}
			r.Logger.Debug("hook output", "hook", name, "line", line)
		}
	}
	if err != nil {
		return fmt.Errorf("hooks: %s failed: %w", name, err)
	}
	return nil
}

// RunAll executes the fixed hook order, optionally including user scripts.
func (r *Runner) RunAll(ctx context.Context, stages ...string) error {
	for _, stage := range stages {
		if err := r.Run(ctx, stage); err != nil {
			return err
		}
	}
	if r.IncludeFiles {
		return r.runUserScripts(ctx, stages)
	}
	return nil
}

func (r *Runner) runUserScripts(ctx context.Context, skip []string) error {
	known := map[string]bool{}
	for _, name := range skip {
		known[name] = true
	}
	all, err := r.Stages()
	if err != nil {
		return err
	}
	for _, name := range all {
		if known[name] || !strings.HasPrefix(name, "hook_") {
			continue
		}
		if err := r.Run(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

// StartStages returns the hooks run during startup.
func StartStages() []string {
	return []string{PirateboxStart}
}

// StartDoneStages returns the hooks run after startup.
func StartDoneStages() []string {
	return []string{PirateboxStartDone}
}

// StopStages returns the hooks run before shutdown.
func StopStages() []string {
	return []string{PirateboxStop}
}

// StopDoneStages returns the hooks run after shutdown.
func StopDoneStages() []string {
	return []string{PirateboxStopDone}
}

// InitStages returns the hooks run on first start.
func InitStages() []string {
	return []string{PreInit, PostInit}
}
