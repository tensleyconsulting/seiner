package splash

import (
	"slices"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSplitCellsWide(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		s    string
		w    int
		want []string
	}{
		{"ab", 3, []string{"a", "b", blank}},
		{"🪣", 2, []string{"🪣", wideTail}},
		{"a🪣b", 4, []string{"a", "🪣", wideTail, "b"}},
		{"🪣", 1, []string{blank}},
	} {
		if got := splitCells(tt.s, tt.w); !slices.Equal(got, tt.want) {
			t.Errorf("splitCells(%q, %d) = %q, want %q", tt.s, tt.w, got, tt.want)
		}
	}
}

// TestPutKeepsWideCellsWhole checks that clipping or drawing over half of a
// two-column character blanks the other half, so a row keeps its width.
func TestPutKeepsWideCellsWhole(t *testing.T) {
	t.Parallel()
	icon := splitCells("🪣", 2)
	for _, tt := range []struct {
		name string
		draw func(c canvas)
		want string
	}{
		{"whole", func(c canvas) { c.put(1, 0, icon, true) }, " 🪣 "},
		{"clipped left", func(c canvas) { c.put(-1, 0, icon, true) }, "    "},
		{"clipped right", func(c canvas) { c.put(3, 0, icon, true) }, "    "},
		{"head covered", func(c canvas) { c.put(1, 0, icon, true); c.put(1, 0, []string{"x"}, true) }, " x  "},
		{"tail covered", func(c canvas) { c.put(1, 0, icon, true); c.put(2, 0, []string{"x"}, true) }, "  x "},
	} {
		c := newCanvas(4, 1)
		tt.draw(c)
		got := c.row(0)
		if got != tt.want || ansi.StringWidth(got) != 4 {
			t.Errorf("%s: row %q (width %d), want %q", tt.name, got, ansi.StringWidth(got), tt.want)
		}
	}
}
