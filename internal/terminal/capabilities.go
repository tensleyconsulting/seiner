package terminal

import (
	"os"
	"strings"
)

// Terminal multiplexers that can sit between the app and the real terminal.
const (
	MultiplexerTmux   = "tmux"
	MultiplexerScreen = "screen"
)

// Capabilities is a conservative snapshot of terminal features.
// Detection must not assume Ghostty; SSH clients vary.
type Capabilities struct {
	Color         bool
	TrueColor     bool
	KittyGraphics bool
	Clipboard     bool
	// NoColor is set when the user asked for no color (NO_COLOR, see
	// https://no-color.org). Kitty icons carry their image ID in a
	// truecolor foreground, so they are not used then.
	NoColor bool
	// Multiplexer is MultiplexerTmux, MultiplexerScreen, or "" when the
	// app talks to the terminal directly.
	Multiplexer string
}

// Detect inspects the process environment. It does not query the terminal.
// Over SSH, TERM is often xterm-256color; use --icons kitty to force graphics.
func Detect(getenv func(string) string) Capabilities {
	if getenv == nil {
		getenv = os.Getenv
	}

	term := strings.ToLower(getenv("TERM"))
	colorterm := strings.ToLower(getenv("COLORTERM"))
	termProgram := strings.ToLower(getenv("TERM_PROGRAM"))

	caps := Capabilities{
		Color:     term != "" && term != "dumb",
		TrueColor: colorterm == "truecolor" || colorterm == "24bit",
		NoColor:   getenv("NO_COLOR") != "",
	}

	// Conservative graphics hints. Do not treat every xterm as Kitty.
	// Herdr parses Kitty graphics and Unicode placeholders in its pane
	// emulator and forwards them to the outer terminal, while setting
	// TERM=xterm-256color and stripping outer terminal variables.
	switch {
	case getenv("KITTY_WINDOW_ID") != "":
		caps.KittyGraphics = true
	case strings.Contains(term, "kitty"):
		caps.KittyGraphics = true
	case strings.Contains(term, "ghostty"):
		caps.KittyGraphics = true
	case termProgram == "ghostty":
		caps.KittyGraphics = true
	case getenv("GHOSTTY_RESOURCES_DIR") != "":
		caps.KittyGraphics = true
	case termProgram == "herdr":
		caps.KittyGraphics = true
	case getenv("HERDR_ENV") == "1":
		caps.KittyGraphics = true
	}

	// A TERM of tmux* or screen* also covers SSH from inside a local
	// multiplexer, where TMUX and STY are not forwarded.
	switch {
	case getenv("TMUX") != "" || termProgram == "tmux" || strings.HasPrefix(term, "tmux"):
		caps.Multiplexer = MultiplexerTmux
	case getenv("STY") != "" || strings.HasPrefix(term, "screen"):
		caps.Multiplexer = MultiplexerScreen
	}
	// The hints above are inherited from the outer terminal, but a
	// multiplexer drops Kitty graphics unless they are wrapped for it, which
	// auto mode does not attempt. --icons kitty still works under tmux.
	if caps.Multiplexer != "" {
		caps.KittyGraphics = false
	}

	if getenv("TERM_PROGRAM") == "iTerm.app" || getenv("WT_SESSION") != "" {
		caps.Clipboard = true
	}
	if caps.KittyGraphics {
		caps.Clipboard = true
	}
	return caps
}
