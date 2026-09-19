package hooks

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"luffy/internal/proc"
)

func writeHook(t *testing.T, dir, name, body string, mode os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func newRunner(t *testing.T, dir string) (*Runner, *proc.Fake) {
	t.Helper()
	fake := proc.NewFake()
	r := &Runner{
		Dir:        dir,
		ConfigPath: "/opt/piratebox/conf/piratebox.conf",
		Runner:     fake,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return r, fake
}

func TestMissingHookIsNotAnError(t *testing.T) {
	r, _ := newRunner(t, t.TempDir())
	if err := r.Run(context.Background(), PirateboxStart); err != nil {
		t.Errorf("missing hook should not fail: %v", err)
	}
}

func TestHookRunsWithConfigArgument(t *testing.T) {
	dir := t.TempDir()
	path := writeHook(t, dir, PirateboxStart, "#!/bin/sh\necho ran\n", 0o755)
	r, fake := newRunner(t, dir)
	if err := r.Run(context.Background(), PirateboxStart); err != nil {
		t.Fatal(err)
	}
	if !fake.Has("/bin/sh", path, r.ConfigPath) {
		t.Errorf("hook invocation wrong: %v", fake.Lines())
	}
}

func TestFailingHookReturnsError(t *testing.T) {
	dir := t.TempDir()
	writeHook(t, dir, PirateboxStart, "#!/bin/sh\nexit 1\n", 0o755)
	r, fake := newRunner(t, dir)
	fake.Errors["/bin/sh"] = errors.New("exit status 1")
	if err := r.Run(context.Background(), PirateboxStart); err == nil {
		t.Error("expected hook error")
	}
}

func TestRunAllStopsOnFailure(t *testing.T) {
	dir := t.TempDir()
	writeHook(t, dir, PirateboxStart, "#!/bin/sh\n", 0o755)
	writeHook(t, dir, PirateboxStartDone, "#!/bin/sh\n", 0o755)
	r, fake := newRunner(t, dir)
	fake.Errors["/bin/sh"] = errors.New("boom")
	if err := r.RunAll(context.Background(), PirateboxStart, PirateboxStartDone); err == nil {
		t.Error("expected failure")
	}
	if len(fake.Commands) != 1 {
		t.Errorf("second hook should not run: %v", fake.Lines())
	}
}

func TestStagesListsScriptsSorted(t *testing.T) {
	dir := t.TempDir()
	writeHook(t, dir, "hook_zzz.sh", "", 0o755)
	writeHook(t, dir, "hook_aaa.sh", "", 0o755)
	writeHook(t, dir, "notes.txt", "", 0o644)
	r, _ := newRunner(t, dir)
	stages, err := r.Stages()
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 2 || stages[0] != "hook_aaa.sh" || stages[1] != "hook_zzz.sh" {
		t.Errorf("stages = %v", stages)
	}
}

func TestStagesMissingDirectory(t *testing.T) {
	r, _ := newRunner(t, filepath.Join(t.TempDir(), "missing"))
	stages, err := r.Stages()
	if err != nil || stages != nil {
		t.Errorf("missing dir = %v, %v", stages, err)
	}
}

func TestIncludeUserScripts(t *testing.T) {
	dir := t.TempDir()
	writeHook(t, dir, PirateboxStart, "", 0o755)
	writeHook(t, dir, "hook_my_custom.sh", "", 0o755)
	r, fake := newRunner(t, dir)
	r.IncludeFiles = true
	if err := r.RunAll(context.Background(), PirateboxStart); err != nil {
		t.Fatal(err)
	}
	if !fake.Has("hook_my_custom.sh") {
		t.Errorf("custom hook not run: %v", fake.Lines())
	}
}

func TestHookArgumentsDoNotLeakSpaces(t *testing.T) {
	dir := t.TempDir()
	writeHook(t, dir, PirateboxStop, "", 0o755)
	r, fake := newRunner(t, dir)
	r.ConfigPath = "/opt/pirate box/conf/piratebox.conf"
	if err := r.Run(context.Background(), PirateboxStop); err != nil {
		t.Fatal(err)
	}
	if !fake.Has("pirate box") {
		t.Errorf("config path lost: %v", fake.Lines())
	}
}

func TestStageHelpers(t *testing.T) {
	if got := StartStages(); len(got) != 1 || got[0] != PirateboxStart {
		t.Errorf("StartStages = %v", got)
	}
	if got := StopStages(); got[0] != PirateboxStop {
		t.Errorf("StopStages = %v", got)
	}
	if got := InitStages(); !strings.Contains(strings.Join(got, ","), PreInit) {
		t.Errorf("InitStages = %v", got)
	}
}
