// Package text holds small string-rendering helpers shared across UI
// components.
package text

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Truncate shortens s to display width w, appending "…" when it does not
// fit. w is a display column count (lipgloss.Width), not a rune count, so
// wide runes are handled correctly, and ANSI styles in s are kept whole.
func Truncate(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	return ansi.Truncate(s, w, "…")
}
