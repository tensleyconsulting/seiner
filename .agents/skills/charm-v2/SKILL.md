---
name: charm-v2
description: Charm Bubble Tea / Bubbles / Lip Gloss v2 conventions for Seiner. Use when writing or reviewing TUI code.
---

# Charm v2 in Seiner

Import paths:

```go
tea "charm.land/bubbletea/v2"
"charm.land/bubbles/v2/key"
"charm.land/bubbles/v2/help"
"charm.land/lipgloss/v2"
```

Do not use `github.com/charmbracelet/bubbletea` (v1) or unversioned Charm modules.

## Model

```go
func (m Model) Init() tea.Cmd
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m Model) View() tea.View
```

`View` returns `tea.View`, not `string`:

```go
func (m Model) View() tea.View {
    v := tea.NewView(content)
    v.AltScreen = true
    v.WindowTitle = "Seiner"
    return v
}
```

Do not call `tea.WithAltScreen()` or `tea.EnterAltScreen`.

## Keys

Handle `tea.KeyPressMsg`. Match with `key.Matches` against `internal/ui/keys`.

```go
case tea.KeyPressMsg:
    switch {
    case key.Matches(msg, keys.Quit):
        return m, tea.Quit
    }
```

`msg.String()` returns `"space"` for the space bar, `"ctrl+c"` for Ctrl+C.

Construct test events as `tea.KeyPressMsg{Text: "j", Code: 'j'}` or
`tea.KeyPressMsg{Code: tea.KeyEnter}`.

## Styling

All colors and reusable styles live in `internal/ui/styles`. Components accept
styles or call theme helpers; they do not invent hex colors.

Join panes with `lipgloss.JoinHorizontal` / `JoinVertical`.
Measure with `lipgloss.Width` / `Height`.

## Logging

Never `fmt.Println` to stdout from the TUI. Use `log/slog` after `internal/log.Init`.
