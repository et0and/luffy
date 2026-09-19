package atomicfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCreatesParentsAndFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "nested", "dir", "file.txt")
	if err := WriteString(target, "hello"); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q", data)
	}
}

func TestWriteReplacesExisting(t *testing.T) {
	target := filepath.Join(t.TempDir(), "file.txt")
	if err := WriteString(target, "first"); err != nil {
		t.Fatal(err)
	}
	if err := WriteString(target, "second"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "second" {
		t.Errorf("content = %q", data)
	}
}

func TestWriteLeavesNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "file.txt")
	if err := WriteString(target, "data"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".luffy-") {
			t.Errorf("temporary file left behind: %s", entry.Name())
		}
	}
}

func TestWriteFailureKeepsOldContent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "file.txt")
	if err := WriteString(target, "original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	if err := WriteString(target, "replacement"); err == nil {
		t.Fatal("expected write error in read-only directory")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original" {
		t.Errorf("original content lost: %q", data)
	}
}

func TestWriteCreateTempFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "file.txt")
	// A read-only directory lets MkdirAll succeed (it already exists) but
	// makes CreateTemp fail, covering that error branch.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	err := WriteString(target, "data")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "temp file") {
		t.Errorf("error = %v, want a temp file error", err)
	}
}

func TestWriteEmptyContent(t *testing.T) {
	target := filepath.Join(t.TempDir(), "empty.txt")
	if err := WriteString(target, ""); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Errorf("size = %d, want 0", info.Size())
	}
}
