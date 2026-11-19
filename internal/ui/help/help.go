package help

import (
	bhelp "charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
	"github.com/tensleyconsulting/seiner/internal/ui/keys"
	"github.com/tensleyconsulting/seiner/internal/ui/styles"
)

// View renders a full-help overlay centered in the given space.
func View(width, height int, km keys.Map) string {
	h := bhelp.New()
	h.ShowAll = true
	h.SetWidth(max(20, width-8))
	body := styles.TitleStyle().Render("Seiner help") + "\n\n" + h.FullHelpView(km.FullHelp())
	box := styles.HelpBox().Render(body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
