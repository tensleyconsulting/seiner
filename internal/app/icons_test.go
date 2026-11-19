package app

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/tensleyconsulting/seiner/internal/terminal"
	"github.com/tensleyconsulting/seiner/internal/ui/icons"
)

// fakeIcons stands in for an icon renderer by name. As "kitty" it acts like
// KittyRenderer's upload bookkeeping and records calls.
type fakeIcons struct {
	icons.TextRenderer
	name    string
	sent    bool
	uploads int
	resets  int
	deletes int
}

func (f *fakeIcons) Name() string { return f.name }

func (f *fakeIcons) Upload() string {
	if f.name != "kitty" || f.sent {
		return ""
	}
	f.sent = true
	f.uploads++
	return "upload"
}

func (f *fakeIcons) Reset() {
	f.resets++
	f.sent = false
}

func (f *fakeIcons) Delete() string {
	if !f.sent {
		return ""
	}
	f.sent = false
	f.deletes++
	return "delete"
}

func withIcons(m Model, r icons.Renderer) Model {
	m.icons = r
	return m
}

func TestColorProfile(t *testing.T) {
	t.Parallel()
	cases := []struct {
		renderer string
		want     bool
	}{
		{"kitty", true},
		{"text", false},
		{"none", false},
	}
	for _, tc := range cases {
		t.Run(tc.renderer, func(t *testing.T) {
			t.Parallel()
			m := withIcons(newTestModel(), &fakeIcons{name: tc.renderer})
			p, ok := m.colorProfile()
			if ok != tc.want {
				t.Fatalf("forced=%v want %v", ok, tc.want)
			}
			if ok && p != colorprofile.TrueColor {
				t.Fatalf("profile=%v want TrueColor", p)
			}
			if got := len(m.ProgramOptions()); got != map[bool]int{true: 1, false: 0}[tc.want] {
				t.Fatalf("options=%d", got)
			}
		})
	}
}

// sequence returns the commands of a tea.Sequence message, whose type is
// unexported.
func sequence(t *testing.T, msg tea.Msg) []tea.Cmd {
	t.Helper()
	rv := reflect.ValueOf(msg)
	if rv.Kind() != reflect.Slice {
		t.Fatalf("not a sequence: %T", msg)
	}
	cmds := make([]tea.Cmd, rv.Len())
	for i := range cmds {
		cmds[i] = rv.Index(i).Interface().(tea.Cmd)
	}
	return cmds
}

func update(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestQuitDeletesKittyImages(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		renderer string
		upload   bool
		key      tea.KeyPressMsg
		del      bool
	}{
		{"q after upload", "kitty", true, keyText("q"), true},
		{"ctrl+c after upload", "kitty", true, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, true},
		{"q before upload", "kitty", false, keyText("q"), false},
		{"text renderer", "text", true, keyText("q"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeIcons{name: tc.renderer}
			m := sized(withIcons(newTestModel(), fake))
			if tc.upload {
				m = update(m, uploadIconsMsg{})
			}
			_, cmd := m.Update(tc.key)
			msg := cmd()
			if !tc.del {
				if _, ok := msg.(tea.QuitMsg); !ok {
					t.Fatalf("expected plain quit, got %T", msg)
				}
				return
			}
			cmds := sequence(t, msg)
			if len(cmds) != 2 {
				t.Fatalf("sequence of %d", len(cmds))
			}
			if raw, ok := cmds[0]().(tea.RawMsg); !ok || raw.Msg != "delete" {
				t.Fatalf("first: %#v", cmds[0]())
			}
			if _, ok := cmds[1]().(tea.QuitMsg); !ok {
				t.Fatal("second must quit")
			}
			if fake.deletes != 1 {
				t.Fatalf("deletes=%d", fake.deletes)
			}
		})
	}
}

func TestKittyResendTriggers(t *testing.T) {
	t.Parallel()
	resize := tea.WindowSizeMsg{Width: 120, Height: 30}
	cases := []struct {
		name     string
		renderer string
		mux      string
		trigger  tea.Msg
		resend   bool
	}{
		{"refresh", "kitty", "", keyText("r"), true},
		{"refresh under tmux", "kitty", terminal.MultiplexerTmux, keyText("r"), true},
		{"focus under tmux", "kitty", terminal.MultiplexerTmux, tea.FocusMsg{}, true},
		{"resize under tmux", "kitty", terminal.MultiplexerTmux, resize, true},
		{"focus direct", "kitty", "", tea.FocusMsg{}, false},
		{"resize direct", "kitty", "", resize, false},
		{"refresh text", "text", "", keyText("r"), false},
		{"focus text under tmux", "text", terminal.MultiplexerTmux, tea.FocusMsg{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeIcons{name: tc.renderer}
			m := sized(withIcons(newTestModel(), fake))
			m.caps.Multiplexer = tc.mux

			// Before the first upload, the scheduled one covers it.
			if m = update(m, tc.trigger); m.resendGen != 0 {
				t.Fatal("resend scheduled before the first upload")
			}
			m = update(m, uploadIconsMsg{})
			uploads := fake.uploads

			m = update(m, tc.trigger)
			if (m.resendGen != 0) != tc.resend {
				t.Fatalf("resend scheduled=%v want %v", m.resendGen != 0, tc.resend)
			}
			if !tc.resend {
				return
			}
			next, cmd := m.Update(uploadIconsMsg{gen: m.resendGen})
			m = next.(Model)
			if fake.resets != 1 || fake.uploads != uploads+1 {
				t.Fatalf("resets=%d uploads=%d", fake.resets, fake.uploads)
			}
			if raw, ok := cmd().(tea.RawMsg); !ok || raw.Msg != "upload" {
				t.Fatalf("resend cmd: %#v", cmd())
			}
			// A late initial upload message does not upload again.
			if update(m, uploadIconsMsg{}); fake.uploads != uploads+1 {
				t.Fatalf("uploads=%d after duplicate initial", fake.uploads)
			}
		})
	}
}

// TestKittyResendDebounced checks a burst of triggers uploads once, after
// the last.
func TestKittyResendDebounced(t *testing.T) {
	t.Parallel()
	fake := &fakeIcons{name: "kitty"}
	m := sized(withIcons(newTestModel(), fake))
	m.caps.Multiplexer = terminal.MultiplexerTmux
	m = update(m, uploadIconsMsg{})
	for w := 100; w < 105; w++ {
		m = update(m, tea.WindowSizeMsg{Width: w, Height: 30})
	}
	for gen := uint64(1); gen < m.resendGen; gen++ {
		m = update(m, uploadIconsMsg{gen: gen})
	}
	if fake.resets != 0 || fake.uploads != 1 {
		t.Fatalf("stale resends ran: resets=%d uploads=%d", fake.resets, fake.uploads)
	}
	m = update(m, uploadIconsMsg{gen: m.resendGen})
	if fake.resets != 1 || fake.uploads != 2 {
		t.Fatalf("latest resend: resets=%d uploads=%d", fake.resets, fake.uploads)
	}
}

func TestReportFocus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		renderer string
		mux      string
		want     bool
	}{
		{"kitty", terminal.MultiplexerTmux, true},
		{"kitty", "", false},
		{"text", terminal.MultiplexerTmux, false},
		{"none", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.renderer+"/"+tc.mux, func(t *testing.T) {
			t.Parallel()
			m := withIcons(newTestModel(), &fakeIcons{name: tc.renderer})
			m.caps.Multiplexer = tc.mux
			if got := m.View().ReportFocus; got != tc.want {
				t.Fatalf("focus reporting=%v want %v", got, tc.want)
			}
		})
	}
}
