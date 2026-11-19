package details

import (
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/icons"
	"github.com/tensleyconsulting/seiner/internal/ui/keys"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden")

// checkGolden compares got, styles and all, with testdata/name.golden.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("%s differs from %s:\ngot:\n%s\nwant:\n%s", name, path, got, want)
	}
}

// wrapInputs are values wrap must split the same way at every width.
var wrapInputs = []string{
	"",
	" ",
	"a: b",
	"sourceRanges: 10.0.0.0/8 192.168.0.0/16",
	strings.Repeat("x", 50),
	strings.Repeat("word ", 40),
	"projects/demo/global/networks/default",
	"10.0.0.0/8,192.168.0.0/16;172.16.0.0/12, 100.64.0.0/10",
	"serviceAccount:svc-123@demo-project.iam.gserviceaccount.com, user:alice@example.com",
	"a  b   c    d     trailing     ",
	"   leading spaces then words",
	"line one\nline two is a little longer\n\nline four",
	"日本語のテキストは幅が広い 文字 です、カンマ，と/スラッシュ",
	"mixed ascii 日本 and wide ドメイン/パス/区切り, done",
	"emoji 👍🏽 family 👨‍👩‍👧 flag 🇯🇵🇺🇸 keycap #️⃣ e\u0301 combining",
	"ar \u0600x prepend \u0600 then space",
	"tab\tseparated\tvalues and \x1b[31mred\x1b[0m text",
	"https://console.cloud.google.com/storage/browser/my-app-bucket;tab=objects?project=demo-project",
	strings.Repeat("a/", 30) + strings.Repeat("b,", 30) + strings.Repeat("c;", 30),
}

var wrapWidths = []int{1, 2, 3, 5, 8, 12, 20, 33, 60}

func TestWrapGolden(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i, in := range wrapInputs {
		for _, w := range wrapWidths {
			fmt.Fprintf(&b, "== input %d width %d\n%s\n", i, w, wrap(in, w))
		}
	}
	checkGolden(t, "wrap", b.String())
}

func TestViewGolden(t *testing.T) {
	t.Parallel()
	items := func(n int, f string) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf(f, i)
		}
		return out
	}
	long := model.Normalize(model.Resource{
		ID:          "projects/demo-project/zones/us-central1-a/instances/web-frontend-with-a-long-name",
		Name:        "web-frontend-with-a-long-name",
		Service:     model.ServiceComputeEngine,
		Location:    "us-central1-a",
		Status:      model.StatusHealthy,
		State:       "RUNNING",
		Description: "A frontend VM with a description long enough to wrap, with commas, semicolons; and/slashes/in/it.",
		Created:     time.Date(2025, 1, 15, 18, 4, 0, 0, time.UTC),
		Labels:      map[string]string{"team": "web", "env": "prod", "cost-center": "1234/5678,abc"},
		Fields: []model.Field{
			{Key: "machineType", Value: "projects/demo-project/zones/us-central1-a/machineTypes/e2-standard-4"},
			{Key: "networkTags", Value: "http-server, https-server, allow-health-checks, internal-only"},
			{Key: "a very long key that pushes the value down", Value: "value"},
			{Key: "wide", Value: "日本語の説明 テキスト、とても長い値です/パス"},
		},
		Lists: []model.List{
			{ID: "members", Label: "Members", Items: items(40, "serviceAccount:svc-%02d@demo-project.iam.gserviceaccount.com, with a tail")},
			{ID: "empty", Label: "Empty"},
			{ID: "ranges", Label: "Ranges", Items: items(30, "10.%d.0.0/16,192.168.0.0/24;172.16.0.0/12")},
		},
		Facets: []model.Facet{{ID: model.FacetObjects, Label: "Objects"}, {ID: "perm", Label: "Permissions"}},
	})
	blank := model.Normalize(model.Resource{
		ID: "b", Name: "b", Service: model.ServiceCloudStorage,
		Lists: []model.List{{ID: "m", Label: "Members", Items: append(items(6, "member-%d"), " ", "", " ", "", "", "", "", "", "", "", "", "", "tail")}},
	})
	blankTail := blank
	blankTail.Lists = []model.List{{ID: "m", Label: "Members", Items: append(items(6, "member-%d"), " ", "", " ", "", "", "")}}
	problems := []model.ProviderError{{Service: model.ServiceCloudStorage, Kind: model.ErrKindPermissionDenied, Message: "denied"}}
	denials := []model.Denial{{Service: model.ServiceComputeEngine, Missing: []string{"compute.instances.list"}}}

	cases := []struct {
		name          string
		r             model.Resource
		width, height int
		focused       bool
		extras        bool
	}{
		{"tall", long, 80, 200, true, true},
		{"clipped", long, 80, 30, false, false},
		{"clipped in list", long, 60, 40, true, true},
		{"clipped in second list", long, 60, 130, true, true},
		{"narrow", long, 12, 30, false, false},
		{"tiny height", long, 40, 4, false, false},
		{"one line budget", long, 40, 9, false, false},
		{"blank items at clip", blank, 40, 14, false, false},
		{"blank items at clip 2", blank, 40, 15, false, false},
		{"blank items at clip 3", blank, 40, 16, false, false},
		{"blank items at clip 4", blank, 40, 17, false, false},
		{"blank tail", blankTail, 40, 14, false, false},
		{"blank tail 2", blankTail, 40, 16, false, false},
		{"blank tail 3", blankTail, 40, 20, false, false},
	}
	var b strings.Builder
	for _, tc := range cases {
		m := New(icons.TextRenderer{}, keys.Default())
		m.SetSize(tc.width, tc.height)
		m.SetFocused(tc.focused)
		r := tc.r
		m.SetResource(&r)
		if tc.extras {
			m.SetProblems(problems)
			m.SetDenials(denials)
		}
		fmt.Fprintf(&b, "== %s\n%s\n", tc.name, m.View())
	}
	checkGolden(t, "view", b.String())
}

// wrapLineByRemeasure is wrapLine as first written, measuring the whole
// line again for each token. TestWrapLineMatchesRemeasure holds the
// running-width version to it.
func wrapLineByRemeasure(s string, w int) []string {
	var lines []string
	var cur strings.Builder
	flush := func() {
		lines = append(lines, strings.TrimRight(cur.String(), " "))
		cur.Reset()
	}
	for _, tok := range tokens(s, w) {
		tw := lipgloss.Width(strings.TrimRight(tok, " "))
		if cur.Len() > 0 && lipgloss.Width(cur.String())+tw > w {
			flush()
			tok = strings.TrimLeft(tok, " ")
		}
		cur.WriteString(tok)
	}
	flush()
	return lines
}

func TestWrapLineMatchesRemeasure(t *testing.T) {
	t.Parallel()
	// Pieces that join into one grapheme across a separator or a cut, or
	// carry escapes, as well as plain and wide text.
	pieces := []string{
		"a", "word", " ", "  ", ",", ";", "/", "日本", "語", "ド", "́", "‍",
		"👨", "👩", "🏽", "🇯", "🇵", "️", "#", "؀", "\t", "\x1b[31m", "\x1b[0m", "é",
	}
	rng := rand.New(rand.NewPCG(1, 2))
	inputs := slices.Clone(wrapInputs)
	for range 1000 {
		var b strings.Builder
		for range 1 + rng.IntN(40) {
			b.WriteString(pieces[rng.IntN(len(pieces))])
		}
		inputs = append(inputs, b.String())
	}
	for _, in := range inputs {
		for _, para := range strings.Split(in, "\n") {
			for _, w := range wrapWidths {
				want := wrapLineByRemeasure(para, w)
				if got := wrapLine(para, w, math.MaxInt); !slices.Equal(got, want) {
					t.Fatalf("wrapLine(%q, %d) = %q, want %q", para, w, got, want)
				}
				for limit := 1; limit < len(want); limit++ {
					if got := wrapLine(para, w, limit); !slices.Equal(got, want[:limit]) {
						t.Fatalf("wrapLine(%q, %d, %d) = %q, want %q", para, w, limit, got, want[:limit])
					}
				}
			}
		}
	}
}

func BenchmarkWrap(b *testing.B) {
	long := strings.Repeat("serviceAccount:svc@demo-project.iam.gserviceaccount.com, ", 40)
	for b.Loop() {
		wrap(long, 60)
	}
}

func BenchmarkView(b *testing.B) {
	items := make([]string, 2000)
	for i := range items {
		items[i] = fmt.Sprintf("serviceAccount:svc-%04d@demo-project.iam.gserviceaccount.com, with a tail", i)
	}
	r := model.Normalize(model.Resource{
		ID: "b", Name: "b", Service: model.ServiceCloudStorage,
		Lists: []model.List{{ID: "m", Label: "Members", Items: items}},
	})
	m := New(icons.TextRenderer{}, keys.Default())
	m.SetSize(80, 40)
	m.SetResource(&r)
	for b.Loop() {
		_ = m.View()
	}
}
