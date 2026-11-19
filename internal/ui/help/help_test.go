package help

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tensleyconsulting/seiner/internal/ui/keys"
)

func TestViewRendersTitleAndAllBindings(t *testing.T) {
	t.Parallel()
	km := keys.Default()
	out := ansi.Strip(View(80, 24, km))

	if !strings.Contains(out, "Seiner help") {
		t.Fatalf("missing title:\n%s", out)
	}
	for _, group := range km.FullHelp() {
		for _, b := range group {
			if desc := b.Help().Desc; desc != "" && !strings.Contains(out, desc) {
				t.Errorf("missing help text %q:\n%s", desc, out)
			}
		}
	}
}

func TestViewFitsRequestedSize(t *testing.T) {
	t.Parallel()
	out := View(80, 24, keys.Default())
	if got := ansi.StringWidth(strings.SplitN(out, "\n", 2)[0]); got != 80 {
		t.Fatalf("first line width = %d, want 80", got)
	}
	if got := strings.Count(out, "\n") + 1; got != 24 {
		t.Fatalf("height = %d, want 24", got)
	}
}
