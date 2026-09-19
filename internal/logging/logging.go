// Package logging configures the process-wide structured logger.
package logging

import (
	"io"
	"log/slog"
	"strings"
)

// Setup installs a text logger writing to w and returns it. Level accepts
// debug, info, warn and error. An unknown level falls back to info.
func Setup(w io.Writer, level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: lvl}))
	slog.SetDefault(logger)
	return logger
}
