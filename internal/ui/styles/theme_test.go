package styles

import (
	"image/color"
	"math"
	"sync"
	"testing"
)

// SetDark changes package state, so these tests do not run in parallel.
func TestSetDark(t *testing.T) {
	t.Cleanup(func() { SetDark(true) })
	SetDark(false)
	if Heading() != light.heading || Selection() != light.selection || LogoBoat() != light.logoBoat {
		t.Fatal("SetDark(false) did not pick the light palette")
	}
	SetDark(true)
	if Heading() != dark.heading || Selection() != dark.selection || LogoBoat() != dark.logoBoat {
		t.Fatal("SetDark(true) did not pick the dark palette")
	}
}

func TestSetDarkConcurrent(t *testing.T) {
	t.Cleanup(func() { SetDark(true) })
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 100 {
				SetDark((id+j)%2 == 0)
				_ = SelectedRow().Render("row")
				_ = Banner().Render("auth")
				_ = Muted()
			}
		}(i)
	}
	wg.Wait()
}

// Text colors meet WCAG AA contrast (4.5:1) on a typical background for
// their palette. Selection and Border are backgrounds and rules, not text.
func TestPaletteContrast(t *testing.T) {
	tests := []struct {
		name string
		p    palette
		bg   color.Color
	}{
		{"dark", dark, color.RGBA{0x20, 0x21, 0x24, 0xff}},
		{"light", light, color.RGBA{0xff, 0xff, 0xff, 0xff}},
	}
	for _, tt := range tests {
		text := map[string]color.Color{
			"accent": tt.p.accent, "success": tt.p.success, "warning": tt.p.warning,
			"error": tt.p.err, "muted": tt.p.muted, "heading": tt.p.heading,
		}
		for role, c := range text {
			if r := contrast(c, tt.bg); r < 4.5 {
				t.Errorf("%s %s contrast %.2f, want at least 4.5", tt.name, role, r)
			}
		}
		// Heading on the selected row must stay readable too.
		if r := contrast(tt.p.heading, tt.p.selection); r < 4.5 {
			t.Errorf("%s heading on selection contrast %.2f, want at least 4.5", tt.name, r)
		}
	}
}

func contrast(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	lin := func(v uint32) float64 {
		s := float64(v) / 0xffff
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}
