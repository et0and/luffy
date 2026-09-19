package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestSetupLevels(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":    slog.LevelDebug,
		"info":     slog.LevelInfo,
		"warn":     slog.LevelWarn,
		"warning":  slog.LevelWarn,
		"error":    slog.LevelError,
		"":         slog.LevelInfo,
		"nonsense": slog.LevelInfo,
		"DEBUG":    slog.LevelDebug,
	}
	for input, want := range cases {
		var buf bytes.Buffer
		logger := Setup(&buf, input)
		logger.Debug("probe")
		if strings.Contains(buf.String(), "probe") != (want <= slog.LevelDebug) {
			t.Errorf("level %q: debug output mismatch", input)
		}
	}
}

func TestSetupWritesToWriter(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup(&buf, "info")
	logger.Info("hello", "key", "value")
	out := buf.String()
	if !strings.Contains(out, "hello") || !strings.Contains(out, "key=value") {
		t.Errorf("output = %q", out)
	}
	if slog.Default() != logger {
		t.Error("Setup should install the default logger")
	}
}
