package splash

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/tensleyconsulting/seiner/internal/config"
	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/terminal"
	"github.com/tensleyconsulting/seiner/internal/ui/icons"
	"github.com/tensleyconsulting/seiner/internal/ui/styles"
)

var t0 = time.Unix(1_700_000_000, 0)

func newIntro(t *testing.T, r icons.Renderer, w, h int) Model {
	t.Helper()
	m := New(r, "demo-project", t0)
	m.SetSize(w, h)
	if m.Done() {
		t.Fatalf("%dx%d should fit the intro", w, h)
	}
	return m
}

// run advances frame by frame until the intro ends or limit passes.
func run(m Model, limit time.Duration) Model {
	for d := time.Duration(0); d <= limit && !m.Done(); d += frameEvery {
		m, _ = m.Update(FrameMsg{At: t0.Add(d)})
	}
	return m
}

func kittyRenderer(t *testing.T) icons.Renderer {
	t.Helper()
	r := icons.New(config.IconKitty, "", terminal.Capabilities{KittyGraphics: true})
	if !icons.Graphics(r) {
		t.Skip("no embedded icon assets")
	}
	return r
}

func TestSlotsFillOutwardFromCenter(t *testing.T) {
	t.Parallel()
	m := newIntro(t, icons.TextRenderer{}, 100, 30)
	l, _ := m.layout()
	cx := l.w/2 - l.iconW/2
	seen := map[[2]int]bool{}
	prevDist := -1
	for i := 0; i < l.perRow; i++ {
		x, y, ok := l.slot(i)
		if !ok {
			t.Fatalf("slot %d missing", i)
		}
		if seen[[2]int{x, y}] {
			t.Fatalf("slot %d reuses %d,%d", i, x, y)
		}
		seen[[2]int{x, y}] = true
		if x-netPad-1 < 0 || x+l.iconW+netPad >= l.w {
			t.Fatalf("slot %d at x=%d leaves no room for the net", i, x)
		}
		if d := abs(x - cx); d < prevDist {
			t.Fatalf("slot %d moved back toward the center", i)
		} else {
			prevDist = d
		}
		if i > 0 && i%2 == 1 && x >= cx || i > 0 && i%2 == 0 && x <= cx {
			t.Fatalf("slot %d on the wrong side: x=%d center=%d", i, x, cx)
		}
	}
	if _, y, _ := l.slot(l.perRow); y <= l.slotsY {
		t.Fatal("overflow should start a second row")
	}
	if _, _, ok := l.slot(l.perRow * l.slotRows); ok {
		t.Fatal("slots past the last row must be refused")
	}
}

// purse runs frames until the net is pursed.
func purse(t *testing.T, m Model) Model {
	t.Helper()
	for d := time.Duration(0); m.pursedAt.IsZero(); d += frameEvery {
		if d > 20*time.Second || m.Done() {
			t.Fatal("the net was never pursed")
		}
		m, _ = m.Update(FrameMsg{At: t0.Add(d)})
	}
	return m
}

func TestSwimmersMillUntilPursed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		r        func(*testing.T) icons.Renderer
		w, h     int
		enclosed bool // the boat has room to sail around every swimmer
	}{
		{"text", func(*testing.T) icons.Renderer { return icons.TextRenderer{} }, 120, 40, true},
		{"text narrow", func(*testing.T) icons.Renderer { return icons.TextRenderer{} }, minWidth, 24, false},
		{"kitty", kittyRenderer, 120, 40, true},
	} {
		for _, tt := range []struct{ from, depth float64 }{{0, 0}, {0.3, 1}, {0.5, 0.5}, {1, 1}} {
			t.Run(fmt.Sprintf("%s %+v", tc.name, tt), func(t *testing.T) {
				t.Parallel()
				m := newIntro(t, tc.r(t), tc.w, tc.h)
				l, _ := m.layout()
				for _, svc := range model.KnownServices()[:4] {
					m.Arrive(svc, 1, nil)
				}
				for i := range m.swimmers {
					m.swimmers[i].from, m.swimmers[i].depth = tt.from, tt.depth
				}
				m.Loaded()
				m = purse(t, m)
				p := m.plan(l)
				pursed := m.pursedAt
				for i, c := range p.courses {
					if c.slot < 0 {
						t.Fatalf("swimmer %d got away from a bunt with room", i)
					}
					x0, _ := c.loiter(c.caught)
					if tc.enclosed && (x0 <= p.set.left || x0+l.iconW >= p.set.right) {
						t.Fatalf("swimmer %d at x=%d was outside the net %+v when it was pursed", i, x0, p.set)
					}
					launch := m.swimmers[i].launch
					m.now = launch
					x, y, seated := m.swimmerAt(l, p, i)
					for k := 0; !seated; k++ {
						m.now = launch.Add(time.Duration(k) * stepEvery)
						nx, ny, s := m.swimmerAt(l, p, i)
						if abs(nx-x) > strideX || abs(ny-y) > 1 {
							t.Fatalf("swimmer %d jumped from %d,%d to %d,%d at step %d", i, x, y, nx, ny, k)
						}
						if nx < 0 || nx+l.iconW > l.w {
							t.Fatalf("swimmer %d off screen at x=%d", i, nx)
						}
						if m.now.Before(pursed) {
							if k > c.caught {
								t.Fatalf("swimmer %d caught at step %d, before the net was pursed", i, c.caught)
							}
							if ny < l.seaTop() || ny+l.iconH > l.leadY() {
								t.Fatalf("swimmer %d mills at row %d, outside the sea %d–%d", i, ny, l.seaTop(), l.leadY())
							}
						} else if lead := m.net(p).bottom; ny+l.iconH > lead {
							t.Fatalf("swimmer %d at row %d fell under the leadline at %d", i, ny, lead)
						}
						x, y, seated = nx, ny, s
						if k > int(time.Minute/stepEvery) {
							t.Fatal("swimmer never sat down")
						}
					}
					tx, ty, _ := l.slot(c.slot)
					if x != tx+p.shift || y != ty {
						t.Fatalf("swimmer %d sat at %d,%d want %d,%d", i, x, y, tx+p.shift, ty)
					}
					if x <= p.bunt.left || x+l.iconW > p.bunt.right || y+l.iconH >= p.bunt.bottom {
						t.Fatalf("swimmer %d sat at %d,%d, outside the bunt %+v", i, x, y, p.bunt)
					}
				}
			})
		}
	}
}

func TestPurseClosesAroundCatch(t *testing.T) {
	t.Parallel()
	m := newIntro(t, icons.TextRenderer{}, 100, 30)
	l, _ := m.layout()
	if b := l.bunt(0); b.right-b.left+1 < ansi.StringWidth(netLabel)+4 {
		t.Fatalf("empty bunt %+v cannot hold its label", b)
	}
	for i, svc := range model.KnownServices()[:4] {
		m.Arrive(svc, i+1, nil)
	}
	m.Loaded()

	var prev netBox
	sawLabel, sawSkiff, sawFresh := false, false, false
	for d := time.Duration(0); d < 10*time.Second && !m.Revealing(); d += frameEvery {
		m, _ = m.Update(FrameMsg{At: t0.Add(d)})
		p := m.plan(l)
		n := m.net(p)
		if m.pursedAt.IsZero() {
			if n.left != l.stern(0) || n.right < prev.right || n.bottom != l.leadY() {
				t.Fatalf("at %v the net %+v is not paid out from the skiff at %d to the stern", d, n, l.stern(0))
			}
		} else {
			between := func(v, a, b int) bool { return min(a, b) <= v && v <= max(a, b) }
			if !between(n.left, p.set.left, p.bunt.left) || !between(n.right, p.set.right, p.bunt.right) {
				t.Fatalf("at %v the net %+v strayed outside the set %+v and the bunt %+v", d, n, p.set, p.bunt)
			}
			if n.bottom > prev.bottom || n.bottom < p.bunt.bottom {
				t.Fatalf("at %v the leadline went from %d to %d, bunt at %d", d, prev.bottom, n.bottom, p.bunt.bottom)
			}
		}
		if n.left < 0 || n.right >= l.w {
			t.Fatalf("at %v the net left the screen: %+v", d, n)
		}
		view := strings.Split(ansi.Strip(m.View("")), "\n")
		sawLabel = sawLabel || strings.Contains(view[l.netY], netLabel)
		sawSkiff = sawSkiff || strings.Contains(view[l.netY-1], skiff)
		sawFresh = sawFresh || n.fresh
		prev = n
	}
	if !sawLabel || !sawSkiff {
		t.Fatalf("label shown %v, skiff shown %v", sawLabel, sawSkiff)
	}
	if !sawFresh {
		t.Fatal("closing should light up the net")
	}
	p := m.plan(l)
	if n := m.net(p); n.left != p.bunt.left || n.right != p.bunt.right || n.bottom != p.bunt.bottom || n.fresh {
		t.Fatalf("the net %+v should settle as the bunt %+v", n, p.bunt)
	}
}

func TestBoatSailsRight(t *testing.T) {
	t.Parallel()
	for _, w := range []int{minWidth, 60, 100, 200} {
		m := newIntro(t, icons.TextRenderer{}, w, 30)
		l, _ := m.layout()
		prev := l.boatX(0)
		if prev > l.logoX {
			t.Fatalf("w=%d: boat sets out at %d, right of home %d", w, prev, l.logoX)
		}
		for d := time.Duration(0); d < 20*time.Second; d += frameEvery {
			x := l.boatX(d)
			if x < prev || x-prev > 1 {
				t.Fatalf("w=%d at %v: boat went from %d to %d", w, d, prev, x)
			}
			if x < 0 || x+l.logoW > l.w {
				t.Fatalf("w=%d at %v: boat at %d leaves the screen", w, d, x)
			}
			prev = x
		}
		if w >= 100 && prev-l.boatX(0) < 2*sailSpan {
			t.Fatalf("w=%d: boat only sailed %d columns", w, prev-l.boatX(0))
		}
		if l.setEnd() > l.stern(time.Hour) {
			t.Fatalf("w=%d: swimmers reach for %d, past where the stern stops at %d", w, l.setEnd(), l.stern(time.Hour))
		}
	}
}

func TestBoatStopsToPurse(t *testing.T) {
	t.Parallel()
	m := newIntro(t, icons.TextRenderer{}, 100, 30)
	m.Arrive(model.ServiceGKE, 2, nil)
	m.Loaded()
	l, _ := m.layout()
	var before rig
	for d := time.Duration(0); m.pursedAt.IsZero(); d += frameEvery {
		before = m.rig(l, m.plan(l))
		m, _ = m.Update(FrameMsg{At: t0.Add(d)})
	}
	if !before.sailing {
		t.Fatal("the boat should still be under way until the net is pursed")
	}
	at := m.rig(l, m.plan(l))
	for d := m.now.Sub(t0); !m.Done(); d += frameEvery {
		m, _ = m.Update(FrameMsg{At: t0.Add(d)})
		if r := m.rig(l, m.plan(l)); r.boatX != at.boatX || r.sailing {
			t.Fatalf("at %v the boat moved from %d to %d after pursing", d, at.boatX, r.boatX)
		}
	}
}

func TestTimeline(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		arrive     int
		loaded     bool
		wantReveal time.Duration // reveal starts no earlier than this
		maxReveal  time.Duration // and no later
	}{
		{"fast load holds the catch", 3, true, holdSeated, 5*time.Second + holdSeated},
		{"nothing to show", 0, true, minSet + holdSeated, minSet + holdSeated + time.Second},
		{"slow load is capped", 2, false, maxIntro, maxIntro + frameEvery},
		{"late catch still holds past the cap", 30, true, maxIntro, maxIntro + 5*time.Second + holdSeated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newIntro(t, icons.TextRenderer{}, 100, 30)
			for i := range tc.arrive {
				m.Arrive(model.ServiceType(fmt.Sprintf("svc-%d", i)), 1, nil)
			}
			if tc.loaded {
				m.Loaded()
			}
			var revealAt time.Duration = -1
			for d := time.Duration(0); d <= 20*time.Second && !m.Done(); d += frameEvery {
				m, _ = m.Update(FrameMsg{At: t0.Add(d)})
				if revealAt < 0 && m.Revealing() {
					revealAt = d
				}
			}
			if !m.Done() {
				t.Fatal("intro never finished")
			}
			if revealAt < tc.wantReveal || revealAt > tc.maxReveal {
				t.Fatalf("reveal at %v, want [%v, %v]", revealAt, tc.wantReveal, tc.maxReveal)
			}
			if tc.loaded {
				if held := m.revealAt.Sub(m.seatedAt); m.seatedAt.IsZero() || held < holdSeated {
					t.Fatalf("catch held %v after the last swimmer sat, want %v", held, holdSeated)
				}
			}
			if m.now.Sub(t0) > revealAt+revealFor+frameEvery {
				t.Fatalf("reveal ran %v", m.now.Sub(t0)-revealAt)
			}
		})
	}
}

// A boat with no room to sail around the swimmers still purses the net.
func TestNarrowSetFinishes(t *testing.T) {
	t.Parallel()
	m := newIntro(t, icons.TextRenderer{}, minWidth, 24)
	for _, svc := range model.KnownServices()[:6] {
		m.Arrive(svc, 1, nil)
	}
	m.Loaded()
	if m = run(m, 15*time.Second); !m.Done() {
		t.Fatal("intro never finished")
	}
}

func TestArriveSpacesLaunchesAndMergesScopes(t *testing.T) {
	t.Parallel()
	m := newIntro(t, icons.TextRenderer{}, 100, 30)
	m.Arrive(model.ServiceIAM, 2, nil)
	m.Arrive(model.ServiceCloudStorage, 3, nil)
	m.Arrive(model.ServiceIAM, 4, errors.New("org scope denied"))

	if len(m.swimmers) != 2 {
		t.Fatalf("swimmers=%d, a service gets one swimmer", len(m.swimmers))
	}
	iam := m.swimmers[m.bySvc[model.ServiceIAM]]
	if iam.count != 6 || !iam.failed {
		t.Fatalf("iam=%+v", iam)
	}
	if got := m.swimmers[0].launch.Sub(t0); got != firstLaunch {
		t.Fatalf("first launch at %v", got)
	}
	if gap := m.swimmers[1].launch.Sub(m.swimmers[0].launch); gap != launchGap {
		t.Fatalf("launch gap %v", gap)
	}
}

func TestArriveSkipsEmptyServices(t *testing.T) {
	t.Parallel()
	m := newIntro(t, icons.TextRenderer{}, 100, 30)
	m.Arrive(model.ServiceGKE, 0, nil)                       // empty: no swimmer
	m.Arrive(model.ServiceCloudSQL, 0, errors.New("denied")) // failed: swimmer
	m.Arrive(model.ServiceCloudStorage, 3, nil)              // found some: swimmer
	m.Arrive(model.ServiceGKE, 2, nil)                       // another scope found some: swimmer now

	var got []model.ServiceType
	for _, w := range m.swimmers {
		got = append(got, w.svc)
	}
	want := []model.ServiceType{model.ServiceCloudSQL, model.ServiceCloudStorage, model.ServiceGKE}
	if !slices.Equal(got, want) {
		t.Fatalf("swimmers = %v, want %v", got, want)
	}
	if gke := m.swimmers[m.bySvc[model.ServiceGKE]]; gke.count != 2 {
		t.Fatalf("gke count = %d", gke.count)
	}
}

func TestTitleStaysPutAsServicesArrive(t *testing.T) {
	t.Parallel()
	m := newIntro(t, icons.TextRenderer{}, 100, 30)
	m.Arrive(model.ServiceIAM, 2, nil)
	m.Arrive(model.ServiceCloudStorage, 3, nil)
	m.Arrive(model.ServiceGKE, 1, nil)
	l, _ := m.layout()

	cols := map[int]bool{}
	labels := map[string]bool{}
	for d := time.Duration(0); d <= firstLaunch+3*launchGap; d += frameEvery {
		m, _ = m.Update(FrameMsg{At: t0.Add(d)})
		row := ansi.Strip(m.scene(l).row(l.titleY))
		cols[strings.Index(row, "catching")] = true
		if _, label, ok := strings.Cut(row, "· "); ok {
			labels[strings.TrimSpace(label)] = true
		}
	}
	if len(labels) < 3 {
		t.Fatalf("expected each service label in turn, got %v", labels)
	}
	if len(cols) != 1 || cols[-1] {
		t.Fatalf("\"catching\" moved between columns %v", cols)
	}
}

func TestEndsEarly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		do   func(*Model)
	}{
		{"skip", func(m *Model) { m.Skip() }},
		{"terminal too short", func(m *Model) { m.SetSize(100, 10) }},
		{"terminal too narrow", func(m *Model) { m.SetSize(30, 40) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newIntro(t, icons.TextRenderer{}, 100, 30)
			tc.do(&m)
			if !m.Done() {
				t.Fatal("intro should be done")
			}
			if _, cmd := m.Update(FrameMsg{At: t0}); cmd != nil {
				t.Fatal("a finished intro must stop ticking")
			}
		})
	}
	if !(Model{}).Done() {
		t.Fatal("zero Model must be done")
	}
}

func TestViewKeepsGrid(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		r    func(*testing.T) icons.Renderer
	}{
		{"text", func(*testing.T) icons.Renderer { return icons.TextRenderer{} }},
		{"kitty", kittyRenderer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const w, h = 90, 32
			m := newIntro(t, tc.r(t), w, h)
			for i, svc := range model.KnownServices() {
				m.Arrive(svc, i, nil)
			}
			m.Loaded()
			under := strings.TrimSuffix(strings.Repeat("app view\n", h), "\n")

			sawFog, sawApp := false, false
			for d := time.Duration(0); !m.Done(); d += frameEvery {
				m, _ = m.Update(FrameMsg{At: t0.Add(d)})
				if m.Done() {
					break
				}
				view := m.View(under)
				lines := strings.Split(view, "\n")
				if len(lines) != h {
					t.Fatalf("at %v: %d lines", d, len(lines))
				}
				for y, line := range lines {
					if got := ansi.StringWidth(line); got != w {
						t.Fatalf("at %v line %d width %d: %q", d, y, got, ansi.Strip(line))
					}
				}
				sawFog = sawFog || strings.Contains(view, "▓")
				sawApp = sawApp || strings.Contains(ansi.Strip(view), "app view")
			}
			if !sawFog || !sawApp {
				t.Fatalf("reveal never showed fog (%v) or the app (%v)", sawFog, sawApp)
			}
		})
	}
}

func TestKittySceneUsesPlaceholders(t *testing.T) {
	t.Parallel()
	m := newIntro(t, kittyRenderer(t), 100, 32)
	m.Arrive(model.ServiceGKE, 3, nil)
	m = run(m, 2*time.Second)
	view := m.View("")
	want := icons.SizeSplash.Cols()*icons.SizeSplash.Rows() + icons.SizeHero.Cols()*icons.SizeHero.Rows()
	if n := strings.Count(view, string(kitty.Placeholder)); n < want {
		t.Fatalf("placeholders=%d want at least logo + one icon (%d)", n, want)
	}
	if strings.Contains(view, "\x1b_G") {
		t.Fatal("view must not carry Kitty APC; images go through Upload")
	}
}

func TestRevealEndsOnApp(t *testing.T) {
	t.Parallel()
	m := newIntro(t, icons.TextRenderer{}, 60, 24)
	l, _ := m.layout()
	under := strings.TrimSuffix(strings.Repeat(strings.Repeat("x", 60)+"\n", 24), "\n")
	got := ansi.Strip(m.reveal(l, m.scene(l), under, 1))
	if got != under {
		t.Fatalf("full reveal should be the app view, got\n%s", got)
	}
	got = ansi.Strip(m.reveal(l, m.scene(l), under, 0))
	if strings.Contains(got, "x") {
		t.Fatal("no app cells before the reveal starts")
	}
}

// Restyle redraws the text logo in the current palette.
func TestRestyleRedrawsLogo(t *testing.T) {
	t.Cleanup(func() { styles.SetDark(true) })
	m := New(icons.TextRenderer{}, "p", time.Now())
	before := strings.Join(m.logo[2], "")
	styles.SetDark(false)
	m.Restyle()
	if after := strings.Join(m.logo[2], ""); after == before {
		t.Fatal("logo kept the dark palette's colors")
	}
}
