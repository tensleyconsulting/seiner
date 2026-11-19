package icons

import (
	"bytes"
	"image"
	"image/png"
	"maps"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/tensleyconsulting/seiner/internal/config"
	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/terminal"
)

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestKittyUploadOnce(t *testing.T) {
	t.Parallel()
	k := newKitty(map[model.ServiceType][]byte{
		model.ServiceGKE: tinyPNG(t),
	})
	first := k.Upload()
	if !strings.Contains(first, "a=t") || !strings.Contains(first, "f=100") {
		t.Fatalf("missing transmit: %q", first[:min(120, len(first))])
	}
	if strings.Contains(first, "a=T") {
		t.Fatal("a=T paints at the cursor; use a=t + virtual placement")
	}
	if !strings.Contains(first, "U=1") {
		t.Fatal("missing virtual placement")
	}
	if !strings.Contains(first, "c=2") || !strings.Contains(first, "r=1") {
		t.Fatal("expected 2x1 list grid")
	}
	if !strings.Contains(first, "c=4") || !strings.Contains(first, "r=2") {
		t.Fatal("expected 4x2 hero grid")
	}
	if !strings.Contains(first, "q=2") {
		t.Fatal("quiet mode required so the TUI is not confused")
	}
	if k.sentCount() != 1 {
		t.Fatalf("sent=%d", k.sentCount())
	}
	if k.Upload() != "" {
		t.Fatal("second upload must not resend PNG bytes")
	}
	cell := k.RenderAt(model.ServiceGKE, SizeList, 0)
	if strings.Contains(cell, "a=t") {
		t.Fatal("render after upload retransmitted")
	}
	if !strings.ContainsRune(cell, kitty.Placeholder) {
		t.Fatal("expected unicode placeholder")
	}
}

func TestKittyRenderFallbackWithoutPNG(t *testing.T) {
	t.Parallel()
	k := newKitty(nil)
	if got := k.RenderAt(model.ServiceBigQuery, SizeList, 0); got != "📊" {
		t.Fatalf("got %q", got)
	}
}

func TestKittyPlaceholderGrid(t *testing.T) {
	t.Parallel()
	k := newKitty(map[model.ServiceType][]byte{model.ServiceCloudStorage: tinyPNG(t)})
	_ = k.Upload()

	list := k.RenderAt(model.ServiceCloudStorage, SizeList, 0)
	if strings.Contains(list, "a=t") {
		t.Fatal("list row should not transmit after upload")
	}
	if strings.Count(list, string(kitty.Placeholder)) != SizeList.Cols() {
		t.Fatalf("list placeholders=%d want %d", strings.Count(list, string(kitty.Placeholder)), SizeList.Cols())
	}
	if k.RenderAt(model.ServiceCloudStorage, SizeList, 1) != "" {
		t.Fatal("list size is one row")
	}

	hero0 := k.RenderAt(model.ServiceCloudStorage, SizeHero, 0)
	hero1 := k.RenderAt(model.ServiceCloudStorage, SizeHero, 1)
	if strings.Contains(hero0, "a=t") || strings.Contains(hero1, "a=t") {
		t.Fatal("hero rows should not transmit after upload")
	}
	if strings.Count(hero0, string(kitty.Placeholder)) != SizeHero.Cols() {
		t.Fatalf("hero row0 placeholders=%d", strings.Count(hero0, string(kitty.Placeholder)))
	}
	if strings.Count(hero1, string(kitty.Placeholder)) != SizeHero.Cols() {
		t.Fatalf("hero row1 placeholders=%d", strings.Count(hero1, string(kitty.Placeholder)))
	}
	if k.Rows(SizeList) != 1 || k.Cols(SizeList) != 2 {
		t.Fatalf("list grid=%dx%d", k.Cols(SizeList), k.Rows(SizeList))
	}
	if k.Rows(SizeHero) != 2 || k.Cols(SizeHero) != 4 {
		t.Fatalf("hero grid=%dx%d", k.Cols(SizeHero), k.Rows(SizeHero))
	}
	if list, _ := k.id(model.ServiceCloudStorage, SizeList); list == k.heroIDs[model.ServiceCloudStorage] {
		t.Fatal("list and hero must use distinct image IDs")
	}
}

func TestNewAutoKitty(t *testing.T) {
	t.Parallel()
	caps := terminal.Capabilities{KittyGraphics: true}
	if got := New(config.IconNone, "", caps); got.Name() != "none" {
		t.Errorf("none: %s", got.Name())
	}
	if got := New(config.IconText, "", caps); got.Name() != "text" {
		t.Errorf("text: %s", got.Name())
	}
	got := New(config.IconAuto, "", caps)
	if got.Name() != "kitty" && got.Name() != "text" {
		t.Errorf("auto: %s", got.Name())
	}
	if New(config.IconKitty, "", terminal.Capabilities{}).Name() == "none" {
		t.Fatal("forced kitty should not be none")
	}
}

func TestTransmitChunksQuiet(t *testing.T) {
	t.Parallel()
	seq := transmitPNG(3, bytes.Repeat([]byte("x"), 8000), SizeList.Cols(), SizeList.Rows(), false)
	if !strings.Contains(seq, "m=1") {
		t.Fatal("expected chunked m=1")
	}
	if !strings.HasSuffix(strings.TrimSpace(seq), "\x1b\\") {
		t.Fatal("APC terminator")
	}
}

func TestTransmitTmuxPassthrough(t *testing.T) {
	t.Parallel()
	plain := transmitPNG(3, bytes.Repeat([]byte("x"), 8000), SizeList.Cols(), SizeList.Rows(), false)
	wrapped := transmitPNG(3, bytes.Repeat([]byte("x"), 8000), SizeList.Cols(), SizeList.Rows(), true)
	apcs := strings.Count(plain, "\x1b_G")
	if apcs < 2 {
		t.Fatalf("expected several APC chunks, got %d", apcs)
	}
	// One passthrough per chunk, ESC doubled inside, and nothing outside.
	if n := strings.Count(wrapped, "\x1bPtmux;"); n != apcs {
		t.Fatalf("passthrough sequences %d, want %d", n, apcs)
	}
	if n := strings.Count(wrapped, "\x1bPtmux;\x1b\x1b_G"); n != apcs {
		t.Fatalf("APCs not escaped inside passthrough: %d of %d", n, apcs)
	}
	// Unwrapped, each chunk is the plain APC followed by the passthrough's
	// own terminator. Base64 holds no ESC, so every ST in plain ends an APC.
	unwrapped := strings.ReplaceAll(strings.ReplaceAll(wrapped, "\x1bPtmux;", ""), "\x1b\x1b", "\x1b")
	if want := strings.ReplaceAll(plain, "\x1b\\", "\x1b\\\x1b\\"); unwrapped != want {
		t.Fatal("unwrapped passthrough does not match the plain upload")
	}
}

func TestNewKittyUnderMultiplexer(t *testing.T) {
	t.Parallel()
	tmux := terminal.Capabilities{Multiplexer: terminal.MultiplexerTmux}
	if got := New(config.IconAuto, "", tmux); got.Name() != "text" {
		t.Errorf("auto in tmux: %s", got.Name())
	}
	forced := New(config.IconKitty, "", tmux)
	k, ok := forced.(*KittyRenderer)
	if !ok {
		t.Fatalf("forced kitty in tmux: %s", forced.Name())
	}
	if up := k.Upload(); !strings.HasPrefix(up, "\x1bPtmux;") {
		t.Fatalf("upload not wrapped for tmux: %q", up[:min(40, len(up))])
	}
	direct, ok := New(config.IconKitty, "", terminal.Capabilities{}).(*KittyRenderer)
	if !ok {
		t.Fatal("forced kitty")
	}
	if up := direct.Upload(); strings.Contains(up, "Ptmux;") {
		t.Fatal("upload wrapped for tmux outside tmux")
	}
}

func TestIDForegroundTruecolor(t *testing.T) {
	t.Parallel()
	if got := idForeground(1); got != "\x1b[38;2;0;0;1m" {
		t.Fatalf("id 1: %q", got)
	}
	row := placeholderRow(1, 0, 2)
	if strings.Contains(row, "38;5;") {
		t.Fatal("256-color IDs break under Bubble Tea color profiles")
	}
	if !strings.Contains(row, "38;2;0;0;1") {
		t.Fatalf("missing truecolor id: %q", row)
	}
}

func TestKittySplashLogo(t *testing.T) {
	t.Parallel()
	k := newKitty(map[model.ServiceType][]byte{
		Logo:             tinyPNG(t),
		model.ServiceGKE: tinyPNG(t),
	})
	k.logo = tinyPNG(t)

	up := k.Upload()
	if !strings.Contains(up, "c=24,r=9") {
		t.Fatal("expected 24x9 splash grid")
	}
	if !strings.Contains(up, "c=8,r=3") {
		t.Fatal("expected 8x3 banner grid")
	}
	if k.Upload() != "" {
		t.Fatal("second upload must not resend the splash logo")
	}

	ids := map[int]bool{}
	for _, size := range []Size{SizeList, SizeHero, SizeBanner, SizeSplash} {
		id, _ := k.id(Logo, size)
		ids[id] = true
	}
	if len(ids) != 4 {
		t.Fatalf("logo sizes must use distinct image ids, got %v", ids)
	}

	tests := []struct {
		name    string
		service model.ServiceType
		row     int
		want    int // placeholder count
	}{
		{"logo top row", Logo, 0, SizeSplash.Cols()},
		{"logo bottom row", Logo, SizeSplash.Rows() - 1, SizeSplash.Cols()},
		{"logo past bottom", Logo, SizeSplash.Rows(), 0},
		{"service has no splash image", model.ServiceGKE, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := k.RenderAt(tc.service, SizeSplash, tc.row)
			if n := strings.Count(got, string(kitty.Placeholder)); n != tc.want {
				t.Fatalf("placeholders=%d want %d (%q)", n, tc.want, got)
			}
		})
	}
}

func TestKittySplashFallsBackWithoutRaster(t *testing.T) {
	t.Parallel()
	k := newKitty(map[model.ServiceType][]byte{Logo: tinyPNG(t)})
	if got := k.RenderAt(Logo, SizeSplash, 0); got != "GCP" {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(k.Upload(), "c=24") {
		t.Fatal("no splash raster, nothing to transmit")
	}
}

// IDs are unique across both sizes and the same on every run, including for
// icons outside KnownServices, and a service without an ID draws as text.
func TestKittyIDsUniqueAndStable(t *testing.T) {
	t.Parallel()
	pngs := map[model.ServiceType][]byte{"zz-custom": {1}, "aa-custom": {1}, model.ServiceCloudStorage: {1}}
	first := newKitty(pngs)
	seen := map[int]model.ServiceType{}
	for _, ids := range []map[model.ServiceType]int{first.listIDs, first.heroIDs} {
		for svc, id := range ids {
			if prev, dup := seen[id]; dup {
				t.Fatalf("id %d used by %s and %s", id, prev, svc)
			}
			seen[id] = svc
		}
	}
	for range 5 {
		again := newKitty(pngs)
		if !maps.Equal(first.listIDs, again.listIDs) || !maps.Equal(first.heroIDs, again.heroIDs) {
			t.Fatal("ids differ between runs")
		}
	}
	if first.listIDs["aa-custom"] >= first.listIDs["zz-custom"] {
		t.Fatal("extra icons should take ids in name order")
	}
	if _, ok := first.id("unknown-service", SizeList); ok {
		t.Fatal("unknown service must have no id")
	}
}
