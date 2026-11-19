package styles

import (
	"image/color"
	"sync/atomic"

	"charm.land/lipgloss/v2"
)

type palette struct {
	accent, selection, success, warning, err, muted, heading, border color.Color
	logoCloud, logoBoat, logoWave, logoWindow                        color.Color
}

var dark = palette{
	accent:     lipgloss.Color("#8AB4F8"),
	selection:  lipgloss.Color("#1A3A5C"),
	success:    lipgloss.Color("#81C995"),
	warning:    lipgloss.Color("#FDD663"),
	err:        lipgloss.Color("#F28B82"),
	muted:      lipgloss.Color("#9AA0A6"),
	heading:    lipgloss.Color("#E8EAED"),
	border:     lipgloss.Color("#3C4043"),
	logoCloud:  lipgloss.Color("#5B6CF6"),
	logoBoat:   lipgloss.Color("#FFFFFF"),
	logoWave:   lipgloss.Color("#B3BCFF"),
	logoWindow: lipgloss.Color("#3A47C9"),
}

// light keeps each role's hue but darkens it for contrast on a pale
// background: text near black, a pale selection, and an amber warning
// that stays readable where the dark palette's yellow would not. The
// logo's white boat becomes dark gray.
var light = palette{
	accent:     lipgloss.Color("#1A73E8"),
	selection:  lipgloss.Color("#D2E3FC"),
	success:    lipgloss.Color("#188038"),
	warning:    lipgloss.Color("#B06000"),
	err:        lipgloss.Color("#D93025"),
	muted:      lipgloss.Color("#5F6368"),
	heading:    lipgloss.Color("#202124"),
	border:     lipgloss.Color("#DADCE0"),
	logoCloud:  lipgloss.Color("#5B6CF6"),
	logoBoat:   lipgloss.Color("#3C4043"),
	logoWave:   lipgloss.Color("#7986CB"),
	logoWindow: lipgloss.Color("#3A47C9"),
}

var current atomic.Pointer[palette]

func init() {
	current.Store(&dark)
}

func active() *palette {
	return current.Load()
}

// SetDark picks the dark or light palette. The app calls it from its event
// loop when the terminal reports its background color; styles read the active
// palette atomically on each call, so the next frame uses the new one.
func SetDark(isDark bool) {
	if isDark {
		current.Store(&dark)
		return
	}
	current.Store(&light)
}

// Semantic colors. Components must use these instead of ad-hoc hex values.
func Accent() color.Color    { return active().accent }
func Selection() color.Color { return active().selection }
func Success() color.Color   { return active().success }
func Warning() color.Color   { return active().warning }
func Error() color.Color     { return active().err }
func Muted() color.Color     { return active().muted }
func Heading() color.Color   { return active().heading }
func Border() color.Color    { return active().border }

// Logo hues from icons/assets/logo.svg, used only for the text-mode logo
// in the startup intro.
func LogoCloud() color.Color  { return active().logoCloud }
func LogoBoat() color.Color   { return active().logoBoat }
func LogoWave() color.Color   { return active().logoWave }
func LogoWindow() color.Color { return active().logoWindow }

// HeaderApp is the app name and version in the header.
func HeaderApp() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(active().accent)
}

// Crumb is the selected node's breadcrumb in the header.
func Crumb() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(active().heading)
}

// Footer is the status bar.
func Footer() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(active().muted).
		Padding(0, 1)
}

// Banner is the auth-error bar under the header.
func Banner() lipgloss.Style {
	p := active()
	return lipgloss.NewStyle().
		Foreground(p.warning).
		Background(p.border).
		Bold(true).
		Padding(0, 1)
}

// Notice is the informational sibling of Banner: same bar, muted text.
func Notice() lipgloss.Style {
	p := active()
	return lipgloss.NewStyle().
		Foreground(p.muted).
		Background(p.border).
		Padding(0, 1)
}

// Pane is a bordered pane; the border is Accent when focused.
func Pane(focused bool) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(paneEdge(focused)).
		Padding(0, 1)
}

func paneEdge(focused bool) color.Color {
	p := active()
	if focused {
		return p.accent
	}
	return p.border
}

// PaneBorder colors border runes drawn by hand to match Pane.
func PaneBorder(focused bool) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(paneEdge(focused))
}

// PaneTitle is a title set into a pane's top border.
func PaneTitle(focused bool) lipgloss.Style {
	p := active()
	if focused {
		return lipgloss.NewStyle().Foreground(p.accent).Bold(true)
	}
	return lipgloss.NewStyle().Foreground(p.muted)
}

// TitleStyle is bold heading text.
func TitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(active().heading)
}

// MutedStyle is de-emphasized text.
func MutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(active().muted)
}

// SelectedRow highlights the selected row in the explorer and overlays.
func SelectedRow() lipgloss.Style {
	p := active()
	return lipgloss.NewStyle().Background(p.selection).Foreground(p.heading)
}

// Cursor marks the cursor position in lists.
func Cursor() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(active().accent).Bold(true)
}

// Choice styles one option of a mutually exclusive choice on a row.
func Choice(chosen bool) lipgloss.Style {
	p := active()
	if chosen {
		return lipgloss.NewStyle().Foreground(p.accent).Bold(true)
	}
	return lipgloss.NewStyle().Foreground(p.muted)
}

// Status colors a resource status word: Healthy, Warning, Error, or muted for
// anything else.
func Status(status string) lipgloss.Style {
	p := active()
	switch status {
	case "Healthy":
		return lipgloss.NewStyle().Foreground(p.success)
	case "Warning":
		return lipgloss.NewStyle().Foreground(p.warning)
	case "Error":
		return lipgloss.NewStyle().Foreground(p.err)
	default:
		return lipgloss.NewStyle().Foreground(p.muted)
	}
}

// Icon is the cell holding an icon or its text abbreviation; 4 columns wide
// unless the caller sets a width.
func Icon() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(active().accent).Bold(true).Width(4)
}

// Search is the search input text.
func Search() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(active().heading)
}

// HelpBox is the bordered box of the help and copy overlays.
func HelpBox() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(active().accent).
		Padding(1, 2)
}

// Fog is the drifting cloud cover the startup intro dissolves through.
func Fog() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(active().muted)
}

// Net is the startup intro's purse seine and the line to the boat's boom. A
// net that just closed is Accent.
func Net(fresh bool) lipgloss.Style {
	p := active()
	if fresh {
		return lipgloss.NewStyle().Foreground(p.accent).Bold(true)
	}
	return lipgloss.NewStyle().Foreground(p.muted)
}

// Float is a cork on the startup intro's net, the line along its top.
func Float() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(active().warning)
}

// Skiff is the small boat holding the far end of the startup intro's net.
func Skiff() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(active().logoCloud)
}

// Bubble is the trail a rising icon leaves in the startup intro.
func Bubble() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(active().logoWave)
}

// Wind is the air streaming off the startup intro's logo as it sails.
func Wind() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(active().muted)
}

// Seabed is the floor the startup intro's icons rise from.
func Seabed() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(active().border)
}

// Count styles a per-service resource count in the startup intro.
func Count(n int, failed bool) lipgloss.Style {
	p := active()
	switch {
	case failed:
		return lipgloss.NewStyle().Foreground(p.err).Bold(true)
	case n == 0:
		return lipgloss.NewStyle().Foreground(p.muted)
	default:
		return lipgloss.NewStyle().Foreground(p.success).Bold(true)
	}
}
