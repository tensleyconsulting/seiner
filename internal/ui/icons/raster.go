package icons

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	"golang.org/x/image/draw"
)

const (
	rasterSize     = 128
	logoRasterSize = 512 // the long side; a 24x9 splash grid is wide on a HiDPI font

	// maxPNGSide caps a custom PNG's width and height before decoding.
	maxPNGSide = 4096
)

// rasterSVG draws svg with its long side size pixels, keeping the viewBox's
// aspect ratio.
func rasterSVG(svg []byte, size int) ([]byte, error) {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(svg))
	if err != nil {
		return nil, fmt.Errorf("parse svg: %w", err)
	}
	// oksvg accepts text that is not SVG at all as an empty icon.
	if len(icon.SVGPaths) == 0 {
		return nil, errors.New("svg has nothing to draw")
	}
	if icon.ViewBox.W <= 0 || icon.ViewBox.H <= 0 {
		return nil, errors.New("svg has no viewBox or size")
	}
	w, h := fit(icon.ViewBox.W, icon.ViewBox.H, size)
	icon.SetTarget(0, 0, float64(w), float64(h))
	normalize(icon, size)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	scanner := rasterx.NewScannerGV(w, h, img, img.Bounds())
	icon.Draw(rasterx.NewDasher(w, h, scanner), 1)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// normalize scales strokes into the viewBox. oksvg strokes in output
// pixels, so a 2-unit stroke in a 24-unit viewBox would be 2px at any size
// where a browser draws it at a twelfth of the image.
func normalize(icon *oksvg.SvgIcon, size int) {
	if icon.ViewBox.W <= 0 || icon.ViewBox.H <= 0 {
		return
	}
	scale := float64(size) / max(icon.ViewBox.W, icon.ViewBox.H)
	for i := range icon.SVGPaths {
		p := &icon.SVGPaths[i]
		p.LineWidth *= scale
		p.DashOffset *= scale
		if len(p.Dash) > 0 {
			// Paths can share one dash slice; scale a copy.
			dash := make([]float64, len(p.Dash))
			for j, d := range p.Dash {
				dash[j] = d * scale
			}
			p.Dash = dash
		}
	}
}

// fit scales a w x h box so its long side is size, at least 1 x 1.
func fit(w, h float64, size int) (int, int) {
	if w >= h {
		return size, max(1, int(math.Round(float64(size)*h/w)))
	}
	return max(1, int(math.Round(float64(size)*w/h))), size
}

// scalePNG scales a PNG so its long side is size, keeping its aspect ratio.
// With square set it is centered in a transparent size x size square, so a
// custom PNG of any size uploads like built-in artwork.
func scalePNG(data []byte, size int, square bool) ([]byte, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse png: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, errors.New("empty png")
	}
	if cfg.Width > maxPNGSide || cfg.Height > maxPNGSide {
		return nil, fmt.Errorf("png is %dx%d, over %dx%d", cfg.Width, cfg.Height, maxPNGSide, maxPNGSide)
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse png: %w", err)
	}
	w, h := fit(float64(cfg.Width), float64(cfg.Height), size)
	cw, ch := w, h
	if square {
		cw, ch = size, size
	}
	x, y := (cw-w)/2, (ch-h)/2
	img := image.NewRGBA(image.Rect(0, 0, cw, ch))
	draw.CatmullRom.Scale(img, image.Rect(x, y, x+w, y+h), src, src.Bounds(), draw.Over, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
