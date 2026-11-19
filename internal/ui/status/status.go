package status

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
	"github.com/tensleyconsulting/seiner/internal/ui/keys"
	"github.com/tensleyconsulting/seiner/internal/ui/styles"
	"github.com/tensleyconsulting/seiner/internal/ui/text"
)

// Model is the bottom bar.
type Model struct {
	Project string
	Account string
	Catalog string
	Icons   string
	Kitty   bool
	Flash   string
	width   int
	help    help.Model
	keys    keys.Map
}

func New(project, account, iconMode string, kitty bool, km keys.Map) Model {
	h := help.New()
	h.Styles.ShortKey = styles.MutedStyle()
	h.Styles.ShortDesc = styles.MutedStyle()
	h.Styles.ShortSeparator = styles.MutedStyle()
	h.ShowAll = false
	return Model{
		Project: project,
		Account: account,
		Catalog: "fixtures",
		Icons:   iconMode,
		Kitty:   kitty,
		help:    h,
		keys:    km,
	}
}

func (m *Model) SetWidth(w int) {
	m.width = w
	m.help.SetWidth(w)
}

func (m *Model) SetFlash(s string) { m.Flash = s }

func (m Model) View() string {
	kitty := "no"
	if m.Kitty {
		kitty = "yes"
	}
	left := fmt.Sprintf("project:%s  %s  %s  icons:%s  kitty:%s", m.Project, m.Account, m.Catalog, m.Icons, kitty)
	if m.Flash != "" {
		left += "  " + m.Flash
	}
	right := m.help.View(m.keys)
	// Content fits inside the footer's padding so the bar stays one row.
	style := styles.Footer()
	inner := m.width - style.GetHorizontalFrameSize()
	gap := inner - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return style.Width(m.width).Render(text.Truncate(left+"  "+right, inner))
	}
	return style.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
}
