package search

import (
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/tensleyconsulting/seiner/internal/ui/keys"
	"github.com/tensleyconsulting/seiner/internal/ui/styles"
)

// Model is a local filter prompt. It never triggers network I/O.
type Model struct {
	Query  string
	Active bool
	keys   keys.Map
}

func New(km keys.Map) Model {
	return Model{keys: km}
}

func (m *Model) Activate() {
	m.Active = true
}

func (m *Model) Deactivate(clear bool) {
	m.Active = false
	if clear {
		m.Query = ""
	}
}

func (m *Model) Handle(msg tea.KeyPressMsg) (consumed bool) {
	if !m.Active {
		return false
	}
	switch {
	case key.Matches(msg, m.keys.ForceQuit):
		return false
	case key.Matches(msg, m.keys.Back):
		m.Deactivate(true)
	case key.Matches(msg, m.keys.Select):
		m.Deactivate(false)
	case key.Matches(msg, m.keys.Erase):
		if m.Query != "" {
			_, size := utf8.DecodeLastRuneInString(m.Query)
			m.Query = m.Query[:len(m.Query)-size]
		}
	case key.Matches(msg, m.keys.ClearLine):
		m.Query = ""
	case msg.Code == tea.KeySpace:
		m.Query += " "
	default:
		m.Query += msg.Text
	}
	return true
}

// Paste appends pasted text to an active query, with line breaks and tabs
// as spaces since the query is one line. It reports whether it consumed
// the paste.
func (m *Model) Paste(s string) bool {
	if !m.Active {
		return false
	}
	m.Query += strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', '\t':
			return ' '
		}
		return r
	}, s)
	return true
}

// View renders the prompt on one row. A query wider than width shows its
// end, where typing happens, behind a leading "…".
func (m Model) View(width int) string {
	if !m.Active && m.Query == "" {
		return ""
	}
	label := "/" + m.Query
	if m.Active {
		label += "█"
	}
	if width > 1 && ansi.StringWidth(label) > width {
		label = "…" + ansi.TruncateLeft(label, ansi.StringWidth(label)-width+1, "")
	}
	return styles.Search().Width(width).Render(label)
}
