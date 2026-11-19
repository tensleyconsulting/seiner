package icons

import (
	"log/slog"

	"github.com/tensleyconsulting/seiner/internal/config"
	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/terminal"
)

// Logo is the Seiner mark shown in the header and the startup
// intro. It is not a resource service; the filename is assets/logo.svg.
const Logo model.ServiceType = "logo"

// Cursor marks the selected explorer row with graphics. It is not a
// resource service; the filename is assets/cursor.svg.
const Cursor model.ServiceType = "cursor"

// BucketFolder is a folder (object prefix) inside a Cloud Storage bucket.
// Prefix rows keep the bucket's service; For picks this icon for them.
const BucketFolder model.ServiceType = "cloud-storage-folder"

// GroupFolder is a bucket node of a long list (a Workspace org unit, or
// the users whose names start with one letter). Bucket rows keep their
// items' service; For picks this icon for them.
const GroupFolder model.ServiceType = "group-folder"

// For is the icon for r: its service, BucketFolder for a folder in a
// bucket, or GroupFolder for a bucket node.
func For(r model.Resource) model.ServiceType {
	if r.IsPrefix() && r.Service == model.ServiceCloudStorage {
		return BucketFolder
	}
	if r.IsBucket() {
		return GroupFolder
	}
	return r.Service
}

// Size selects a placeholder grid. Kitty unicode placement binds one
// (cols, rows) pair per image ID, so list and hero use distinct IDs of
// the same PNG.
type Size int

const (
	SizeList   Size = iota // 2x1, one list row tall
	SizeHero               // 4x2, details pane
	SizeBanner             // 8x3, header; Logo only
	SizeSplash             // 24x9, startup intro; Logo only
)

func (s Size) Cols() int {
	switch s {
	case SizeHero:
		return heroCols
	case SizeBanner:
		return bannerCols
	case SizeSplash:
		return splashCols
	}
	return listCols
}

func (s Size) Rows() int {
	switch s {
	case SizeHero:
		return heroRows
	case SizeBanner:
		return bannerRows
	case SizeSplash:
		return splashRows
	}
	return listRows
}

// Renderer turns a ServiceType into a visual prefix.
// Implementations must not be the only indication of service type.
type Renderer interface {
	RenderAt(service model.ServiceType, size Size, row int) string
	Name() string
	Cols(size Size) int
	Rows(size Size) int
	// Upload returns Kitty transmit sequences for all icons, or empty.
	// Call once at program start via tea.Raw so Lip Gloss cannot strip them.
	// It sends only images not yet sent since the last Reset or Delete.
	Upload() string
	// Reset forgets what Upload sent, so the next Upload sends it all
	// again (after the terminal may have dropped its images).
	Reset()
	// Delete returns sequences that free every uploaded image, or empty.
	// Call on quit via tea.Raw.
	Delete() string
}

// Graphics reports whether Render output contains terminal graphics and
// must not be recolored by Lip Gloss.
func Graphics(r Renderer) bool {
	return r != nil && r.Name() == "kitty"
}

// New selects a renderer. auto uses Kitty when the terminal claims support,
// which Detect never does inside a multiplexer. Forced kitty under tmux
// wraps uploads for tmux passthrough. dir, when set, holds custom icons that
// replace the built-in artwork (see loadIcon).
//
// Kitty placeholders carry the image ID in a truecolor foreground, so the
// program must run with a TrueColor profile when Graphics reports true (see
// app.Model.ProgramOptions). With NO_COLOR set the user asked for no color,
// so neither auto nor --icons kitty uses Kitty; a downsampled ID would point
// at the wrong image or none.
func New(mode config.IconMode, dir string, caps terminal.Capabilities) Renderer {
	switch mode {
	case config.IconNone:
		return NoopRenderer{}
	case config.IconText:
		return TextRenderer{}
	}
	if caps.NoColor && (mode == config.IconKitty || caps.KittyGraphics) {
		slog.Warn("text icons because NO_COLOR is set; kitty icons need truecolor image IDs", "icon_mode", mode)
		return TextRenderer{}
	}
	switch mode {
	case config.IconKitty:
		if caps.Multiplexer == terminal.MultiplexerScreen {
			slog.Warn("kitty icons forced under GNU screen, which does not pass them through; icons may not render")
		}
		return mustKitty(dir, caps.Multiplexer == terminal.MultiplexerTmux)
	default:
		if caps.KittyGraphics {
			return mustKitty(dir, false)
		}
		if caps.Multiplexer != "" {
			slog.Info("text icons inside a terminal multiplexer; use --icons kitty to force graphics", "multiplexer", caps.Multiplexer)
		}
		return TextRenderer{}
	}
}

func mustKitty(dir string, tmux bool) Renderer {
	pngs := loadPNGs(dir)
	if len(pngs) == 0 {
		slog.Warn("kitty renderer requested but no SVG assets were embedded")
		return TextRenderer{}
	}
	slog.Debug("kitty assets loaded", "count", len(pngs))
	k := newKitty(pngs)
	k.logo, _ = loadIcon(dir, Logo, logoRasterSize)
	k.tmux = tmux
	return k
}

// TextRenderer prints the service's glyph, an emoji two cells wide.
type TextRenderer struct{}

func (TextRenderer) Render(service model.ServiceType) string {
	switch service {
	case Logo:
		return "GCP"
	case BucketFolder, GroupFolder:
		return "📂"
	}
	return service.Glyph()
}

func (t TextRenderer) RenderAt(service model.ServiceType, size Size, row int) string {
	if row == 0 {
		return t.Render(service)
	}
	return ""
}

func (TextRenderer) Name() string { return "text" }

// Cols is one wide emoji, as wide as a Kitty list icon. The logo is the
// word "GCP" and gets a wider cell.
func (TextRenderer) Cols(size Size) int {
	if size == SizeBanner || size == SizeSplash {
		return 4
	}
	return 2
}

func (TextRenderer) Rows(Size) int { return 1 }

func (TextRenderer) Upload() string { return "" }

func (TextRenderer) Reset() {}

func (TextRenderer) Delete() string { return "" }

// NoopRenderer draws no icon.
type NoopRenderer struct{}

func (NoopRenderer) RenderAt(model.ServiceType, Size, int) string { return "" }

func (NoopRenderer) Name() string { return "none" }

func (NoopRenderer) Cols(Size) int { return 0 }

func (NoopRenderer) Rows(Size) int { return 1 }

func (NoopRenderer) Upload() string { return "" }

func (NoopRenderer) Reset() {}

func (NoopRenderer) Delete() string { return "" }

var (
	_ Renderer = TextRenderer{}
	_ Renderer = NoopRenderer{}
)
