package icons

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/tensleyconsulting/seiner/internal/model"
)

//go:embed assets/*.svg
var iconFS embed.FS

// maxIconFile caps a custom icon file. Icons are a few kilobytes; this keeps
// a stray large file from stalling startup.
const maxIconFile = 4 << 20

var (
	builtinPNGs = sync.OnceValue(func() map[model.ServiceType][]byte {
		names := Names()
		out := make(map[model.ServiceType][]byte, len(names))
		for _, name := range names {
			if png := rasterEmbedded(name, rasterSize); png != nil {
				out[name] = png
			}
		}
		return out
	})
	builtinSplashLogo = sync.OnceValue(func() []byte {
		return rasterEmbedded(Logo, logoRasterSize)
	})
)

// Names lists every icon by the name its file uses: each service type with
// built-in artwork, plus Logo. A custom icon directory overrides a name with
// <name>.svg or <name>.png.
func Names() []model.ServiceType {
	entries, err := fs.ReadDir(iconFS, "assets")
	if err != nil {
		return nil
	}
	var out []model.ServiceType
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".svg"); ok && !e.IsDir() {
			out = append(out, model.ServiceType(name))
		}
	}
	slices.Sort(out)
	return out
}

// loadPNGs rasterizes every icon at rasterSize. Files in dir replace the
// built-in artwork; dir may be "".
func loadPNGs(dir string) map[model.ServiceType][]byte {
	if dir == "" {
		return maps.Clone(builtinPNGs())
	}
	out := make(map[model.ServiceType][]byte)
	custom := 0
	for _, name := range Names() {
		png, fromDir := loadIcon(dir, name, rasterSize)
		if png == nil {
			continue
		}
		out[name] = png
		if fromDir {
			custom++
		}
	}
	slog.Info("custom icons loaded", "dir", dir, "count", custom, "of", len(out))
	return out
}

// loadIcon returns name as a PNG with its long side size: from dir/<name>.svg or
// dir/<name>.png when present and usable, else from the built-in artwork.
// fromDir reports which. A custom file that cannot be used is logged and
// skipped. It returns nil when neither source has the icon. Service icons are
// square; Logo keeps its own shape, which is wider than tall.
func loadIcon(dir string, name model.ServiceType, size int) (png []byte, fromDir bool) {
	if png := loadCustom(dir, name, size); png != nil {
		return png, true
	}
	return loadPNG(name, size), false
}

func loadCustom(dir string, name model.ServiceType, size int) []byte {
	if dir == "" {
		return nil
	}
	for _, ext := range []string{".svg", ".png"} {
		path := filepath.Join(dir, string(name)+ext)
		data, err := readIconFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err == nil {
			if ext == ".svg" {
				data, err = rasterSVG(data, size)
			} else {
				data, err = scalePNG(data, size, name != Logo)
			}
		}
		if err != nil {
			slog.Warn("custom icon unusable; using the built-in icon", "file", path, "err", err)
			return nil
		}
		return data
	}
	return nil
}

func readIconFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if info.Size() > maxIconFile {
		return nil, fmt.Errorf("%d bytes is over the %d byte limit", info.Size(), maxIconFile)
	}
	return os.ReadFile(path)
}

// loadPNG rasterizes one embedded asset at size, or returns nil.
func loadPNG(service model.ServiceType, size int) []byte {
	if size == rasterSize {
		if png, ok := builtinPNGs()[service]; ok {
			return png
		}
	}
	if service == Logo && size == logoRasterSize {
		return builtinSplashLogo()
	}
	return rasterEmbedded(service, size)
}

func rasterEmbedded(service model.ServiceType, size int) []byte {
	name := string(service) + ".svg"
	svg, err := iconFS.ReadFile("assets/" + name)
	if err != nil {
		slog.Warn("icon svg read failed", "file", name, "err", err)
		return nil
	}
	png, err := rasterSVG(svg, size)
	if err != nil {
		slog.Warn("icon svg raster failed", "file", name, "err", err)
		return nil
	}
	return png
}
