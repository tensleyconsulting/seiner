package status

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tensleyconsulting/seiner/internal/ui/keys"
)

func TestViewNarrowWidthTruncatesAndEndsWithFlash(t *testing.T) {
	t.Parallel()
	m := New("p", "a@b.com", "text", false, keys.Default())
	m.SetFlash("flashed!")

	left := "project:p  a@b.com  fixtures  icons:text  kitty:no  flashed!"
	m.SetWidth(len(left) + 1 + 2) // + 2 for the footer padding

	out := ansi.Strip(m.View())
	if !strings.Contains(out, "flashed!…") {
		t.Fatalf("narrow view should end with the flash followed by an ellipsis:\n%s", out)
	}
	if strings.Contains(out, "quit") {
		t.Fatalf("narrow view should drop the help text entirely:\n%s", out)
	}
}

func TestViewWideWidthRightAlignsHelp(t *testing.T) {
	t.Parallel()
	m := New("p", "a@b.com", "text", false, keys.Default())
	m.SetFlash("flashed!")
	m.SetWidth(200)

	out := ansi.Strip(m.View())
	if strings.Contains(out, "…") {
		t.Fatalf("wide view should not truncate:\n%s", out)
	}
	if !strings.Contains(out, "project:p") || !strings.Contains(out, "flashed!") {
		t.Fatalf("wide view should show the full left side:\n%s", out)
	}
	if !strings.Contains(out, "quit") {
		t.Fatalf("wide view should show the help text:\n%s", out)
	}
	left := strings.Index(out, "flashed!")
	right := strings.Index(out, "quit")
	if right <= left {
		t.Fatalf("help text should come after the left side:\n%s", out)
	}
	if !strings.Contains(out[left:right], strings.Repeat(" ", 5)) {
		t.Fatalf("expected a wide gap between the left side and the right-aligned help:\n%s", out)
	}
}

func TestViewIsOneLine(t *testing.T) {
	t.Parallel()
	for _, w := range []int{20, 61, 80, 120, 160, 200} {
		m := New("p", "a@b.com", "text", false, keys.Default())
		m.SetFlash("flashed!")
		m.SetWidth(w)
		out := m.View()
		if n := strings.Count(out, "\n"); n != 0 {
			t.Fatalf("width %d: footer is %d lines:\n%s", w, n+1, ansi.Strip(out))
		}
		if got := ansi.StringWidth(out); got != w {
			t.Fatalf("width %d: footer is %d columns", w, got)
		}
	}
}

func TestSetFlashUpdatesView(t *testing.T) {
	t.Parallel()
	m := New("p", "a@b.com", "text", false, keys.Default())
	m.SetWidth(200)
	if out := ansi.Strip(m.View()); strings.Contains(out, "saved") {
		t.Fatalf("flash should be empty by default:\n%s", out)
	}
	m.SetFlash("saved")
	if out := ansi.Strip(m.View()); !strings.Contains(out, "saved") {
		t.Fatalf("flash should appear after SetFlash:\n%s", out)
	}
}

// Truncating into the styled help must not cut an escape sequence in half.
func TestViewTruncatesStyledHelpWhole(t *testing.T) {
	t.Parallel()
	left := "project:p  a@b.com  fixtures  icons:text  kitty:no"
	for w := len(left) + 4; w < len(left)+40; w++ {
		m := New("p", "a@b.com", "text", false, keys.Default())
		m.SetWidth(w)
		out := m.View()
		if !strings.Contains(out, "\x1b[") {
			t.Fatalf("width %d: expected styled output", w)
		}
		plain := ansi.Strip(out)
		if strings.ContainsAny(plain, "\x1b") || strings.Contains(plain, "[3") {
			t.Fatalf("width %d: escape cut in half: %q", w, plain)
		}
		// Escapes take no columns, so the cut fills the bar to its padding.
		if got := ansi.StringWidth(strings.TrimRight(plain, " ")); got != w-1 {
			t.Fatalf("width %d: text ends at column %d, want %d: %q", w, got, w-1, plain)
		}
	}
}
