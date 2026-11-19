package splash

import (
	"math"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// blank is an empty canvas cell.
const blank = " "

// wideTail is the cell under the right half of a two-column character (a
// text-mode emoji icon). The cell to its left draws the character, and the
// tail draws nothing.
const wideTail = ""

// canvas is a grid of one-column cells. Each cell is a complete styled
// string, so Kitty placeholder cells can be moved and clipped one at a time:
// a placeholder's diacritics name its image row and column, not its position.
// A two-column character is a head cell followed by a wideTail.
type canvas struct {
	w, h  int
	cells [][]string
}

func newCanvas(w, h int) canvas {
	c := canvas{w: w, h: h, cells: make([][]string, h)}
	for y := range c.cells {
		row := make([]string, w)
		for x := range row {
			row[x] = blank
		}
		c.cells[y] = row
	}
	return c
}

// put writes cells starting at (x, y), clipping at the edges. When opaque is
// false, blank cells leave whatever is underneath.
func (c canvas) put(x, y int, cells []string, opaque bool) {
	if y < 0 || y >= c.h {
		return
	}
	for i, cell := range cells {
		cx := x + i
		if cx < 0 || cx >= c.w || (!opaque && cell == blank) {
			continue
		}
		c.cells[y][cx] = cell
	}
	c.mend(y)
}

// mend blanks the half of any two-column character that put split from its
// other half: by clipping it, or by drawing over one of its cells.
func (c canvas) mend(y int) {
	row := c.cells[y]
	for x, cell := range row {
		switch {
		case cell == wideTail && (x == 0 || ansi.StringWidth(row[x-1]) != 2):
			row[x] = blank
		case cell != blank && cell != wideTail && ansi.StringWidth(cell) == 2 && (x+1 == len(row) || row[x+1] != wideTail):
			row[x] = blank
		}
	}
}

// text writes a styled string centered on column cx.
func (c canvas) text(cx, y int, s string) {
	w := ansi.StringWidth(s)
	c.put(cx-w/2, y, splitCells(s, w), true)
}

func (c canvas) row(y int) string {
	return strings.Join(c.cells[y], "")
}

func (c canvas) String() string {
	rows := make([]string, c.h)
	for y := range rows {
		rows[y] = c.row(y)
	}
	return strings.Join(rows, "\n")
}

// splitCells cuts a styled string into w one-column cells, a two-column
// character into a head and a wideTail. ansi.Cut keeps the SGR state in
// effect, so each cell renders the same on its own.
func splitCells(s string, w int) []string {
	out := make([]string, w)
	for i := 0; i < w; i++ {
		cell := ansi.Cut(s, i, i+1)
		if ansi.StringWidth(cell) == 0 && i+1 < w {
			// ansi.Cut leaves out a character it would split.
			if wide := ansi.Cut(s, i, i+2); ansi.StringWidth(wide) == 2 {
				out[i], out[i+1] = wide, wideTail
				i++
				continue
			}
		}
		if ansi.StringWidth(cell) != 1 || strings.TrimSpace(ansi.Strip(cell)) == "" {
			cell = blank
		}
		out[i] = cell
	}
	return out
}

// valueNoise is smooth 2D noise in [0, 1].
func valueNoise(x, y float64) float64 {
	x0, y0 := math.Floor(x), math.Floor(y)
	tx, ty := smooth(x-x0), smooth(y-y0)
	i, j := int(x0), int(y0)
	top := lerp(lattice(i, j), lattice(i+1, j), tx)
	bot := lerp(lattice(i, j+1), lattice(i+1, j+1), tx)
	return lerp(top, bot, ty)
}

func lattice(i, j int) float64 {
	h := uint32(i)*0x9E3779B1 ^ uint32(j)*0x85EBCA77
	h ^= h >> 15
	h *= 0x2C1B3C6D
	h ^= h >> 12
	return float64(h&0xFFFF) / 0xFFFF
}

func smooth(t float64) float64 { return t * t * (3 - 2*t) }

func lerp(a, b, t float64) float64 { return a + (b-a)*t }
