package messages

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var at = time.Date(2026, 10, 19, 14, 3, 7, 0, time.UTC)

func TestAddCollapsesRepeats(t *testing.T) {
	t.Parallel()
	m := New()
	m.Add(Error, "denied", at)
	m.Add(Error, "denied", at.Add(time.Second))
	m.Add(Info, "denied", at)
	got := m.Entries()
	if len(got) != 2 || got[0].Count != 2 || got[1].Count != 1 {
		t.Fatalf("entries %+v", got)
	}
	m.SetSize(60, 5)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "14:03:08 ! denied ×2") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestViewFitsPane(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		w, h, n int
	}{
		{"empty", 40, 5, 0},
		{"few", 40, 5, 2},
		{"overflow", 40, 5, 20},
		{"narrow", 12, 3, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := New()
			for i := range tc.n {
				m.Add(Warn, fmt.Sprintf("message %d with enough words to wrap in a narrow pane", i), at)
			}
			m.SetSize(tc.w, tc.h)
			v := m.View()
			if h := lipgloss.Height(v); h != tc.h {
				t.Fatalf("height %d want %d:\n%s", h, tc.h, v)
			}
			if w := lipgloss.Width(v); w != tc.w {
				t.Fatalf("width %d want %d:\n%s", w, tc.w, v)
			}
		})
	}
}

func TestTitleInBorder(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSize(40, 5)
	if top := strings.Split(ansi.Strip(m.View()), "\n")[0]; !strings.HasPrefix(top, "╭─ Messages ─") {
		t.Fatalf("top %q", top)
	}
	m.Add(Info, "loaded", at)
	if top := strings.Split(ansi.Strip(m.View()), "\n")[0]; !strings.HasPrefix(top, "╭─ Messages (1) ─") {
		t.Fatalf("top %q", top)
	}
}

func TestScrollFollowsAndHolds(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetSize(60, 5) // three lines
	for i := range 6 {
		m.Add(Info, fmt.Sprintf("m%d", i), at)
	}
	visible := func() string { return ansi.Strip(m.View()) }
	if v := visible(); !strings.Contains(v, "m5") || strings.Contains(v, "m2") {
		t.Fatalf("should follow the newest:\n%s", v)
	}
	m.Scroll(-2)
	if v := visible(); !strings.Contains(v, "m1") || strings.Contains(v, "m4") {
		t.Fatalf("scrolled up 2:\n%s", v)
	}
	m.Add(Info, "m6", at)
	if v := visible(); !strings.Contains(v, "m1") || strings.Contains(v, "m6") {
		t.Fatalf("a new message should not move a scrolled view:\n%s", v)
	}
	m.GoTop()
	if v := visible(); !strings.Contains(v, "m0") {
		t.Fatalf("top:\n%s", v)
	}
	m.Scroll(-10)
	if v := visible(); !strings.Contains(v, "m0") {
		t.Fatalf("scroll past top should clamp:\n%s", v)
	}
	m.GoBottom()
	if v := visible(); !strings.Contains(v, "m6") {
		t.Fatalf("bottom:\n%s", v)
	}
}
