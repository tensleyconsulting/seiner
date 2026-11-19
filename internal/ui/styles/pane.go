package styles

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// TitledPane renders body in a Pane of width × height with title set into
// the top border ("╭─ Title ───╮"), so the title costs no row. The title
// is dropped when the pane is too narrow for it.
func TitledPane(title string, focused bool, width, height int, body string) string {
	if width <= 0 || height < 2 {
		return ""
	}
	h := height - 1
	pane := Pane(focused).BorderTop(false).Width(width).Height(h).MaxHeight(h).Render(body)
	return paneTop(title, focused, width) + "\n" + pane
}

// PaneInnerWidth is the columns inside a Pane of width.
func PaneInnerWidth(width int) int {
	return width - Pane(false).GetHorizontalFrameSize()
}

func paneTop(title string, focused bool, width int) string {
	b := lipgloss.RoundedBorder()
	edge := PaneBorder(focused)
	if title != "" {
		title = " " + title + " "
	}
	fill := width - 3 - lipgloss.Width(title)
	if fill < 0 {
		title = ""
		fill = max(0, width-3)
	} else if title != "" {
		title = PaneTitle(focused).Render(title)
	}
	return edge.Render(b.TopLeft+b.Top) + title + edge.Render(strings.Repeat(b.Top, fill)+b.TopRight)
}
