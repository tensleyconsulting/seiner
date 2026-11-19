package search

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tensleyconsulting/seiner/internal/ui/keys"
)

func keyText(s string) tea.KeyPressMsg {
	r := []rune(s)
	return tea.KeyPressMsg{Text: s, Code: r[0]}
}

func TestHandleIgnoresKeysWhenInactive(t *testing.T) {
	t.Parallel()
	m := New(keys.Default())
	if consumed := m.Handle(keyText("a")); consumed {
		t.Fatal("inactive search consumed a key")
	}
	if m.Query != "" {
		t.Fatalf("query changed while inactive: %q", m.Query)
	}
}

func TestHandleTypingAndBackspace(t *testing.T) {
	t.Parallel()
	m := New(keys.Default())
	m.Activate()

	for _, r := range "abc" {
		if !m.Handle(keyText(string(r))) {
			t.Fatalf("typing %q was not consumed", r)
		}
	}
	if m.Query != "abc" {
		t.Fatalf("Query = %q, want abc", m.Query)
	}

	if !m.Handle(tea.KeyPressMsg{Code: tea.KeyBackspace}) {
		t.Fatal("backspace was not consumed")
	}
	if m.Query != "ab" {
		t.Fatalf("Query after backspace = %q, want ab", m.Query)
	}
}

func TestHandleBackspaceOnEmptyQueryIsNoop(t *testing.T) {
	t.Parallel()
	m := New(keys.Default())
	m.Activate()
	if !m.Handle(tea.KeyPressMsg{Code: tea.KeyBackspace}) {
		t.Fatal("backspace was not consumed")
	}
	if m.Query != "" {
		t.Fatalf("Query = %q, want empty", m.Query)
	}
}

func TestHandleCtrlU(t *testing.T) {
	t.Parallel()
	m := New(keys.Default())
	m.Activate()
	m.Query = "something"
	if !m.Handle(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}) {
		t.Fatal("ctrl+u was not consumed")
	}
	if m.Query != "" {
		t.Fatalf("Query = %q, want empty after ctrl+u", m.Query)
	}
	if !m.Active {
		t.Fatal("ctrl+u should not deactivate search")
	}
}

func TestHandleSpace(t *testing.T) {
	t.Parallel()
	m := New(keys.Default())
	m.Activate()
	m.Query = "a"
	if !m.Handle(tea.KeyPressMsg{Code: tea.KeySpace}) {
		t.Fatal("space was not consumed")
	}
	if m.Query != "a " {
		t.Fatalf("Query = %q, want %q", m.Query, "a ")
	}
}

func TestHandleEscClearsAndDeactivates(t *testing.T) {
	t.Parallel()
	m := New(keys.Default())
	m.Activate()
	m.Query = "abc"
	if !m.Handle(tea.KeyPressMsg{Code: tea.KeyEscape}) {
		t.Fatal("esc was not consumed")
	}
	if m.Active {
		t.Fatal("esc should deactivate search")
	}
	if m.Query != "" {
		t.Fatalf("esc should clear the query, got %q", m.Query)
	}
}

func TestHandleEnterKeepsQueryAndDeactivates(t *testing.T) {
	t.Parallel()
	m := New(keys.Default())
	m.Activate()
	m.Query = "abc"
	if !m.Handle(tea.KeyPressMsg{Code: tea.KeyEnter}) {
		t.Fatal("enter was not consumed")
	}
	if m.Active {
		t.Fatal("enter should deactivate search")
	}
	if m.Query != "abc" {
		t.Fatalf("enter should keep the query, got %q", m.Query)
	}
}

func TestHandleCtrlCIsNotConsumed(t *testing.T) {
	t.Parallel()
	m := New(keys.Default())
	m.Activate()
	if m.Handle(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}) {
		t.Fatal("ctrl+c should fall through to let the app quit")
	}
	if !m.Active {
		t.Fatal("ctrl+c should not itself change Active")
	}
}

func TestViewHiddenWhenInactiveAndEmpty(t *testing.T) {
	t.Parallel()
	m := New(keys.Default())
	if got := m.View(40); got != "" {
		t.Fatalf("View() = %q, want empty", got)
	}
}

func TestViewShowsQueryAndCursorWhenActive(t *testing.T) {
	t.Parallel()
	m := Model{Active: true, Query: "web"}
	got := m.View(40)
	if got == "" {
		t.Fatal("View() is empty while active")
	}
	if !containsAll(got, "/", "web", "█") {
		t.Fatalf("View() = %q, want it to contain /, web, and the cursor", got)
	}
}

func TestViewKeepsQueryAfterDeactivateWithoutClear(t *testing.T) {
	t.Parallel()
	m := Model{Active: false, Query: "web"}
	got := m.View(40)
	if !containsAll(got, "/", "web") {
		t.Fatalf("View() = %q, want it to retain the query", got)
	}
	if containsAll(got, "█") {
		t.Fatalf("View() = %q, should not show the cursor once inactive", got)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

func TestPaste(t *testing.T) {
	t.Parallel()
	m := New(keys.Default())
	if m.Paste("web") {
		t.Fatal("inactive search consumed a paste")
	}
	m.Activate()
	m.Query = "a"
	if !m.Paste("web-1\nprod\t") || m.Query != "aweb-1 prod " {
		t.Fatalf("Query = %q", m.Query)
	}
}

// A query wider than the pane stays on one row and shows its end.
func TestViewLongQueryIsOneRow(t *testing.T) {
	t.Parallel()
	m := New(keys.Default())
	m.Activate()
	m.Query = strings.Repeat("abcdefghij", 10) + "END"
	got := ansi.Strip(m.View(30))
	if strings.Contains(got, "\n") {
		t.Fatalf("View wrapped:\n%s", got)
	}
	if w := ansi.StringWidth(got); w != 30 {
		t.Fatalf("width %d, want 30: %q", w, got)
	}
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "END█") {
		t.Fatalf("View = %q, want …<end of query>█", got)
	}
}
