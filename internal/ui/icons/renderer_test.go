package icons

import (
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/tensleyconsulting/seiner/internal/config"
	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/terminal"
)

func TestNewModes(t *testing.T) {
	t.Parallel()
	caps := terminal.Capabilities{KittyGraphics: true}
	if got := New(config.IconNone, "", caps); got.Name() != "none" {
		t.Errorf("none: %s", got.Name())
	}
	if got := New(config.IconText, "", caps); got.Name() != "text" {
		t.Errorf("text: %s", got.Name())
	}
	if got := New(config.IconAuto, "", terminal.Capabilities{}); got.Name() != "text" {
		t.Errorf("auto without kitty caps: %s", got.Name())
	}
	if got := New(config.IconAuto, "", caps); got.Name() != "kitty" {
		t.Errorf("auto with kitty caps: %s", got.Name())
	}
}

// TestNewColorEnv covers the renderer choice across color environments.
// Kitty image IDs ride in a truecolor foreground, so NO_COLOR picks text
// even for --icons kitty; a missing COLORTERM does not, because the program
// forces TrueColor for kitty (see app.Model.ProgramOptions).
func TestNewColorEnv(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		mode config.IconMode
		env  map[string]string
		want string
	}{
		{"auto ghostty truecolor", config.IconAuto, map[string]string{"TERM": "xterm-ghostty", "COLORTERM": "truecolor"}, "kitty"},
		{"auto ghostty no colorterm (ssh)", config.IconAuto, map[string]string{"TERM": "xterm-ghostty"}, "kitty"},
		{"auto herdr", config.IconAuto, map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "herdr", "COLORTERM": "truecolor"}, "kitty"},
		{"auto herdr no_color", config.IconAuto, map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "herdr", "NO_COLOR": "1"}, "text"},
		{"auto ghostty no_color", config.IconAuto, map[string]string{"TERM": "xterm-ghostty", "COLORTERM": "truecolor", "NO_COLOR": "1"}, "text"},
		{"auto xterm no_color", config.IconAuto, map[string]string{"TERM": "xterm-256color", "NO_COLOR": "1"}, "text"},
		{"auto xterm", config.IconAuto, map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"}, "text"},
		{"kitty forced xterm", config.IconKitty, map[string]string{"TERM": "xterm-256color"}, "kitty"},
		{"kitty forced tmux", config.IconKitty, map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/tmux"}, "kitty"},
		{"kitty forced no_color", config.IconKitty, map[string]string{"TERM": "xterm-ghostty", "NO_COLOR": "1"}, "text"},
		{"text no_color", config.IconText, map[string]string{"TERM": "xterm-ghostty", "NO_COLOR": "1"}, "text"},
		{"none no_color", config.IconNone, map[string]string{"TERM": "xterm-ghostty", "NO_COLOR": "1"}, "none"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			caps := terminal.Detect(func(k string) string { return tc.env[k] })
			r := New(tc.mode, "", caps)
			if r.Name() != tc.want {
				t.Fatalf("renderer=%s want %s", r.Name(), tc.want)
			}
			if Graphics(r) != (tc.want == "kitty") {
				t.Fatalf("Graphics=%v for %s", Graphics(r), r.Name())
			}
		})
	}
}

func TestTextRenderer(t *testing.T) {
	t.Parallel()
	r := TextRenderer{}
	if got := r.Render(model.ServiceCloudStorage); got != "🪣" {
		t.Errorf("got %q", got)
	}
	if got := r.Render(BucketFolder); got != "📂" {
		t.Errorf("bucket folder: %q", got)
	}
	if got := r.Render(GroupFolder); got != "📂" {
		t.Errorf("group folder: %q", got)
	}
	if got := r.Render(Logo); got != "GCP" {
		t.Errorf("logo: %q", got)
	}
	if got := r.RenderAt(model.ServiceCloudStorage, SizeHero, 1); got != "" {
		t.Errorf("text hero row 1: %q", got)
	}
	if r.Rows(SizeHero) != 1 || r.Cols(SizeList) != 2 || r.Cols(SizeBanner) != 4 {
		t.Errorf("text grid list=%dx%d hero rows=%d", r.Cols(SizeList), r.Rows(SizeList), r.Rows(SizeHero))
	}
	var noop NoopRenderer
	if noop.RenderAt(model.ServiceGKE, SizeList, 0) != "" {
		t.Error("noop should be empty")
	}
}

// TestTextGlyphsAreWide keeps text icons in a 2x1 cell: one rune that
// every width table measures as two columns. A symbol that needs U+FE0F to
// look like an emoji (🛡️) fails, since terminals disagree on its width.
func TestTextGlyphsAreWide(t *testing.T) {
	t.Parallel()
	r := TextRenderer{}
	names := []model.ServiceType{BucketFolder, GroupFolder}
	for _, spec := range model.Specs() {
		names = append(names, spec.Type)
	}
	for _, name := range names {
		g := r.Render(name)
		if utf8.RuneCountInString(g) != 1 || lipgloss.Width(g) != r.Cols(SizeList) {
			t.Errorf("%s: glyph %q is %d runes, %d cells; want one rune, %d cells",
				name, g, utf8.RuneCountInString(g), lipgloss.Width(g), r.Cols(SizeList))
		}
	}
}

func TestFor(t *testing.T) {
	t.Parallel()
	bucket := model.Resource{ID: "projects/p/buckets/b", Service: model.ServiceCloudStorage}
	repo := model.Resource{ID: "projects/p/locations/l/repositories/r", Service: model.ServiceArtifactRegistry}
	coll := model.Resource{ID: "organizations/1/collections/workspace-user", Service: model.ServiceWorkspaceUser}
	user := model.Resource{ID: "organizations/1/users/1", Name: "a@example.com", Service: model.ServiceWorkspaceUser}
	for _, tt := range []struct {
		name string
		r    model.Resource
		want model.ServiceType
	}{
		{"bucket", bucket, model.ServiceCloudStorage},
		{"bucket folder", model.PrefixResource(bucket, "a/", "a", ""), BucketFolder},
		{"other prefix", model.PrefixResource(repo, "a/", "a", ""), model.ServiceArtifactRegistry},
		{"project", model.Resource{Service: model.ServiceProject}, model.ServiceProject},
		{"user", user, model.ServiceWorkspaceUser},
		{"bucket node", model.Buckets(coll, "", []model.Resource{wsUserIn("/Eng", user), wsUserIn("/", user)})[0].Resource, GroupFolder},
	} {
		if got := For(tt.r); got != tt.want {
			t.Errorf("%s: For = %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestSizeGrid(t *testing.T) {
	t.Parallel()
	if SizeList.Cols() != 2 || SizeList.Rows() != 1 {
		t.Fatalf("list %dx%d", SizeList.Cols(), SizeList.Rows())
	}
	if SizeHero.Cols() != 4 || SizeHero.Rows() != 2 {
		t.Fatalf("hero %dx%d", SizeHero.Cols(), SizeHero.Rows())
	}
}

// wsUserIn is user in the org unit ou.
func wsUserIn(ou string, user model.Resource) model.Resource {
	user.Metadata = map[string]string{model.MetaOrgUnit: ou}
	return user
}
