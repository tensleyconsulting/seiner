package log

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// Init sends slog to path and returns a close function for the underlying file.
// It never writes to stdout.
// When debug is true the file is truncated so one run is easy to inspect.
// The file and any directory Init creates are the user's alone, and a
// symlink at path is refused rather than followed. When path cannot be
// opened, Init discards logs and returns a no-op close function beside the
// error, so the caller can say so and carry on.
func Init(path string, debug bool) (func() error, error) {
	f, err := open(path, debug)
	if err != nil {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
		return noopClose, err
	}
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	handler := slog.NewTextHandler(f, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
	return func() error {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
		return f.Close()
	}, nil
}

func noopClose() error { return nil }

func open(path string, debug bool) (*os.File, error) {
	if path == "" {
		return nil, fmt.Errorf("log path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	flags := os.O_APPEND | os.O_CREATE | os.O_WRONLY | openNoFollow
	if debug {
		flags = os.O_TRUNC | os.O_CREATE | os.O_WRONLY | openNoFollow
	}
	return os.OpenFile(path, flags, 0o600)
}
