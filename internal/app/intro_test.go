package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tensleyconsulting/seiner/internal/config"
	"github.com/tensleyconsulting/seiner/internal/gcp"
	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/splash"
)

// newIntroModel builds an app with the intro on. It does not run Init, whose
// frame ticks would sleep; tests send splash.FrameMsg themselves.
func newIntroModel(mode config.IconMode, w, h int) Model {
	cfg := config.Config{Project: "demo-project", IconMode: mode, Intro: true}
	m := New(cfg, "test", gcp.FakeCatalog(), testIdent())
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(Model)
}

func frameAt(m Model, d time.Duration) Model {
	next, _ := m.Update(splash.FrameMsg{At: time.Now().Add(d)})
	return next.(Model)
}

func plainView(m Model) string {
	return ansi.Strip(m.View().Content)
}

// seatsAService reports whether view shows any service's text icon. The
// fixture catalog has more services than the intro has slots, and providers
// report in no fixed order, so which ones are seated varies from run to run.
func seatsAService(view string) bool {
	for _, svc := range model.KnownServices() {
		if strings.Contains(view, " "+svc.Glyph()+" ") {
			return true
		}
	}
	return false
}

func TestIntroPlaysWhileLoading(t *testing.T) {
	t.Parallel()
	m := newIntroModel(config.IconText, 100, 32)
	if !strings.Contains(plainView(m), "catching") {
		t.Fatalf("expected the intro first:\n%s", plainView(m))
	}
	m = drain(m, m.startup)
	var d time.Duration
	for ; d < 10*time.Second && !strings.Contains(plainView(m), "resources across"); d += 50 * time.Millisecond {
		m = frameAt(m, d)
	}
	if view := plainView(m); !strings.Contains(view, "resources across") || !seatsAService(view) {
		t.Fatalf("loaded services should be seated with a summary:\n%s", view)
	}
	if !strings.Contains(m.nav.View(), "Cloud Storage") {
		t.Fatal("the explorer must load underneath the intro")
	}
	for ; d < 20*time.Second && !m.intro.Done(); d += 50 * time.Millisecond {
		m = frameAt(m, d)
	}
	if !m.intro.Done() {
		t.Fatal("intro never handed off")
	}
	if strings.Contains(plainView(m), "catching") {
		t.Fatal("app view expected after the intro")
	}
}

func TestIntroSkip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		msg      tea.Msg
		wantQuit bool
	}{
		{"any key", keyText("?"), false},
		{"q skips rather than quits", keyText("q"), false},
		{"ctrl+c quits", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, true},
		{"click", tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newIntroModel(config.IconText, 100, 32)
			next, cmd := m.Update(tc.msg)
			m = next.(Model)
			if tc.wantQuit {
				if cmd == nil {
					t.Fatal("expected quit")
				}
				return
			}
			if cmd != nil {
				t.Fatal("skipping must not run the key's own action")
			}
			if !m.intro.Done() {
				t.Fatal("intro should be skipped")
			}
			if m.showHelp {
				t.Fatal("the skipping key must not also open help")
			}
		})
	}
}

func TestIntroOff(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		mode  config.IconMode
		intro bool
		w, h  int
	}{
		{"disabled", config.IconText, false, 100, 32},
		{"icons none", config.IconNone, true, 100, 32},
		{"small terminal", config.IconText, true, 60, 12},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Config{Project: "demo-project", IconMode: tc.mode, Intro: tc.intro}
			m := New(cfg, "test", gcp.FakeCatalog(), testIdent())
			next, _ := m.Update(tea.WindowSizeMsg{Width: tc.w, Height: tc.h})
			m = next.(Model)
			if !m.intro.Done() {
				t.Fatal("intro should not play")
			}
			if m.intro.Init() != nil {
				t.Fatal("no frames should be scheduled")
			}
		})
	}
}
