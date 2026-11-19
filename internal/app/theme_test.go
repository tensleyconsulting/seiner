package app

import (
	"image/color"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tensleyconsulting/seiner/internal/ui/messages"
	"github.com/tensleyconsulting/seiner/internal/ui/styles"
)

// The theme follows the terminal's reported background, and kept message
// lines are rendered again in it. It changes package state in styles, so it
// does not run in parallel.
func TestBackgroundPicksTheme(t *testing.T) {
	t.Cleanup(func() { styles.SetDark(true) })
	m := sized(newTestModel())
	m.messages.SetSize(60, 10)
	m.messages.Add(messages.Info, "hello", time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC))
	darkMuted := styles.Muted()

	next, _ := m.Update(tea.BackgroundColorMsg{Color: color.RGBA{0xff, 0xff, 0xff, 0xff}})
	m = next.(Model)
	if styles.Muted() == darkMuted {
		t.Fatal("a white background kept the dark palette")
	}
	r, g, b, _ := styles.Muted().RGBA()
	want := ansi.Style{}.ForegroundColor(color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 0xff}).String()
	want = strings.TrimSuffix(strings.TrimPrefix(want, "\x1b["), "m")
	// The pane title is drawn fresh each frame; the kept line is the one
	// that must change.
	var line string
	for _, l := range strings.Split(m.messages.View(), "\n") {
		if strings.Contains(l, "hello") {
			line = l
		}
	}
	if !strings.Contains(line, want) {
		t.Fatalf("message line kept the old colors; want %q in %q", want, line)
	}

	next, _ = m.Update(tea.BackgroundColorMsg{Color: color.RGBA{0x10, 0x10, 0x10, 0xff}})
	m = next.(Model)
	if styles.Muted() != darkMuted {
		t.Fatal("a dark background did not restore the dark palette")
	}
	_ = m
}
