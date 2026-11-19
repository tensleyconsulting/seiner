package terminal

import "testing"

func TestDetect(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		env   map[string]string
		kitty bool
		color bool
		mux   string
	}{
		{
			name:  "dumb",
			env:   map[string]string{"TERM": "dumb"},
			kitty: false,
			color: false,
		},
		{
			name:  "xterm",
			env:   map[string]string{"TERM": "xterm-256color"},
			kitty: false,
			color: true,
		},
		{
			name:  "kitty window",
			env:   map[string]string{"TERM": "xterm-256color", "KITTY_WINDOW_ID": "1"},
			kitty: true,
			color: true,
		},
		{
			name:  "ghostty term",
			env:   map[string]string{"TERM": "xterm-ghostty"},
			kitty: true,
			color: true,
		},
		{
			name:  "ghostty resources",
			env:   map[string]string{"TERM": "xterm-256color", "GHOSTTY_RESOURCES_DIR": "/usr/share/ghostty"},
			kitty: true,
			color: true,
		},
		{
			name:  "herdr term program",
			env:   map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "herdr"},
			kitty: true,
			color: true,
		},
		{
			name:  "herdr env",
			env:   map[string]string{"TERM": "xterm-256color", "HERDR_ENV": "1"},
			kitty: true,
			color: true,
		},
		{
			name:  "herdr env disabled",
			env:   map[string]string{"TERM": "xterm-256color", "HERDR_ENV": "0"},
			kitty: false,
			color: true,
		},
		{
			name:  "tmux inside herdr",
			env:   map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/tmux-1000/default,1,0", "TERM_PROGRAM": "tmux", "HERDR_ENV": "1"},
			kitty: false,
			color: true,
			mux:   MultiplexerTmux,
		},
		{
			name:  "tmux inside ghostty",
			env:   map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/tmux-1000/default,1,0", "TERM_PROGRAM": "tmux", "GHOSTTY_RESOURCES_DIR": "/usr/share/ghostty"},
			kitty: false,
			color: true,
			mux:   MultiplexerTmux,
		},
		{
			name:  "tmux with screen TERM inside kitty",
			env:   map[string]string{"TERM": "screen-256color", "TMUX": "/tmp/tmux-1000/default,1,0", "KITTY_WINDOW_ID": "1"},
			kitty: false,
			color: true,
			mux:   MultiplexerTmux,
		},
		{
			name:  "ssh from inside tmux",
			env:   map[string]string{"TERM": "tmux-256color"},
			kitty: false,
			color: true,
			mux:   MultiplexerTmux,
		},
		{
			name:  "screen session",
			env:   map[string]string{"TERM": "screen.xterm-256color", "STY": "1234.pts-0.host", "GHOSTTY_RESOURCES_DIR": "/usr/share/ghostty"},
			kitty: false,
			color: true,
			mux:   MultiplexerScreen,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			caps := Detect(func(k string) string { return tc.env[k] })
			if caps.KittyGraphics != tc.kitty {
				t.Errorf("kitty=%v want %v", caps.KittyGraphics, tc.kitty)
			}
			if caps.Color != tc.color {
				t.Errorf("color=%v want %v", caps.Color, tc.color)
			}
			if caps.Multiplexer != tc.mux {
				t.Errorf("multiplexer=%q want %q", caps.Multiplexer, tc.mux)
			}
		})
	}
}

func TestDetectColorEnv(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		env       map[string]string
		trueColor bool
		noColor   bool
	}{
		{"no hints", map[string]string{"TERM": "xterm-256color"}, false, false},
		{"colorterm truecolor", map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"}, true, false},
		{"colorterm 24bit", map[string]string{"TERM": "xterm-256color", "COLORTERM": "24bit"}, true, false},
		{"no_color", map[string]string{"TERM": "xterm-ghostty", "NO_COLOR": "1"}, false, true},
		{"no_color any value", map[string]string{"TERM": "xterm-ghostty", "NO_COLOR": "yes"}, false, true},
		{"no_color with colorterm", map[string]string{"TERM": "xterm-ghostty", "COLORTERM": "truecolor", "NO_COLOR": "1"}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			caps := Detect(func(k string) string { return tc.env[k] })
			if caps.TrueColor != tc.trueColor {
				t.Errorf("truecolor=%v want %v", caps.TrueColor, tc.trueColor)
			}
			if caps.NoColor != tc.noColor {
				t.Errorf("nocolor=%v want %v", caps.NoColor, tc.noColor)
			}
		})
	}
}
