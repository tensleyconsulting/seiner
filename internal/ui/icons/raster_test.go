package icons

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func TestLoadSVGsRasterize(t *testing.T) {
	t.Parallel()
	pngs := loadPNGs("")
	want := append([]model.ServiceType{Logo}, model.KnownServices()...)
	if len(pngs) < len(want) {
		t.Fatalf("got %d assets want at least %d", len(pngs), len(want))
	}
	for _, svc := range want {
		b := pngs[svc]
		if len(b) < 32 || !bytes.HasPrefix(b, []byte("\x89PNG")) {
			t.Errorf("%s: not a PNG (%d bytes)", svc, len(b))
		}
	}
}

func TestLogoHasTransparency(t *testing.T) {
	t.Parallel()
	pngs := loadPNGs("")
	b := pngs[Logo]
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, a := img.At(0, 0).RGBA()
	if a != 0 {
		t.Fatalf("corner alpha=%d, want transparent", a)
	}
}

// alphaAt rasterizes svg at size and returns the alpha at (x, y).
func alphaAt(t *testing.T, svg string, size, x, y int) uint32 {
	t.Helper()
	b, err := rasterSVG([]byte(svg), size)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, a := img.At(x, y).RGBA()
	return a
}

func TestRasterSVG(t *testing.T) {
	t.Parallel()
	const head = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">`
	cases := []struct {
		name   string
		body   string
		x, y   int
		opaque bool
	}{
		// At 48px a 4-unit stroke across y=12 covers rows 20 to 28.
		{"stroke scales with viewBox", `<path stroke="#000" stroke-width="4" d="M0 12H24"/>`, 24, 21, true},
		{"stroke stays inside its width", `<path stroke="#000" stroke-width="4" d="M0 12H24"/>`, 24, 17, false},
		// 6-unit dashes are 12px at 48px: x=9 is in the first dash.
		{"dashes scale with viewBox", `<path stroke="#000" stroke-width="4" stroke-dasharray="6 6" d="M0 12H24"/>`, 9, 24, true},
		// The rasterizer fills nonzero, so a hole must wind against its
		// outline; the built-in artwork relies on this.
		{"reverse-wound hole stays open", `<path d="M2 2H22V22H2Z M8 8V16H16V8Z"/>`, 24, 24, false},
		{"rect rx needs ry", `<rect width="24" height="24" rx="8" ry="8"/>`, 1, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := alphaAt(t, head+tc.body+`</svg>`, 48, tc.x, tc.y)
			if got := a > 0; got != tc.opaque {
				t.Fatalf("alpha at (%d,%d) = %d, want opaque %v", tc.x, tc.y, a, tc.opaque)
			}
		})
	}
}

func TestRasterSVGRejectsNonSVG(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"not an svg", `<svg xmlns="http://www.w3.org/2000/svg"></svg>`} {
		if _, err := rasterSVG([]byte(body), 32); err == nil {
			t.Errorf("%q: expected error", body)
		}
	}
}

func encodePNG(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{c}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestScalePNG(t *testing.T) {
	t.Parallel()
	green := color.RGBA{0, 200, 0, 255}
	b, err := scalePNG(encodePNG(t, 60, 30, green), 64, true)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != image.Rect(0, 0, 64, 64) {
		t.Fatalf("bounds %v", img.Bounds())
	}
	// 60x30 fits as 64x32, centred: rows 16 to 48.
	if _, g, _, a := img.At(32, 32).RGBA(); a == 0 || g == 0 {
		t.Error("centre is not the image")
	}
	if _, _, _, a := img.At(32, 4).RGBA(); a != 0 {
		t.Error("letterbox is not transparent")
	}
	b, err = scalePNG(encodePNG(t, 60, 30, green), 64, false)
	if err != nil {
		t.Fatal(err)
	}
	if img, err = png.Decode(bytes.NewReader(b)); err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != image.Rect(0, 0, 64, 32) {
		t.Fatalf("unsquared bounds %v", img.Bounds())
	}
	if _, err := scalePNG(encodePNG(t, maxPNGSide+1, 1, green), 64, true); err == nil {
		t.Error("oversized png: expected error")
	}
	if _, err := scalePNG([]byte("not a png"), 64, true); err == nil {
		t.Error("garbage: expected error")
	}
}
