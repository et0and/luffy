package timesave

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedSaver(t *testing.T, dir string) (*Saver, *string) {
	t.Helper()
	s := New(filepath.Join(dir, "timesave_file"), "+%C%g%m%d %H%M")
	s.Now = func() time.Time { return time.Date(2026, 9, 19, 20, 30, 0, 0, time.UTC) }
	var set string
	s.SetSystemTime = func(value string) error { set = value; return nil }
	return s, &set
}

func TestFormatTimeConfigFormat(t *testing.T) {
	got, err := FormatTime("+%C%g%m%d %H%M", time.Date(2026, 9, 19, 20, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got != "20260919 2030" {
		t.Errorf("FormatTime = %q, want %q", got, "20260919 2030")
	}
}

func TestFormatTimeDirectives(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got, err := FormatTime("%% %Y %y %j %S", at)
	if err != nil {
		t.Fatal(err)
	}
	if got != "% 2026 26 002 05" {
		t.Errorf("FormatTime = %q", got)
	}
	if _, err := FormatTime("%Q", at); err == nil {
		t.Error("unsupported directive should error")
	}
	if _, err := FormatTime("abc%", at); err == nil {
		t.Error("trailing %% should error")
	}
}

func TestSaveWritesCurrentTime(t *testing.T) {
	dir := t.TempDir()
	saver, _ := fixedSaver(t, dir)
	if err := saver.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(saver.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "20260919 2030" {
		t.Errorf("saved %q", data)
	}
}

func TestSaveRefusesToGoBackwards(t *testing.T) {
	dir := t.TempDir()
	saver, _ := fixedSaver(t, dir)
	if err := os.WriteFile(saver.Path, []byte("20261231 2359"), 0o666); err != nil {
		t.Fatal(err)
	}
	err := saver.Save()
	if err == nil || !strings.Contains(err.Error(), "lower than saved") {
		t.Errorf("save error = %v", err)
	}
	data, _ := os.ReadFile(saver.Path)
	if strings.TrimSpace(string(data)) != "20261231 2359" {
		t.Errorf("file should be unchanged, got %q", data)
	}
}

func TestSaveAcceptsForwardTime(t *testing.T) {
	dir := t.TempDir()
	saver, _ := fixedSaver(t, dir)
	if err := os.WriteFile(saver.Path, []byte("20260101 0001"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := saver.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, _ := os.ReadFile(saver.Path)
	if strings.TrimSpace(string(data)) != "20260919 2030" {
		t.Errorf("saved %q", data)
	}
}

func TestSaveIgnoresMalformedStoredValue(t *testing.T) {
	dir := t.TempDir()
	saver, _ := fixedSaver(t, dir)
	if err := os.WriteFile(saver.Path, []byte("garbage\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := saver.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
}

func TestRecoverSetsClockWhenAhead(t *testing.T) {
	dir := t.TempDir()
	saver, set := fixedSaver(t, dir)
	if err := os.WriteFile(saver.Path, []byte("20261001 1200"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := saver.Recover(); err != nil {
		t.Fatalf("recover: %v", err)
	}
	if *set != "20261001 1200" {
		t.Errorf("clock set to %q", *set)
	}
}

func TestRecoverRejectsPastTime(t *testing.T) {
	dir := t.TempDir()
	saver, set := fixedSaver(t, dir)
	if err := os.WriteFile(saver.Path, []byte("20260101 0001"), 0o666); err != nil {
		t.Fatal(err)
	}
	err := saver.Recover()
	if err == nil || !strings.Contains(err.Error(), "past") {
		t.Errorf("recover error = %v", err)
	}
	if *set != "" {
		t.Errorf("clock should not be set, got %q", *set)
	}
}

func TestRecoverMissingFile(t *testing.T) {
	dir := t.TempDir()
	saver, _ := fixedSaver(t, dir)
	if err := saver.Recover(); err == nil {
		t.Error("expected error for missing timesave file")
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"20260919 2030", "20260919 2029", 1},
		{"20260919 2030", "20260919 2031", -1},
		{"202609192030", "20260919 2030", 0},
	}
	for _, c := range cases {
		got, err := Compare(c.a, c.b)
		if err != nil {
			t.Fatalf("compare(%q,%q): %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Errorf("compare(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	if _, err := Compare("abc", "123"); err == nil {
		t.Error("expected compare error for non numeric input")
	}
}
