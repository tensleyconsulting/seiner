package header

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/tensleyconsulting/seiner/internal/config"
	"github.com/tensleyconsulting/seiner/internal/terminal"
	"github.com/tensleyconsulting/seiner/internal/ui/icons"
)

func view(r icons.Renderer, width int, crumb string) []string {
	return viewID(r, width, crumb, "projects/demo-project/buckets/my-app-bucket")
}

func viewID(r icons.Renderer, width int, crumb, id string) []string {
	m := New(r, "Seiner v0.1.0")
	m.Account = "dev@example.com"
	m.Crumb = crumb
	m.ID = id
	m.SetWidth(width)
	return strings.Split(ansi.Strip(m.View()), "\n")
}

func TestLayout(t *testing.T) {
	t.Parallel()
	for _, r := range []icons.Renderer{icons.TextRenderer{}, icons.NoopRenderer{}} {
		lines := view(r, 80, "example-org / prod / demo-project")
		if len(lines) != Rows {
			t.Fatalf("%s: %d lines", r.Name(), len(lines))
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w != 80 {
				t.Errorf("%s: line %d width %d", r.Name(), i, w)
			}
		}
		if !strings.Contains(lines[0], "Seiner v0.1.0") || strings.Contains(lines[0], "Google") {
			t.Errorf("%s: brand line %q", r.Name(), lines[0])
		}
		if !strings.HasSuffix(strings.TrimRight(lines[0], " "), "dev@example.com") {
			t.Errorf("%s: account should end line 1: %q", r.Name(), lines[0])
		}
		if a, b := strings.Index(lines[0], "Seiner"), strings.Index(lines[1], "example-org"); a != b {
			t.Errorf("%s: crumb at col %d, title at %d", r.Name(), b, a)
		}
		if a, b := strings.Index(lines[0], "Seiner"), strings.Index(lines[2], "ID projects/demo-project/buckets/my-app-bucket"); a != b {
			t.Errorf("%s: ID at col %d, title at %d: %q", r.Name(), b, a, lines[2])
		}
	}
}

func TestNarrow(t *testing.T) {
	t.Parallel()
	lines := view(icons.TextRenderer{}, 32, "example-org / platform / prod / prod-apps / demo-project")
	if strings.Contains(lines[0], "dev@example.com") {
		t.Errorf("account should give way: %q", lines[0])
	}
	if !strings.Contains(lines[1], "…") || !strings.HasSuffix(strings.TrimRight(lines[1], " "), "/ demo-project") {
		t.Errorf("crumb should keep its end: %q", lines[1])
	}
	if !strings.Contains(lines[2], "ID …") || !strings.HasSuffix(strings.TrimRight(lines[2], " "), "/my-app-bucket") {
		t.Errorf("ID should keep its end: %q", lines[2])
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 32 {
			t.Errorf("line %d width %d", i, w)
		}
	}
}

func TestNoID(t *testing.T) {
	t.Parallel()
	lines := viewID(icons.TextRenderer{}, 80, "example-org", "")
	if len(lines) != Rows || strings.TrimSpace(lines[2]) != "" {
		t.Errorf("no ID should leave the third line blank: %q", lines)
	}
	if w := lipgloss.Width(lines[2]); w != 80 {
		t.Errorf("third line width %d", w)
	}
}

func TestKittyLogoFillsEveryRow(t *testing.T) {
	t.Parallel()
	r := icons.New(config.IconKitty, "", terminal.Capabilities{KittyGraphics: true})
	if !icons.Graphics(r) {
		t.Skip("no embedded icon assets")
	}
	m := New(r, "Seiner v0.1.0")
	m.Crumb = "example-org"
	m.ID = "organizations/123"
	m.SetWidth(80)
	lines := strings.Split(m.View(), "\n")
	if len(lines) != Rows {
		t.Fatalf("%d lines", len(lines))
	}
	for i, l := range lines {
		if n := strings.Count(l, string(kitty.Placeholder)); n != icons.SizeBanner.Cols() {
			t.Errorf("line %d: %d logo cells, want %d", i, n, icons.SizeBanner.Cols())
		}
	}
}
