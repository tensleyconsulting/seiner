// Package header draws the three-line band above the panes: the logo beside
// the app name and the account on the first line, the breadcrumb of the
// selected node on the second, and the selected resource's ID on the third.
package header

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tensleyconsulting/seiner/internal/ui/icons"
	"github.com/tensleyconsulting/seiner/internal/ui/styles"
)

// Rows is the header's height. The banner logo is three rows tall.
const Rows = 3

// idLabel leads the ID line.
const idLabel = "ID "

// Model is the header band. The app sets AppName once and Account, Crumb,
// and ID as they change.
type Model struct {
	AppName  string
	Account  string
	Crumb    string
	ID       string
	width    int
	renderer icons.Renderer
}

func New(renderer icons.Renderer, appName string) Model {
	return Model{renderer: renderer, AppName: appName}
}

func (m *Model) SetWidth(w int) { m.width = w }

func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}
	// One column of margin each side, matching the footer.
	inner := max(1, m.width-2)
	lead := m.logoWidth()
	if lead > 0 {
		lead++ // gap after the logo
	}
	avail := max(1, inner-lead)

	brand := styles.HeaderApp().Render(m.AppName)
	first := m.logo(0) + fitRight(brand, m.Account, avail)
	second := m.logo(1) + styles.Crumb().Render(truncateLeft(m.Crumb, avail))
	third := m.logo(2)
	if m.ID != "" {
		third += styles.MutedStyle().Render(idLabel) + truncateLeft(m.ID, max(1, avail-len(idLabel)))
	}

	line := lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Padding(0, 1)
	return line.Render(first) + "\n" + line.Render(second) + "\n" + line.Render(third)
}

// fitRight lays out left with right flush to the end of w columns. The
// account gives way first: it is dropped when both do not fit, then left is
// truncated.
func fitRight(left, right string, w int) string {
	lw := lipgloss.Width(left)
	if right != "" {
		rw := lipgloss.Width(right)
		if gap := w - lw - rw; gap >= 2 {
			return left + strings.Repeat(" ", gap) + styles.MutedStyle().Render(right)
		}
	}
	if lw > w {
		return ansi.Truncate(left, w, "…")
	}
	return left
}

// truncateLeft keeps the end of s, the selected node, when s is wider than
// w: "…prod-apps / demo-project". An ID keeps its last, most specific
// segments the same way.
func truncateLeft(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > w {
		r = r[1:]
	}
	return "…" + string(r)
}

func (m Model) logoWidth() int {
	if m.renderer == nil {
		return 0
	}
	return m.renderer.Cols(icons.SizeBanner)
}

// logo is row of the banner logo, or blanks of the same width where the
// renderer draws nothing on that row (text mode is one row tall).
func (m Model) logo(row int) string {
	w := m.logoWidth()
	if w == 0 {
		return ""
	}
	s := m.renderer.RenderAt(icons.Logo, icons.SizeBanner, row)
	switch {
	case s == "":
		s = strings.Repeat(" ", w)
	case !icons.Graphics(m.renderer):
		// Graphics placeholders must not be restyled.
		s = styles.Icon().Width(w).Render(s)
	}
	return s + " "
}
