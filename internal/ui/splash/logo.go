package splash

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/tensleyconsulting/seiner/internal/ui/icons"
	"github.com/tensleyconsulting/seiner/internal/ui/styles"
)

// logoPixels is the text-mode logo, logo.svg redrawn on a pixel grid of
// two pixels to a cell: c cloud, w boat, l wave, d window, . clear. Clear
// cells stay transparent, so the scene behind the logo shows through them.
var logoPixels = []string{
	".................cc.................",
	"..............ccccccc...............",
	".............ccccccccc..............",
	"............ccccccccccc..ccccc......",
	"......cccc..ccccccwwwcccccccccc.....",
	"....ccccccccccccccwwwccccccccccc....",
	"....ccccccccccccccwwwccccccccccc....",
	"...cccccccccccccccwwwccccccccccc....",
	"...cccccwwwwcwwwwwwwwwwwwwcccccc....",
	".ccccccwwccwccwwwwwwwwwwwcccccccccc.",
	"ccccccwcwccwccwddwwwddddwccccccccccc",
	"ccccccwcwccwccwddwwwddddwccccwwccccc",
	"cccccwccwccwccwddwwwwwwwwccwwwwccccc",
	"ccccwccwwwwwwwwwwwwwwwwwwwwwwwwwcccc",
	"ccccwcccwwwwwwwwwwwwwwwwwwwwwwwccccc",
	"cccwcccccwwwwwwwwwwwwwwwwwwwwwccccc.",
	".ccccccccccwwwwwwwwwwwwwwwwwcccccc..",
	"....cccccccccccccccccccccccccccc....",
	"....cccllllccllllccllllccllllccc....",
	".....ccccc.cccccccccccccc.ccccc.....",
	"............ccccc..ccccc............",
	"....................................",
}

// logoCells renders the Seiner logo as canvas rows. Kitty draws the
// real artwork at icons.SizeSplash; anything else gets logoPixels as half
// blocks in the logo's colors.
func logoCells(r icons.Renderer) [][]string {
	if icons.Graphics(r) {
		w, h := r.Cols(icons.SizeSplash), r.Rows(icons.SizeSplash)
		rows := make([][]string, h)
		for y := range rows {
			rows[y] = splitCells(r.RenderAt(icons.Logo, icons.SizeSplash, y), w)
		}
		return rows
	}
	w := len(logoPixels[0])
	rows := make([][]string, (len(logoPixels)+1)/2)
	for y := range rows {
		top := logoPixels[2*y]
		bottom := strings.Repeat(".", w)
		if 2*y+1 < len(logoPixels) {
			bottom = logoPixels[2*y+1]
		}
		row := make([]string, w)
		for x := range row {
			row[x] = halfBlock(logoHue(top[x]), logoHue(bottom[x]))
		}
		rows[y] = row
	}
	return rows
}

// halfBlock draws a cell whose upper and lower halves are top and bottom;
// nil is clear.
func halfBlock(top, bottom color.Color) string {
	switch {
	case top == nil && bottom == nil:
		return blank
	case top == nil:
		return lipgloss.NewStyle().Foreground(bottom).Render("▄")
	case bottom == nil:
		return lipgloss.NewStyle().Foreground(top).Render("▀")
	case top == bottom:
		return lipgloss.NewStyle().Foreground(top).Render("█")
	}
	return lipgloss.NewStyle().Foreground(top).Background(bottom).Render("▀")
}

// logoHue is the color of a logoPixels pixel, or nil for clear.
func logoHue(p byte) color.Color {
	switch p {
	case 'c':
		return styles.LogoCloud()
	case 'w':
		return styles.LogoBoat()
	case 'l':
		return styles.LogoWave()
	case 'd':
		return styles.LogoWindow()
	}
	return nil
}
