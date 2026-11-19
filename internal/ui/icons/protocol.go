package icons

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

// Terminal cells are typically ~1:2 (width:height), so the list and hero
// grids are approximately square. List icons stay one row tall; hero icons
// (details pane) keep the larger 4x2 badge. The banner (header) and splash
// (startup intro) grids are the logo's, about 4:3 like logo.svg.
const (
	listCols   = 2
	listRows   = 1
	heroCols   = 4
	heroRows   = 2
	bannerCols = 8
	bannerRows = 3
	splashCols = 24
	splashRows = 9
)

// transmitPNG uploads png as image id with a cols x rows virtual placement.
// With tmux set, each chunk is wrapped in tmux's passthrough sequence (one
// per chunk, so no single sequence outgrows tmux's buffer); tmux forwards it
// to the outer terminal only with allow-passthrough on.
func transmitPNG(id int, png []byte, cols, rows int, tmux bool) string {
	b64 := base64.StdEncoding.EncodeToString(png)
	var b strings.Builder
	emit := func(seq string) {
		b.WriteString(passthrough(seq, tmux))
	}
	for i := 0; i < len(b64); i += kitty.MaxChunkSize {
		end := min(len(b64), i+kitty.MaxChunkSize)
		more := 1
		if end == len(b64) {
			more = 0
		}
		var opts []string
		if i == 0 {
			// a=t transmits only. a=T would also paint at the cursor and
			// leave full-size icons scattered on the alt screen.
			opts = []string{
				"a=t",
				"f=100",
				"t=d",
				fmt.Sprintf("i=%d", id),
				"q=2",
				fmt.Sprintf("m=%d", more),
			}
		} else {
			opts = []string{fmt.Sprintf("m=%d", more)}
		}
		emit(ansi.KittyGraphics([]byte(b64[i:end]), opts...))
	}
	emit(ansi.KittyGraphics(nil,
		"a=p",
		"U=1",
		fmt.Sprintf("i=%d", id),
		fmt.Sprintf("c=%d", cols),
		fmt.Sprintf("r=%d", rows),
		"q=2",
	))
	return b.String()
}

// deleteImage frees image id. d=I (upper case) removes the image's
// placements and its stored data; lower-case d=i would keep the data.
func deleteImage(id int, tmux bool) string {
	return passthrough(ansi.KittyGraphics(nil,
		"a=d",
		"d=I",
		fmt.Sprintf("i=%d", id),
		"q=2",
	), tmux)
}

// passthrough wraps seq for tmux, which forwards it to the outer terminal
// only with allow-passthrough on.
func passthrough(seq string, tmux bool) string {
	if tmux {
		return ansi.TmuxPassthrough(seq)
	}
	return seq
}

// idForeground encodes the Kitty image ID in the cell's truecolor foreground.
// Ghostty reads the RGB triple as a 24-bit ID. Using 256-color (38;5;n) fails
// because Bubble Tea's color profile rewrites it to an unrelated RGB value,
// and any profile below TrueColor would downsample this one too, so the
// program runs with a forced TrueColor profile while this renderer is active.
func idForeground(id int) string {
	r := (id >> 16) & 0xff
	g := (id >> 8) & 0xff
	b := id & 0xff
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}

func placeholderRow(id, row, cols int) string {
	var b strings.Builder
	b.WriteString(idForeground(id))
	for col := 0; col < cols; col++ {
		b.WriteRune(kitty.Placeholder)
		b.WriteRune(kitty.Diacritic(row))
		b.WriteRune(kitty.Diacritic(col))
	}
	b.WriteString("\x1b[39m")
	return b.String()
}
