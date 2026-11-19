// Package splash is the startup intro. A large Seiner logo sails left to
// right, paying a purse seine out over its stern while a skiff holds the
// net's far end: a line of floats on the surface, a wall of net hanging from
// it, and a leadline of purse rings along the bottom. As each provider's
// first results arrive, its icon turns up in the sea and mills about. Once
// every result is in, the boat stops and purses the net: the leadline rises
// under the icons, lifting them into the bunt, and the net's ends draw in
// around them. Then a fog front rolls out from the logo and dissolves the
// scene into the explorer underneath.
//
// Positions are a function of elapsed time, so a dropped frame makes the
// animation choppier, not longer.
package splash

import (
	"cmp"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/icons"
	"github.com/tensleyconsulting/seiner/internal/ui/navigation"
	"github.com/tensleyconsulting/seiner/internal/ui/styles"
)

const (
	frameEvery  = 33 * time.Millisecond
	stepEvery   = 30 * time.Millisecond   // a swimmer advances one path point
	haulRise    = 2                       // steps per row a swimmer, and the leadline under it, rises while pursing
	minLoiter   = 500 * time.Millisecond  // a swimmer mills about at least this long before the net is pursed
	firstLaunch = 350 * time.Millisecond  // give the logo a moment alone
	launchGap   = 110 * time.Millisecond  // spacing between swimmers
	minSet      = 1200 * time.Millisecond // the boat pays out net at least this long before pursing
	minIntro    = 1500 * time.Millisecond
	maxIntro    = 4 * time.Second // reveal starts by now if still loading
	holdSeated  = 2 * time.Second // time to read the counts before the reveal
	revealFor   = 800 * time.Millisecond
	flashFor    = 250 * time.Millisecond // the net lit up once it is pursed
	sailEvery   = 50 * time.Millisecond  // the boat advances one column

	strideX  = 3 // columns per step a swimmer, or an end of the net, moves across the bunt, at most
	slotGap  = 4 // columns between seated icons
	netPad   = 2 // columns between the bunt's sides and the icons inside
	boomRows = 2 // rows between the logo and the floats, where the skiff rides
	minSea   = 3 // open rows below the bunt, beyond an icon's height
	maxSea   = 12
	sailSpan = 20 // columns left of center the boat sets out from, at most
	setSpan  = 60 // columns of net the boat pays out around the swimmers, at most

	wanderX  = 5 // columns a milling swimmer drifts either way
	wanderY  = 1 // rows a milling swimmer drifts either way
	floatGap = 3 // columns between floats on the corkline
	ringGap  = 4 // columns between purse rings on the leadline
	fogBand  = 0.12
	minWidth = 40
)

// netLabel matches the explorer pane's title, which the intro fogs into.
const netLabel = navigation.Title

// skiff is the small boat that holds the net's far end.
const skiff = "▜██▛"

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// fogShades runs from thin (the cleared side) to thick (the scene side).
var fogShades = []string{"░", "▒", "▓"}

// FrameMsg advances the animation.
type FrameMsg struct{ At time.Time }

// Model is the intro state. The zero value is finished.
type Model struct {
	renderer icons.Renderer
	project  string
	w, h     int

	start, now time.Time
	lastLaunch time.Time
	pursedAt   time.Time // the boat stopped and began closing the net
	seatedAt   time.Time
	revealAt   time.Time
	loaded     bool
	done       bool

	swimmers []swimmer
	bySvc    map[model.ServiceType]int
	logo     [][]string
}

type swimmer struct {
	svc    model.ServiceType
	count  int
	failed bool
	launch time.Time
	from   float64 // where across the sea it turns up, 0 to 1
	depth  float64 // how deep, 0 just under the bunt to 1 just over the leadline
	seed   float64 // its own drift
	sprite [][]string
}

// New starts an intro at now for project.
func New(r icons.Renderer, project string, now time.Time) Model {
	return Model{
		renderer:   r,
		project:    project,
		start:      now,
		now:        now,
		lastLaunch: now.Add(firstLaunch - launchGap),
		bySvc:      map[model.ServiceType]int{},
		logo:       logoCells(r),
	}
}

// Init schedules the first frame.
func (m Model) Init() tea.Cmd {
	if m.done || m.renderer == nil {
		return nil
	}
	return tick()
}

func tick() tea.Cmd {
	return tea.Tick(frameEvery, func(t time.Time) tea.Msg { return FrameMsg{At: t} })
}

// Done reports whether the app should draw itself instead.
func (m Model) Done() bool { return m.done || m.renderer == nil }

// Revealing reports whether View needs the app's own view underneath.
func (m Model) Revealing() bool { return !m.Done() && !m.revealAt.IsZero() }

// Skip ends the intro immediately.
func (m *Model) Skip() { m.done = true }

// SetSize lays out for a new terminal size. A terminal too small to hold the
// scene skips the intro rather than drawing a broken one.
func (m *Model) SetSize(w, h int) {
	m.w, m.h = w, h
	if _, ok := m.layout(); !ok {
		m.done = true
	}
}

// Restyle renders the logo again after the theme changed (styles.SetDark),
// since the text logo is kept with its colors.
func (m *Model) Restyle() {
	if m.Done() {
		return
	}
	m.logo = logoCells(m.renderer)
}

// Arrive records one provider's result. The first result for a service
// with resources (or an error) launches its swimmer; later ones (another
// scope on the startup path) add to its count. A service that found nothing
// gets no swimmer, so the slots go to the ones that did.
func (m *Model) Arrive(svc model.ServiceType, n int, err error) {
	if m.Done() {
		return
	}
	if i, ok := m.bySvc[svc]; ok {
		m.swimmers[i].count += n
		m.swimmers[i].failed = m.swimmers[i].failed || err != nil
		return
	}
	if n == 0 && err == nil {
		return
	}
	launch := m.lastLaunch.Add(launchGap)
	if m.now.After(launch) {
		launch = m.now
	}
	m.lastLaunch = launch
	m.bySvc[svc] = len(m.swimmers)
	m.swimmers = append(m.swimmers, swimmer{
		svc:    svc,
		count:  n,
		failed: err != nil,
		launch: launch,
		from:   rand.Float64(),
		depth:  rand.Float64(),
		seed:   rand.Float64() * 1000,
		sprite: m.sprite(svc),
	})
	m.seatedAt = time.Time{}
}

// Loaded marks the startup scope as fully reported.
func (m *Model) Loaded() { m.loaded = true }

// Update handles FrameMsg and returns the next frame until the intro ends.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	f, ok := msg.(FrameMsg)
	if !ok || m.Done() {
		return m, nil
	}
	m.advance(f.At)
	if m.Done() {
		return m, nil
	}
	return m, tick()
}

func (m *Model) advance(now time.Time) {
	m.now = now
	elapsed := now.Sub(m.start)
	if !m.revealAt.IsZero() {
		if now.Sub(m.revealAt) >= revealFor {
			m.done = true
		}
		return
	}
	// The whole catch is in the water and inside the net before it closes,
	// so nothing turns up once it is pursed.
	if m.pursedAt.IsZero() && m.loaded && elapsed >= minSet && m.surrounded() {
		m.pursedAt = now
	}
	if m.loaded && m.seatedAt.IsZero() && m.allSeated() {
		m.seatedAt = now
	}
	// Once loading is done the cap no longer applies: the net is pursed and
	// the full catch holds so the counts can be read.
	settled := !m.seatedAt.IsZero() && now.Sub(m.seatedAt) >= holdSeated && elapsed >= minIntro
	if settled || !m.loaded && elapsed >= maxIntro {
		m.revealAt = now
	}
}

// surrounded reports whether every swimmer has milled about for minLoiter
// and the boat has paid out net past it, or as far as it can.
func (m Model) surrounded() bool {
	l, ok := m.layout()
	if !ok {
		return false
	}
	stern := l.stern(m.now.Sub(m.start))
	for i, c := range m.plan(l).courses {
		if m.now.Sub(m.swimmers[i].launch) < minLoiter || c.ax+l.iconW+wanderX >= stern && stern < l.setEnd() {
			return false
		}
	}
	return true
}

// allSeated reports whether the net is pursed and every swimmer it caught is
// in its slot.
func (m Model) allSeated() bool {
	l, ok := m.layout()
	if !ok || m.pursedAt.IsZero() {
		return false
	}
	p := m.plan(l)
	if m.now.Sub(m.pursedAt) < time.Duration(p.purse)*stepEvery {
		return false
	}
	for i, c := range p.courses {
		if _, _, seated := m.swimmerAt(l, p, i); c.slot >= 0 && !seated {
			return false
		}
	}
	return true
}

func (m Model) sprite(svc model.ServiceType) [][]string {
	r := m.renderer
	w, h := r.Cols(icons.SizeHero), r.Rows(icons.SizeHero)
	rows := make([][]string, h)
	for y := range rows {
		s := r.RenderAt(svc, icons.SizeHero, y)
		if !icons.Graphics(r) {
			s = styles.Icon().Width(w).Render(s)
		}
		rows[y] = splitCells(s, w)
	}
	return rows
}

// layout is the scene geometry for one terminal size. From the top: the
// logo, the boom's line and the skiff, the floats, the bunt at its fullest,
// open sea down to the leadline, the seabed, and the title with its hint.
type layout struct {
	w, h         int
	logoX, logoY int
	logoW, logoH int
	iconW, iconH int
	lineY        int // first row of the boom's line, under the logo
	netY         int // the corkline of floats
	slotsY       int // top of the first row of seated icons
	slotRows     int
	perRow       int
	seabedY      int
	titleY       int
}

func (m Model) layout() (layout, bool) {
	if m.renderer == nil || len(m.logo) == 0 || m.w < minWidth {
		return layout{}, false
	}
	l := layout{
		w:     m.w,
		h:     m.h,
		logoW: len(m.logo[0]),
		logoH: len(m.logo),
		iconW: m.renderer.Cols(icons.SizeHero),
		iconH: m.renderer.Rows(icons.SizeHero),
	}
	// An odd count keeps the center slot centered; the outermost slots
	// leave room for the net's padding and sides.
	l.perRow = 2*((m.w/2-l.iconW/2-netPad-1)/(l.iconW+slotGap)) + 1
	fixed := func(rows int) int {
		return l.logoH + boomRows + rows*l.slotH() + 1 + 3 // net edges; seabed, title, hint
	}
	sea := l.iconH + minSea
	l.slotRows = 2
	if fixed(l.slotRows)+sea > m.h {
		l.slotRows = 1
	}
	if fixed(l.slotRows)+sea > m.h || l.perRow < 3 || l.logoW > m.w {
		return layout{}, false
	}
	sea = min(maxSea, m.h-fixed(l.slotRows))
	l.logoY = (m.h - fixed(l.slotRows) - sea) / 2
	l.logoX = (m.w - l.logoW) / 2
	l.lineY = l.logoY + l.logoH
	l.netY = l.lineY + boomRows
	l.slotsY = l.netY + 1
	l.seabedY = l.slotsY + l.slotRows*l.slotH() + sea
	l.titleY = l.seabedY + 1
	return l, true
}

// slotH is one row of seated icons: icon, count, gap. The last row's gap is
// the bunt's bottom edge.
func (l layout) slotH() int { return l.iconH + 2 }

// slot returns the top-left of seated slot i, filling outward from the
// center so the bunt widens on alternate sides. ok is false once every slot
// row is full.
func (l layout) slot(i int) (x, y int, ok bool) {
	row, k := i/l.perRow, i%l.perRow
	if row >= l.slotRows {
		return 0, 0, false
	}
	off := (k + 1) / 2
	if k%2 == 1 {
		off = -off
	}
	x = l.w/2 - l.iconW/2 + off*(l.iconW+slotGap)
	return x, l.slotsY + row*l.slotH(), true
}

// seaTop is the first row below the bunt at its deepest.
func (l layout) seaTop() int { return l.slotsY + l.slotRows*l.slotH() }

// leadY is the row the leadline hangs at while the net is set.
func (l layout) leadY() int { return l.seabedY - 1 }

// boatX is the logo's left column t into the intro. It sets out left of
// center and advances a column every sailEvery until it meets the right edge.
func (l layout) boatX(t time.Duration) int {
	span := min((l.w-l.logoW)/2, sailSpan)
	return min(l.logoX-span+int(max(0, t)/sailEvery), l.w-l.logoW)
}

// stern is the column under the boat's boom t into the intro, where the net
// pays out.
func (l layout) stern(t time.Duration) int { return l.boatX(t) + l.logoW/8 }

// setEnd is the furthest column a swimmer reaches for: past it, the boat would
// be sailing off the screen or the net would take too long to set.
func (l layout) setEnd() int {
	return min(l.w-l.logoW+l.logoW/8, l.stern(0)+setSpan)
}

// bunt is the pursed net at home: wide enough for its label and for the
// first n slots with room around them.
func (l layout) bunt(n int) netBox {
	w := ansi.StringWidth(netLabel) + 6
	b := netBox{left: l.w/2 - w/2, bottom: l.slotsY + l.slotH() - 1}
	b.right = b.left + w - 1
	for i := range n {
		x, y, _ := l.slot(i)
		b.left = min(b.left, x-netPad-1)
		b.right = max(b.right, x+l.iconW+netPad)
		b.bottom = max(b.bottom, y+l.slotH()-1)
	}
	return b
}

// course is a swimmer's whole path: it mills about a spot until the net is
// pursed, then rises ahead of the leadline to its slot's row and slides
// across the bunt into its slot.
type course struct {
	ax, ay int     // the spot it mills about
	seed   float64 // its drift
	caught int     // steps after launch the net is pursed; -1 until then
	slot   int     // -1 until the net is pursed, or if the bunt is full
	rise   int     // steps from the purse up to its slot's row
	haul   int     // steps from the purse into its slot
}

// plan is the set at one moment: every swimmer's course, the net as the boat
// laid it, and, once it is pursed, the bunt it closes into.
type plan struct {
	courses []course
	set     netBox // from the skiff to the stern, down to the leadline
	bunt    netBox // in the middle of the set, already shifted
	shift   int    // columns the bunt and its slots sit right of home
	purse   int    // steps from pursing until the net is closed into the bunt
}

// sailed is how long the boat has been under way. It stops to purse the
// net, or where it is when the reveal starts if loading outlasted the intro.
func (m Model) sailed() time.Duration {
	switch {
	case !m.pursedAt.IsZero():
		return m.pursedAt.Sub(m.start)
	case !m.revealAt.IsZero():
		return m.revealAt.Sub(m.start)
	}
	return m.now.Sub(m.start)
}

// plan lays out the set. Swimmers turn up anywhere between the skiff and
// setEnd, so the boat sails around them. Once the net is pursed, the swimmers nearest the
// bunt get its slots, matched left to right so their paths do not cross;
// any more than the bunt holds get away.
func (m Model) plan(l layout) plan {
	left := l.stern(0)
	p := plan{
		courses: make([]course, len(m.swimmers)),
		set:     netBox{left: left, right: max(left+1, l.stern(m.sailed())), bottom: l.leadY()},
	}
	half := l.iconW / 2
	lo, hi := max(left+1, 0)+wanderX, l.setEnd()-l.iconW-wanderX-1
	for i, w := range m.swimmers {
		c := course{seed: w.seed, caught: -1, slot: -1}
		// Swimmers that turn up together spread out rather than bunch.
		at := math.Mod(0.35*w.from+0.618*float64(i), 1)
		c.ax = max(wanderX, min(lo+int(math.Round(at*float64(max(0, hi-lo)))), l.w-l.iconW-wanderX))

		top, bottom := l.seaTop()+wanderY, l.leadY()-l.iconH-wanderY
		c.ay = (top + bottom) / 2
		if bottom > top {
			c.ay = top + int(math.Round(w.depth*float64(bottom-top)))
		}
		p.courses[i] = c
	}
	if m.pursedAt.IsZero() {
		return p
	}

	n := min(len(m.swimmers), l.perRow*l.slotRows)
	p.bunt = l.bunt(n)
	p.shift = max(-p.bunt.left, min((p.set.left+p.set.right)/2-l.w/2, l.w-1-p.bunt.right))
	p.bunt.left += p.shift
	p.bunt.right += p.shift
	ends := max(abs(p.bunt.left-p.set.left), abs(p.bunt.right-p.set.right))
	p.purse = max((p.set.bottom-p.bunt.bottom)*haulRise, (ends+strideX-1)/strideX)

	type caught struct{ i, x int }
	var all []caught
	for i := range p.courses {
		c := &p.courses[i]
		c.caught = int(m.pursedAt.Sub(m.swimmers[i].launch) / stepEvery)
		x, _ := c.loiter(c.caught)
		all = append(all, caught{i, x})
	}
	mid := (p.bunt.left + p.bunt.right) / 2
	slices.SortStableFunc(all, func(a, b caught) int { return cmp.Compare(abs(a.x+half-mid), abs(b.x+half-mid)) })
	all = all[:n]
	slices.SortStableFunc(all, func(a, b caught) int { return cmp.Compare(a.x, b.x) })
	slots := make([]int, n)
	for i := range slots {
		slots[i] = i
	}
	slices.SortStableFunc(slots, func(a, b int) int {
		ax, _, _ := l.slot(a)
		bx, _, _ := l.slot(b)
		return cmp.Compare(ax, bx)
	})
	for j, w := range all {
		c := &p.courses[w.i]
		c.slot = slots[j]
		tx, ty, _ := l.slot(c.slot)
		tx += p.shift
		x, y := c.loiter(c.caught)
		c.rise = abs(y-ty) * haulRise
		c.haul = c.rise + (abs(tx-x)+strideX-1)/strideX
	}
	return p
}

// loiter is where the swimmer drifts to k steps after it turns up.
func (c course) loiter(k int) (x, y int) {
	drift := func(scale, axis float64, reach int) int {
		v := (valueNoise(float64(k)/scale, c.seed+axis)*2 - 1) * 1.6
		return int(math.Round(max(-1, min(1, v)) * float64(reach)))
	}
	return c.ax + drift(14, 0, wanderX), c.ay + drift(25, 500, wanderY)
}

// swimmerAt is where swimmer i is now: milling about, rising as the net is
// pursed, sliding across the bunt, or seated in its slot. A swimmer that got
// away from a full bunt mills about and is never seated.
func (m Model) swimmerAt(l layout, p plan, i int) (x, y int, seated bool) {
	c := p.courses[i]
	d := m.now.Sub(m.swimmers[i].launch)
	if d < 0 {
		return 0, 0, false
	}
	k := int(d / stepEvery)
	if c.slot < 0 || k < c.caught {
		x, y = c.loiter(k)
		return x, y, false
	}
	tx, ty, _ := l.slot(c.slot)
	tx += p.shift
	k -= c.caught
	if k >= c.haul {
		return tx, ty, true
	}
	x0, y0 := c.loiter(c.caught)
	if k < c.rise {
		return x0, y0 + (ty-y0)*k/c.rise, false
	}
	slid := min(abs(tx-x0), (k-c.rise)*strideX)
	if tx < x0 {
		slid = -slid
	}
	return x0 + slid, ty, false
}

// rig is where the boat and its net are at one moment.
type rig struct {
	boatX   int
	net     netBox
	sailing bool
}

func (m Model) rig(l layout, p plan) rig {
	t := m.sailed()
	r := rig{boatX: l.boatX(t), net: m.net(p)}
	r.sailing = m.pursedAt.IsZero() && m.revealAt.IsZero() && r.boatX != l.boatX(t-sailEvery)
	return r
}

// netBox is the net's edges at one moment: left and right are the columns of
// its ends, bottom the row of its leadline.
type netBox struct {
	left, right, bottom int
	fresh               bool // it closed within flashFor
}

// net is the seine now: paid out from the skiff to the stern while the boat
// sails, then pursed. The leadline rises a row every haulRise steps, just
// under the swimmers it lifts, and the ends draw in to the bunt.
func (m Model) net(p plan) netBox {
	if m.pursedAt.IsZero() {
		return p.set
	}
	k := int(m.now.Sub(m.pursedAt) / stepEvery)
	f := 1.0
	if p.purse > 0 {
		f = min(1, float64(k)/float64(p.purse))
	}
	toward := func(from, to int) int {
		return from + int(math.Round(float64(to-from)*f))
	}
	closed := m.now.Sub(m.pursedAt) - time.Duration(p.purse)*stepEvery
	return netBox{
		left:   toward(p.set.left, p.bunt.left),
		right:  toward(p.set.right, p.bunt.right),
		bottom: max(p.bunt.bottom, p.set.bottom-k/haulRise),
		fresh:  closed >= 0 && closed < flashFor,
	}
}

// View draws the intro. under is the app's own view; it is only read while
// Revealing.
func (m Model) View(under string) string {
	l, ok := m.layout()
	if !ok {
		return under
	}
	c := m.scene(l)
	if m.revealAt.IsZero() {
		return c.String()
	}
	p := float64(m.now.Sub(m.revealAt)) / float64(revealFor)
	return m.reveal(l, c, under, min(1, max(0, p)))
}

func (m Model) scene(l layout) canvas {
	c := newCanvas(l.w, l.h)
	p := m.plan(l)
	r := m.rig(l, p)
	drawNet(c, l, r.net)

	recent := model.ServiceType("")
	for i, w := range m.swimmers {
		if m.now.Before(w.launch) {
			continue // not here yet
		}
		recent = w.svc
		if _, _, seated := m.swimmerAt(l, p, i); !seated {
			continue
		}
		x, y, _ := l.slot(p.courses[i].slot)
		x += p.shift
		for row, cells := range w.sprite {
			c.put(x, y+row, cells, true)
		}
		c.text(x+l.iconW/2, y+l.iconH, styles.Count(w.count, w.failed).Render(countLabel(w)))
	}
	// Swimmers go over the net, so one milling past an end of it swims
	// across the wall.
	bubble := func(x, y int, ch string) {
		if y > l.netY && y < l.seabedY && x >= 0 && x < l.w && c.cells[y][x] == blank {
			c.cells[y][x] = styles.Bubble().Render(ch)
		}
	}
	for i, w := range m.swimmers {
		d := m.now.Sub(w.launch)
		x, y, seated := m.swimmerAt(l, p, i)
		if seated || d < 0 {
			continue
		}
		k := int(d / stepEvery)
		if cs := p.courses[i]; cs.slot < 0 || k < cs.caught {
			// Milling about: now and then a bubble rises off it.
			if rise := (k + int(w.seed)) % 24 / 4; rise < 3 {
				bubble(x+l.iconW/2+rise%2, y-1-rise, []string{"·", "°", "∘"}[rise])
			}
		} else {
			// Lifted: a trail of bubbles below.
			for b, ch := range []string{"°", "·"} {
				bubble(x+l.iconW/2-1+(k/3+b)%2, y+l.iconH+2*b, ch)
			}
		}
		for row, cells := range w.sprite {
			c.put(x, y+row, cells, true)
		}
	}

	c.put(0, l.seabedY, seabed(l.w), true)
	for row, cells := range m.logo {
		c.put(r.boatX, l.logoY+row, cells, false)
	}
	if r.sailing {
		m.drawWind(c, l, r.boatX)
	}
	c.put(r.net.left-ansi.StringWidth(skiff)/2, l.netY-1, splitCells(styles.Skiff().Render(skiff), ansi.StringWidth(skiff)), false)
	drawLine(c, l, r)

	title, anchor := m.title(recent)
	x := max(1, l.w/2-anchor/2)
	title = ansi.Truncate(title, l.w-1-x, "…")
	c.put(x, l.titleY, splitCells(title, ansi.StringWidth(title)), true)
	c.text(l.w/2, l.titleY+1, styles.MutedStyle().Render("press any key to skip"))
	return c
}

// drawNet draws the seine: a corkline of floats with netLabel set into it,
// the net's ends hanging from it, and the leadline of purse rings along the
// bottom.
func drawNet(c canvas, l layout, n netBox) {
	edge := styles.Net(n.fresh)
	inner := n.right - n.left - 1
	top := make([]string, inner+2)
	bottom := make([]string, inner+2)
	top[0], top[inner+1] = edge.Render("╭"), edge.Render("╮")
	bottom[0], bottom[inner+1] = edge.Render("╰"), edge.Render("╯")
	for x := 1; x <= inner; x++ {
		top[x], bottom[x] = edge.Render("─"), edge.Render("┄")
		if x%floatGap == 0 {
			top[x] = styles.Float().Render("•")
		}
		if x%ringGap == ringGap/2 {
			bottom[x] = edge.Render("o")
		}
	}
	label := " " + netLabel + " "
	if w := ansi.StringWidth(label); inner >= w+2 {
		copy(top[2:], splitCells(styles.PaneTitle(true).Render(label), w))
	}
	c.put(n.left, l.netY, top, true)
	side := []string{edge.Render("┊")}
	for y := l.netY + 1; y < n.bottom; y++ {
		c.put(n.left, y, side, true)
		c.put(n.right, y, side, true)
	}
	c.put(n.left, n.bottom, bottom, true)
}

// drawWind streams a few dashes off the logo's trailing edge while it sails.
func (m Model) drawWind(c canvas, l layout, boatX int) {
	phase := int(m.now.Sub(m.start) / (90 * time.Millisecond))
	wind := styles.Wind()
	for k, row := range []int{l.logoH / 4, l.logoH / 2, 3 * l.logoH / 4} {
		edge := slices.IndexFunc(m.logo[row], func(cell string) bool { return cell != blank })
		if edge < 0 {
			continue
		}
		y := l.logoY + row
		for j := range 3 + 2*(k%2) {
			x := boatX + edge - 2 - j
			// The gaps drift away from the boat, so the air streams past it.
			if ((j-phase-k)%3+3)%3 == 0 || x < 0 || c.cells[y][x] != blank {
				continue
			}
			c.cells[y][x] = wind.Render("─")
		}
	}
}

// drawLine draws the net's line in braille dots, from the boom at the
// boat's stern down to the net's near end, on cells nothing else uses.
func drawLine(c canvas, l layout, r rig) {
	dots := make([][]uint8, boomRows)
	for y := range dots {
		dots[y] = make([]uint8, l.w)
	}
	x0, x1 := 2*(r.boatX+l.logoW/8), 2*r.net.right
	h := 4 * boomRows
	for dy := range h {
		// Fill across to where the next dot row starts, so a shallow line
		// stays joined.
		a := x0 + (x1-x0)*dy/(h-1)
		b := x0 + (x1-x0)*min(dy+1, h-1)/(h-1)
		lo, hi := a, a
		if b > a {
			hi = b - 1
		} else if b < a {
			lo = b + 1
		}
		for x := lo; x <= hi; x++ {
			if x >= 0 && x/2 < l.w {
				dots[dy/4][x/2] |= brailleBit(x%2, dy%4)
			}
		}
	}
	rope := styles.Net(false)
	for y, row := range dots {
		for x, bits := range row {
			if bits != 0 && c.cells[l.lineY+y][x] == blank {
				c.cells[l.lineY+y][x] = rope.Render(string(rune(0x2800 + int(bits))))
			}
		}
	}
}

// brailleBit is the dot at column x (0–1) and row y (0–3) of a braille cell.
func brailleBit(x, y int) uint8 {
	if y == 3 {
		return 0x40 << x
	}
	return 1 << (y + 3*x)
}

// seabed is a row of uneven sand.
func seabed(w int) []string {
	sand := styles.Seabed()
	row := make([]string, w)
	for x := range row {
		switch n := lattice(x, 0x5EA); {
		case n < 0.55:
			row[x] = sand.Render("▁")
		case n < 0.85:
			row[x] = sand.Render("▂")
		default:
			row[x] = sand.Render("▃")
		}
	}
	return row
}

func countLabel(w swimmer) string {
	if w.failed && w.count == 0 {
		return "!"
	}
	return fmt.Sprint(w.count)
}

// title returns the status line and the width of its head, the part to
// center. The most recent service trails the head, so the head stays put
// while the label changes.
func (m Model) title(recent model.ServiceType) (string, int) {
	if m.loaded && !m.seatedAt.IsZero() {
		total := 0
		for _, w := range m.swimmers {
			total += w.count
		}
		s := styles.TitleStyle().Render(fmt.Sprintf("hauled in %d resources across %d services", total, len(m.swimmers)))
		return s, ansi.StringWidth(s)
	}
	frame := spinner[int(m.now.Sub(m.start)/(80*time.Millisecond))%len(spinner)]
	s := styles.Cursor().Render(frame) + " " + styles.TitleStyle().Render("catching "+m.project)
	head := ansi.StringWidth(s)
	if recent != "" {
		s += styles.MutedStyle().Render("  · " + recent.Label())
	}
	return s, head
}

// reveal blends scene into under along a fog front that starts at the logo
// and spreads outward in uneven patches as p goes from 0 to 1.
func (m Model) reveal(l layout, scene canvas, under string, p float64) string {
	lines := strings.Split(under, "\n")
	front := p * (1 + fogBand)
	cx := float64(l.boatX(m.sailed()) + l.logoW/2)
	cy := float64(l.logoY + l.logoH/2)
	far := 0.0
	for _, corner := range [][2]float64{{0, 0}, {float64(l.w), 0}, {0, float64(l.h)}, {float64(l.w), float64(l.h)}} {
		far = math.Max(far, math.Hypot(corner[0]-cx, (corner[1]-cy)*2))
	}

	out := make([]string, l.h)
	for y := range out {
		line := ""
		if y < len(lines) {
			line = lines[y]
		}
		var b strings.Builder
		appStart := -1
		flush := func(x int) {
			if appStart < 0 {
				return
			}
			seg := ansi.Cut(line, appStart, x)
			b.WriteString(seg)
			b.WriteString(strings.Repeat(" ", max(0, x-appStart-ansi.StringWidth(seg))))
			appStart = -1
		}
		head := -1 // column of the last two-column scene character written
		for x := 0; x < l.w; x++ {
			if scene.cells[y][x] == wideTail && head == x-1 {
				continue // the character at x-1 covers this column
			}
			d := math.Hypot(float64(x)-cx, (float64(y)-cy)*2) / far
			n := 0.75*d + 0.25*valueNoise(float64(x)/7, float64(y)/3.5)
			switch {
			case n < front-fogBand:
				if appStart < 0 {
					appStart = x
				}
				continue
			case n < front:
				flush(x)
				depth := (front - n) / fogBand // 0 at the scene edge, 1 at the app edge
				shade := fogShades[len(fogShades)-1-min(len(fogShades)-1, int(depth*float64(len(fogShades))))]
				b.WriteString(styles.Fog().Render(shade))
			default:
				flush(x)
				cell := scene.cells[y][x]
				switch {
				case cell == wideTail:
					// Its head went under the fog or the app.
					cell = blank
				case ansi.StringWidth(cell) == 2:
					head = x
				}
				b.WriteString(cell)
			}
		}
		flush(l.w)
		out[y] = b.String()
	}
	return strings.Join(out, "\n")
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
