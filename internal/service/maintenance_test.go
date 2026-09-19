package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDistributeFilesCopiesIntoSubdirectories(t *testing.T) {
	root := t.TempDir()
	src := t.TempDir()
	if err := writeTestFile(filepath.Join(src, "HEADER.txt"), "header"); err != nil {
		t.Fatal(err)
	}
	if err := writeTestFile(filepath.Join(src, "README.txt"), "footer"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "photos", "2026"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := DistributeFiles(root, src, false); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{root, filepath.Join(root, "photos"), filepath.Join(root, "photos", "2026")} {
		for _, name := range []string{"HEADER.txt", "README.txt"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				t.Errorf("%s/%s missing: %v", dir, name, err)
			}
		}
	}
}

func TestDistributeFilesRespectsOverwrite(t *testing.T) {
	root := t.TempDir()
	src := t.TempDir()
	if err := writeTestFile(filepath.Join(src, "HEADER.txt"), "new header"); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(root, "HEADER.txt")
	if err := writeTestFile(existing, "custom header"); err != nil {
		t.Fatal(err)
	}
	if err := DistributeFiles(root, src, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(existing)
	if string(data) != "custom header" {
		t.Errorf("file overwritten without permission: %q", data)
	}
	if err := DistributeFiles(root, src, true); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(existing)
	if string(data) != "new header" {
		t.Errorf("overwrite failed: %q", data)
	}
}

func TestDeleteEmptyFiles(t *testing.T) {
	root := t.TempDir()
	if err := writeTestFile(filepath.Join(root, "empty.txt"), ""); err != nil {
		t.Fatal(err)
	}
	if err := writeTestFile(filepath.Join(root, "full.txt"), "content"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeTestFile(filepath.Join(root, "sub", "empty2"), ""); err != nil {
		t.Fatal(err)
	}
	deleted, err := DeleteEmptyFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}
	if _, err := os.Stat(filepath.Join(root, "full.txt")); err != nil {
		t.Errorf("non empty file removed: %v", err)
	}
}

func TestDeleteEmptyFilesRefusesRoot(t *testing.T) {
	for _, root := range []string{"", "/", "  "} {
		if _, err := DeleteEmptyFiles(root); err == nil {
			t.Errorf("DeleteEmptyFiles(%q) should be refused", root)
		}
	}
}

func TestRemovePartialUploads(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"tmpdroopy123", "TMPabc", "keep.txt"} {
		if err := writeTestFile(filepath.Join(root, name), "x"); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := RemovePartialUploads(root)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}
	if _, err := os.Stat(filepath.Join(root, "keep.txt")); err != nil {
		t.Errorf("keep.txt removed: %v", err)
	}
}

func TestFlushDNSMasq(t *testing.T) {
	m, fake, cfg, _ := testManager(t, nil)
	tmp := filepath.Join(cfg.PirateboxFolder, "tmp")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "dnsmasq.pid"), []byte("777\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.FlushDNSMasq(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fake.Has("kill", "-1", "777") {
		t.Errorf("SIGHUP not sent: %v", fake.Lines())
	}
}

func TestFlushDNSMasqMissingPidFile(t *testing.T) {
	m, _, _, _ := testManager(t, nil)
	if err := m.FlushDNSMasq(context.Background()); err == nil {
		t.Error("expected error without pid file")
	}
}

func TestPrepareUploadFolder(t *testing.T) {
	_, _, cfg, _ := testManager(t, nil)
	if err := os.MkdirAll(cfg.UploadFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeTestFile(filepath.Join(cfg.UploadFolder, "empty"), ""); err != nil {
		t.Fatal(err)
	}
	if err := writeTestFile(filepath.Join(cfg.UploadFolder, "tmpdroopy9"), "partial"); err != nil {
		t.Fatal(err)
	}
	deleted, err := DeleteEmptyFiles(cfg.UploadFolder)
	if err != nil || deleted != 1 {
		t.Errorf("delete empty = %d, %v", deleted, err)
	}
	removed, err := RemovePartialUploads(cfg.UploadFolder)
	if err != nil || removed != 1 {
		t.Errorf("remove partial = %d, %v", removed, err)
	}
}
