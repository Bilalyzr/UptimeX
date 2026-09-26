// Package logging configures structured logging for all services.
// JSON output is used in production so logs can be ingested by log
// aggregators; text output keeps local development readable.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New returns a *slog.Logger for the given environment ("development" or
// "production"/anything else). Development uses a human-readable handler at
// debug level; production uses JSON at info level.
func New(env string) *slog.Logger {
	var handler slog.Handler
	opts := &slog.HandlerOptions{}
	out := io.Writer(os.Stdout)

	if env == "development" {
		opts.Level = slog.LevelDebug
		handler = slog.NewTextHandler(out, opts)
	} else {
		opts.Level = slog.LevelInfo
		handler = slog.NewJSONHandler(out, opts)
	}
	return slog.New(handler)
}

// NewForTest returns a logger that discards output, for use in tests.
func NewForTest() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// LevelFromString maps a user-provided level string to a slog level.
func LevelFromString(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
