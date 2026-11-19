package messages

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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

// TestViewGolden pins the pane through adds, repeats, resizes, and
// scrolling, and checks the cached lines against a fresh wrap at each step.
func TestViewGolden(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	m := New()
	snap := func(step string) {
		t.Helper()
		var fresh []string
		for _, e := range m.entries {
			fresh = append(fresh, entryLines(e, m.innerWidth())...)
		}
		if m.wrapW != m.innerWidth() || !slices.Equal(m.wrapped, fresh) {
			t.Errorf("%s: cached lines at width %d drifted from the entries at %d", step, m.wrapW, m.innerWidth())
		}
		fmt.Fprintf(&b, "== %s (scroll %d)\n%s\n", step, m.scroll, m.View())
	}
	texts := []struct {
		level Level
		text  string
	}{
		{Info, "loaded 12 resources in projects/demo-project"},
		{Warn, "Cloud Storage: permission denied listing buckets in projects/demo-project; ask for storage.buckets.list"},
		{Error, "compute.googleapis.com is disabled in demo-project, enable it at https://console.cloud.google.com/apis/library/compute.googleapis.com?project=demo-project"},
		{Info, "日本語のメッセージ、幅の広い文字で折り返す テスト"},
		{Info, "short"},
	}
	m.Add(Info, "before any size", at)
	snap("unsized")
	for i, tc := range texts {
		m.Add(tc.level, tc.text, at.Add(time.Duration(i)*time.Second))
	}
	for _, size := range [][2]int{{80, 8}, {40, 10}, {20, 12}, {12, 6}, {80, 3}, {0, 5}} {
		m.SetSize(size[0], size[1])
		snap(fmt.Sprintf("size %dx%d", size[0], size[1]))
	}
	m.SetSize(30, 8)
	snap("30x8")
	m.Add(Info, "short", at.Add(time.Minute))
	m.Add(Info, "short", at.Add(2*time.Minute))
	snap("repeat")
	m.Scroll(-3)
	snap("scrolled back")
	m.Add(Warn, "a new warning while scrolled back that wraps onto lines", at.Add(3*time.Minute))
	snap("add while scrolled")
	m.Add(Warn, "a new warning while scrolled back that wraps onto lines", at.Add(4*time.Minute))
	snap("repeat while scrolled")
	m.SetSize(50, 8)
	snap("resize while scrolled")
	m.GoTop()
	snap("top")
	m.GoBottom()
	snap("bottom")
	for i := range maxEntries + 3 {
		m.Add(Level(i%3), fmt.Sprintf("entry %d with a few words to wrap", i), at.Add(time.Duration(i)*time.Millisecond))
	}
	snap("past the cap")
	m.GoTop()
	snap("top past the cap")
	m.SetSize(24, 6)
	snap("narrow past the cap")
	checkGolden(t, "view", b.String())
}

func BenchmarkView(b *testing.B) {
	m := New()
	for i := range maxEntries {
		m.Add(Level(i%3), fmt.Sprintf("entry %d: Cloud Storage permission denied listing buckets in projects/demo-project", i), at)
	}
	m.SetSize(80, 12)
	for b.Loop() {
		_ = m.View()
	}
}
