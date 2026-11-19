// Package messages is the scrollable log of status and error messages shown
// under the details pane.
package messages

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tensleyconsulting/seiner/internal/ui/styles"
)

// Level is a message's severity.
type Level int

const (
	Info Level = iota
	Warn
	Error
)

// maxEntries bounds the log; the oldest entries are dropped first.
const maxEntries = 500

// Entry is one logged message. Count is how many times it repeated in a row.
type Entry struct {
	Time  time.Time
	Level Level
	Text  string
	Count int
}

// Model is the messages pane.
type Model struct {
	entries []Entry
	width   int
	height  int
	focused bool
	// scroll is how many lines the view sits above the newest line; 0
	// follows new messages as they arrive.
	scroll int
	// wrapped is every entry's lines at wrapW columns, and spans how many
	// of them each entry takes. Add and SetSize keep them current, so View
	// wraps nothing.
	wrapped []string
	spans   []int
	wrapW   int
}

func New() Model { return Model{} }

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	if m.wrapW != m.innerWidth() {
		m.rewrap()
	}
	m.clampScroll()
}

func (m *Model) SetFocused(v bool) { m.focused = v }

func (m Model) Entries() []Entry { return m.entries }

// Add logs text at level. A repeat of the newest entry bumps its count
// instead of adding a line. A view scrolled back stays where it is.
func (m *Model) Add(level Level, text string, at time.Time) {
	w := m.innerWidth()
	if m.wrapW != w {
		m.rewrap()
	}
	if n := len(m.entries); n > 0 {
		last := &m.entries[n-1]
		if last.Level == level && last.Text == text {
			last.Count++
			last.Time = at
			lines := entryLines(*last, w)
			m.wrapped = append(m.wrapped[:len(m.wrapped)-m.spans[n-1]], lines...)
			m.spans[n-1] = len(lines)
			return
		}
	}
	e := Entry{Time: at, Level: level, Text: text, Count: 1}
	lines := entryLines(e, w)
	m.entries = append(m.entries, e)
	m.wrapped = append(m.wrapped, lines...)
	m.spans = append(m.spans, len(lines))
	if drop := len(m.entries) - maxEntries; drop > 0 {
		m.entries = m.entries[drop:]
		m.wrapped = m.wrapped[sum(m.spans[:drop]):]
		m.spans = m.spans[drop:]
	}
	if m.scroll > 0 {
		m.scroll += len(lines)
		m.clampScroll()
	}
}

// Scroll moves the view by delta lines; negative is toward older messages.
func (m *Model) Scroll(delta int) {
	m.scroll -= delta
	m.clampScroll()
}

// GoTop shows the oldest messages.
func (m *Model) GoTop() {
	m.scroll = len(m.lines())
	m.clampScroll()
}

// GoBottom shows the newest messages and follows new ones.
func (m *Model) GoBottom() { m.scroll = 0 }

func (m *Model) clampScroll() {
	limit := max(0, len(m.lines())-m.innerHeight())
	m.scroll = min(max(m.scroll, 0), limit)
}

func (m Model) innerWidth() int {
	return max(8, styles.PaneInnerWidth(m.width))
}

// innerHeight is the rows for messages: the pane less its two border rows.
func (m Model) innerHeight() int { return max(1, m.height-2) }

// lines is every entry's lines at the pane's width.
func (m Model) lines() []string {
	if m.wrapW == m.innerWidth() {
		return m.wrapped
	}
	var out []string
	for _, e := range m.entries {
		out = append(out, entryLines(e, m.innerWidth())...)
	}
	return out
}

// Restyle renders every entry again after the theme changed
// (styles.SetDark), since the kept lines carry the old colors.
func (m *Model) Restyle() { m.rewrap() }

// rewrap wraps every entry again at the pane's width.
func (m *Model) rewrap() {
	m.wrapW = m.innerWidth()
	m.wrapped = nil
	m.spans = make([]int, len(m.entries))
	for i, e := range m.entries {
		lines := entryLines(e, m.wrapW)
		m.wrapped = append(m.wrapped, lines...)
		m.spans[i] = len(lines)
	}
}

func sum(s []int) int {
	n := 0
	for _, v := range s {
		n += v
	}
	return n
}

// timeLayout stamps each entry; the date is left out to save columns.
const timeLayout = "15:04:05"

// entryLines renders e in w columns: time, marker, then the text wrapped
// under itself.
func entryLines(e Entry, w int) []string {
	prefix := styles.MutedStyle().Render(e.Time.Format(timeLayout)) + " " + marker(e.Level) + " "
	indent := strings.Repeat(" ", lipgloss.Width(prefix))
	text := e.Text
	if e.Count > 1 {
		text += styles.MutedStyle().Render(fmt.Sprintf(" ×%d", e.Count))
	}
	wrapped := strings.Split(ansi.Wrap(text, max(4, w-len(indent)), ""), "\n")
	out := make([]string, len(wrapped))
	for i, l := range wrapped {
		if i == 0 {
			out[i] = prefix + l
		} else {
			out[i] = indent + l
		}
	}
	return out
}

func marker(l Level) string {
	switch l {
	case Error:
		return styles.Status("Error").Render("!")
	case Warn:
		return styles.Status("Warning").Render("!")
	default:
		return styles.MutedStyle().Render("·")
	}
}

func (m Model) View() string {
	if m.width <= 0 || m.height < 3 {
		return ""
	}
	var body string
	if len(m.entries) == 0 {
		body = styles.MutedStyle().Render("No messages.")
	} else {
		lines := m.lines()
		end := len(lines) - m.scroll
		start := max(0, end-m.innerHeight())
		body = strings.Join(lines[start:end], "\n")
	}
	return styles.TitledPane(m.title(), m.focused, m.width, m.height, body)
}

// title is the pane's border title: its name, the entry count, and a
// mark while scrolled back from the newest message.
func (m Model) title() string {
	t := "Messages"
	if n := len(m.entries); n > 0 {
		t = fmt.Sprintf("Messages (%d)", n)
	}
	if m.scroll > 0 {
		t += " ↓"
	}
	return t
}
