package log

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Init replaces slog's default logger, so these tests do not run in
// parallel and restore it afterwards.
func TestInit(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	tests := []struct {
		name      string
		debug     bool
		wantOld   bool
		wantDebug bool
	}{
		{name: "appends at info", debug: false, wantOld: true, wantDebug: false},
		{name: "debug truncates and logs debug", debug: true, wantOld: false, wantDebug: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "seiner.log")
			if err := os.WriteFile(path, []byte("old run\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			closeLog, err := Init(path, tt.debug)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = closeLog() })
			slog.Debug("debug line")
			slog.Info("info line")
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			got := string(b)
			if strings.Contains(got, "old run") != tt.wantOld {
				t.Errorf("old content kept=%v, want %v:\n%s", !tt.wantOld, tt.wantOld, got)
			}
			if strings.Contains(got, "debug line") != tt.wantDebug {
				t.Errorf("debug logged=%v, want %v:\n%s", !tt.wantDebug, tt.wantDebug, got)
			}
			if !strings.Contains(got, "info line") {
				t.Errorf("info line missing:\n%s", got)
			}
		})
	}
}

func TestInitRejectsEmptyPath(t *testing.T) {
	if _, err := Init("", false); err == nil {
		t.Fatal("Init with an empty path should fail")
	}
}

func TestInitFileIsPrivate(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	dir := filepath.Join(t.TempDir(), "state", "seiner")
	path := filepath.Join(dir, "seiner.log")
	closeLog, err := Init(path, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeLog() })
	for p, want := range map[string]os.FileMode{dir: 0o700, path: 0o600} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s mode %o, want %o", p, got, want)
		}
	}
}

// A symlink at the log path is refused, so --debug cannot truncate the
// file it points at, and logs go nowhere instead.
func TestInitRefusesSymlink(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	dir := t.TempDir()
	target := filepath.Join(dir, "precious")
	if err := os.WriteFile(target, []byte("keep me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "seiner.log")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(link, true); err == nil {
		t.Fatal("Init followed a symlink")
	}
	slog.Info("after failure")
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "keep me\n" {
		t.Fatalf("symlink target changed: %q", b)
	}
}
