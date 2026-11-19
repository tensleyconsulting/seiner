package keys

import "charm.land/bubbles/v2/key"

// Map is the centralized key binding set.
type Map struct {
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	Top      key.Binding
	Bottom   key.Binding
	Select   key.Binding
	Back     key.Binding
	Search   key.Binding
	Refresh  key.Binding
	Copy     key.Binding
	Open     key.Binding
	Details  key.Binding
	Messages key.Binding
	View     key.Binding
	Help     key.Binding
	Quit     key.Binding
	// ForceQuit quits from anywhere, even while typing a search or with an
	// overlay open, where Quit is text or closes the overlay.
	ForceQuit key.Binding
	// Erase and ClearLine edit the search query.
	Erase     key.Binding
	ClearLine key.Binding
}

// Default returns the default key bindings.
func Default() Map {
	return Map{
		Up: key.NewBinding(
			key.WithKeys("k", "up"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("j", "down"),
			key.WithHelp("↓/j", "down"),
		),
		Left: key.NewBinding(
			key.WithKeys("h", "left"),
			key.WithHelp("←/h", "prev pane"),
		),
		Right: key.NewBinding(
			key.WithKeys("l", "right"),
			key.WithHelp("→/l", "next pane"),
		),
		Top: key.NewBinding(
			key.WithKeys("g"),
			key.WithHelp("g", "top"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("G"),
			key.WithHelp("G", "bottom"),
		),
		Select: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
		Copy: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "copy"),
		),
		Open: key.NewBinding(
			key.WithKeys("o"),
			key.WithHelp("o", "open console"),
		),
		Details: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "details"),
		),
		Messages: key.NewBinding(
			key.WithKeys("m"),
			key.WithHelp("m", "messages"),
		),
		View: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", "switch view"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "quit"),
		),
		ForceQuit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		Erase: key.NewBinding(
			key.WithKeys("backspace"),
			key.WithHelp("backspace", "erase"),
		),
		ClearLine: key.NewBinding(
			key.WithKeys("ctrl+u"),
			key.WithHelp("ctrl+u", "clear"),
		),
	}
}

func (m Map) ShortHelp() []key.Binding {
	return []key.Binding{m.Up, m.Down, m.Copy, m.Open, m.Help, m.Quit}
}

func (m Map) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{m.Up, m.Down, m.Top, m.Bottom},
		{m.Left, m.Right, m.Select, m.Back},
		{m.Search, m.Refresh, m.Copy, m.Open, m.Details, m.Messages, m.View},
		{m.Help, m.Quit},
	}
}
