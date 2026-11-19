package icons

import (
	"bytes"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// missingAssets lists service types allowed to fall back to the text icon.
var missingAssets = map[model.ServiceType]bool{}

func TestEveryServiceHasSVG(t *testing.T) {
	t.Parallel()
	for _, spec := range model.Specs() {
		if missingAssets[spec.Type] {
			continue
		}
		if _, err := fs.Stat(iconFS, "assets/"+string(spec.Type)+".svg"); err != nil {
			t.Errorf("%s: no assets/%s.svg", spec.Type, spec.Type)
		}
	}
}

func TestEveryServiceSVGRasterizes(t *testing.T) {
	t.Parallel()
	pngs := loadPNGs("")
	for _, spec := range model.Specs() {
		if missingAssets[spec.Type] {
			continue
		}
		if len(pngs[spec.Type]) == 0 {
			t.Errorf("%s: SVG did not rasterize", spec.Type)
		}
	}
}

func TestLogoRasterizes(t *testing.T) {
	t.Parallel()
	b := loadPNG(Logo, logoRasterSize)
	if len(b) == 0 {
		t.Fatal("logo.svg did not rasterize at splash size")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	// 4:3 like the banner and splash grids.
	if cfg.Width != logoRasterSize || cfg.Height != logoRasterSize*3/4 {
		t.Fatalf("logo raster %dx%d, want %dx%d", cfg.Width, cfg.Height, logoRasterSize, logoRasterSize*3/4)
	}
	if loadPNG("no-such-asset", rasterSize) != nil {
		t.Fatal("missing asset must return nil")
	}
}

var (
	rectTag = regexp.MustCompile(`<rect\b[^>]*>`)
	rxAttr  = regexp.MustCompile(`\srx="`)
	ryAttr  = regexp.MustCompile(`\sry="`)
)

// TestAssetConventions keeps the built-in set drawn one way: original
// artwork on a 24-unit grid with no exported styling or embedded images, and
// rounded rects that oksvg actually rounds (it ignores rx without ry).
func TestAssetConventions(t *testing.T) {
	t.Parallel()
	for _, name := range Names() {
		b, err := iconFS.ReadFile("assets/" + string(name) + ".svg")
		if err != nil {
			t.Fatal(err)
		}
		svg := string(b)
		box := `viewBox="0 0 24 24"`
		if name == Logo {
			box = `viewBox="0 0 32 24"`
		}
		if !strings.Contains(svg, box) {
			t.Errorf("%s: want %s", name, box)
		}
		for _, bad := range []string{"<style", "class=", "<image", "Generator", "xlink:href"} {
			if strings.Contains(svg, bad) {
				t.Errorf("%s: contains %q", name, bad)
			}
		}
		for _, tag := range rectTag.FindAllString(svg, -1) {
			if rxAttr.MatchString(tag) && !ryAttr.MatchString(tag) {
				t.Errorf("%s: %s has rx without ry", name, tag)
			}
		}
	}
}

func TestNamesCoverServicesAndLogo(t *testing.T) {
	t.Parallel()
	names := Names()
	if !slices.Contains(names, Logo) {
		t.Error("Names lacks Logo")
	}
	for _, spec := range model.Specs() {
		if !missingAssets[spec.Type] && !slices.Contains(names, spec.Type) {
			t.Errorf("Names lacks %s", spec.Type)
		}
	}
}

func TestCustomIconsDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	red := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><rect width="24" height="24" fill="#FF0000"/></svg>`)
	write(string(model.ServiceGKE)+".svg", red)
	write(string(Logo)+".png", encodePNG(t, 60, 30, color.RGBA{0, 200, 0, 255}))
	write(string(model.ServiceCloudRun)+".svg", []byte("not an svg"))
	write(string(model.ServiceCloudSQL)+".png", []byte("not a png"))
	write("no-such-service.svg", red)
	write(string(model.ServiceBigQuery)+".svg", red)
	if err := os.Truncate(filepath.Join(dir, string(model.ServiceBigQuery)+".svg"), maxIconFile+1); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, string(model.ServiceFirestore)+".svg"), 0o700); err != nil {
		t.Fatal(err)
	}

	got, builtin := loadPNGs(dir), loadPNGs("")
	if len(got) != len(builtin) {
		t.Fatalf("got %d icons, want %d", len(got), len(builtin))
	}
	if _, ok := got["no-such-service"]; ok {
		t.Error("a file with an unknown name became an icon")
	}
	if r, g, _ := centre(t, got[model.ServiceGKE]); r < 0xf000 || g != 0 {
		t.Errorf("gke.svg not used: centre r=%x g=%x", r, g)
	}
	if _, g, _ := centre(t, got[Logo]); g == 0 {
		t.Error("logo.png not used")
	}
	for _, svc := range []model.ServiceType{model.ServiceCloudRun, model.ServiceCloudSQL, model.ServiceBigQuery, model.ServiceFirestore, model.ServiceVPC} {
		if !bytes.Equal(got[svc], builtin[svc]) {
			t.Errorf("%s: want the built-in icon", svc)
		}
	}
	logo, fromDir := loadIcon(dir, Logo, logoRasterSize)
	if !fromDir || len(logo) == 0 {
		t.Error("large logo not loaded from dir")
	}
}

// centre decodes a PNG and returns the colour at its centre. The long side
// must be rasterSize; the logo keeps its own aspect ratio.
func centre(t *testing.T, b []byte) (r, g, bl uint32) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	dx, dy := img.Bounds().Dx(), img.Bounds().Dy()
	if max(dx, dy) != rasterSize {
		t.Fatalf("bounds %v, want a long side of %d", img.Bounds(), rasterSize)
	}
	r, g, bl, _ = img.At(dx/2, dy/2).RGBA()
	return r, g, bl
}
