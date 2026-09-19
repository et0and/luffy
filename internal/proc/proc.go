// Package proc wraps external commands so the system integration layer can
// be tested without root access or real network interfaces.
package proc

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// Command describes one external program invocation.
type Command struct {
	Name    string
	Args    []string
	Dir     string
	Env     []string
	Timeout time.Duration
}

// String renders the command line for logs and test assertions.
func (c Command) String() string {
	parts := append([]string{c.Name}, c.Args...)
	return strings.Join(parts, " ")
}

// Runner runs external commands. Exec is the real implementation; Fake
// records invocations for tests.
type Runner interface {
	Run(ctx context.Context, cmd Command) (string, error)
	Start(ctx context.Context, cmd Command) (int, error)
}

// Exec runs real processes.
type Exec struct {
	// LogDir receives stdout and stderr of started background processes. An
	// empty LogDir sends them to os.DevNull.
	LogDir string
}

// Run executes cmd and returns its combined output.
func (e Exec) Run(ctx context.Context, cmd Command) (string, error) {
	if cmd.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cmd.Timeout)
		defer cancel()
	}
	c := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
	c.Dir = cmd.Dir
	if cmd.Env != nil {
		c.Env = cmd.Env
	}
	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf
	err := c.Run()
	output := buf.String()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return output, fmt.Errorf("%s: timed out after %s", cmd, cmd.Timeout)
		}
		return output, fmt.Errorf("%s: %w: %s", cmd, err, strings.TrimSpace(output))
	}
	return output, nil
}

// Start launches cmd in the background and returns its PID. The process is
// detached from the controlling terminal so it survives the parent.
func (e Exec) Start(ctx context.Context, cmd Command) (int, error) {
	c := exec.Command(cmd.Name, cmd.Args...)
	c.Dir = cmd.Dir
	if cmd.Env != nil {
		c.Env = cmd.Env
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return 0, fmt.Errorf("proc: open %s: %w", os.DevNull, err)
	}
	defer devNull.Close()
	stdout, stderr := devNull, devNull
	if e.LogDir != "" {
		logFile, err := os.OpenFile(logPath(e.LogDir, cmd), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			defer logFile.Close()
			stdout, stderr = logFile, logFile
		}
	}
	c.Stdout = stdout
	c.Stderr = stderr
	c.Stdin = devNull
	detach(c)
	if err := c.Start(); err != nil {
		return 0, fmt.Errorf("proc: start %s: %w", cmd, err)
	}
	pid := c.Process.Pid
	// Release the process; it is intentionally not waited for.
	go func() { _ = c.Wait() }()
	return pid, nil
}

func logPath(dir string, cmd Command) string {
	name := cmd.Name
	if idx := strings.LastIndexAny(name, `/\`); idx >= 0 {
		name = name[idx+1:]
	}
	return dir + string(os.PathSeparator) + name + ".log"
}

// Fake is a Runner for tests. It matches commands by name and returns the
// configured output.
type Fake struct {
	mu       sync.Mutex
	Commands []Command
	Outputs  map[string]string
	Errors   map[string]error
	PIDs     map[string]int
	nextPID  int
}

// NewFake returns an empty fake runner.
func NewFake() *Fake {
	return &Fake{
		Outputs: map[string]string{},
		Errors:  map[string]error{},
		PIDs:    map[string]int{},
		nextPID: 1000,
	}
}

// Run implements Runner.
func (f *Fake) Run(_ context.Context, cmd Command) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Commands = append(f.Commands, cmd)
	if err := f.Errors[cmd.Name]; err != nil {
		return f.Outputs[cmd.Name], err
	}
	return f.Outputs[cmd.Name], nil
}

// Start implements Runner.
func (f *Fake) Start(_ context.Context, cmd Command) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Commands = append(f.Commands, cmd)
	if err := f.Errors[cmd.Name]; err != nil {
		return 0, err
	}
	if pid, ok := f.PIDs[cmd.Name]; ok {
		return pid, nil
	}
	f.nextPID++
	f.PIDs[cmd.Name] = f.nextPID
	return f.nextPID, nil
}

// Names returns the names of all recorded commands in order.
func (f *Fake) Names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.Commands))
	for _, cmd := range f.Commands {
		names = append(names, cmd.Name)
	}
	return names
}

// Lines returns all recorded commands as command lines.
func (f *Fake) Lines() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	lines := make([]string, 0, len(f.Commands))
	for _, cmd := range f.Commands {
		lines = append(lines, cmd.String())
	}
	return lines
}

// Has reports whether any recorded command line contains all fragments.
func (f *Fake) Has(fragments ...string) bool {
	for _, line := range f.Lines() {
		all := true
		for _, frag := range fragments {
			if !strings.Contains(line, frag) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// SortedNames returns recorded command names sorted alphabetically. Useful
// for diagnostics.
func (f *Fake) SortedNames() []string {
	names := f.Names()
	sort.Strings(names)
	return names
}
