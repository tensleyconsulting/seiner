package app

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/tensleyconsulting/seiner/internal/config"
	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/terminal"
	"github.com/tensleyconsulting/seiner/internal/ui/actions"
	"github.com/tensleyconsulting/seiner/internal/ui/details"
	"github.com/tensleyconsulting/seiner/internal/ui/header"
	"github.com/tensleyconsulting/seiner/internal/ui/help"
	"github.com/tensleyconsulting/seiner/internal/ui/icons"
	"github.com/tensleyconsulting/seiner/internal/ui/keys"
	"github.com/tensleyconsulting/seiner/internal/ui/messages"
	"github.com/tensleyconsulting/seiner/internal/ui/navigation"
	"github.com/tensleyconsulting/seiner/internal/ui/search"
	"github.com/tensleyconsulting/seiner/internal/ui/splash"
	"github.com/tensleyconsulting/seiner/internal/ui/status"
	"github.com/tensleyconsulting/seiner/internal/ui/styles"
	"github.com/tensleyconsulting/seiner/internal/ui/text"
)

// Pane is the focused region.
type Pane int

const (
	PaneNav Pane = iota
	PaneDetail
	PaneMessages
)

func (p Pane) String() string {
	switch p {
	case PaneNav:
		return "nav"
	case PaneDetail:
		return "detail"
	case PaneMessages:
		return "messages"
	default:
		return "unknown"
	}
}

// Model is the top-level Bubble Tea model.
type Model struct {
	cfg      config.Config
	width    int
	height   int
	focus    Pane
	showHelp bool
	showCopy bool
	keys     keys.Map
	caps     terminal.Capabilities
	icons    icons.Renderer

	src model.Catalog
	// iconsUploaded is set once the first Kitty upload ran. resendGen
	// numbers scheduled re-uploads; only the latest runs (scheduleResend).
	iconsUploaded bool
	resendGen     uint64
	catalog       []model.Resource
	tree          model.HierarchyNode
	scope         model.HierarchyNode
	// crumbs is the header breadcrumb for the selected scope, kept so a
	// frame does not search the tree. applyHierarchy clears it.
	crumbs crumbCache
	// hierFailed is set when the CRM tree failed to load or loaded
	// incomplete; refresh retries it.
	hierFailed bool
	// authErr is the first error Google returned for rejected live
	// credentials. It shows as a banner until refresh reads credentials
	// that work (reopen).
	authErr *model.ProviderError
	// reopen reads credentials again on refresh (WithReopen); reopening is
	// set while it runs.
	reopen    Reopener
	reopening bool
	// quotaAPIs are the APIs already reported off in the quota project, so
	// each is one message rather than one per scope.
	quotaAPIs map[string]bool

	header   header.Model
	nav      navigation.Model
	details  details.Model
	search   search.Model
	status   status.Model
	copyMenu actions.CopyModel
	copyRes  model.Resource
	identity model.Identity
	outline  outlineState
	messages messages.Model
	split    int
	// msgH is the messages pane height the user dragged to; 0 is the
	// default.
	msgH int
	// showMessages is whether the messages pane is on. It starts off and
	// an error turns it on.
	showMessages bool
	drag         dragTarget
	intro        splash.Model
	// startup is the first loads, registered in New so Init only returns it.
	startup tea.Cmd
}

// startScope is the CRM node the explorer starts on: the configured
// initial resource, else the project.
func startScope(cfg config.Config) model.HierarchyNode {
	if cfg.InitialResource != "" {
		return model.ScopeNode(cfg.InitialResource)
	}
	return model.ScopeNode(cfg.Project)
}

// New constructs the application. src is the live or demo catalog, or nil
// when there is none to browse (ident says why). ident is resolved in cmd;
// a credentials error is UI state, not a panic.
func New(cfg config.Config, version string, src model.Catalog, ident model.Identity) Model {
	caps := terminal.Detect(nil)
	renderer := icons.New(cfg.IconMode, cfg.IconsDir, caps)
	km := keys.Default()
	m := Model{
		cfg:      cfg,
		focus:    PaneNav,
		keys:     km,
		caps:     caps,
		icons:    renderer,
		src:      src,
		scope:    startScope(cfg),
		header:   header.New(renderer, "Seiner "+version),
		nav:      navigation.New(renderer),
		search:   search.New(km),
		details:  details.New(renderer, km),
		messages: messages.New(),
		status:   status.New(startScope(cfg).Name, ident.DisplayAccount(), renderer.Name(), caps.KittyGraphics, km),
		identity: ident,
		outline:  newOutline(),
	}
	m.outline.googleSAsDefault = cfg.GoogleServiceAccounts
	if cfg.DataSource != "" {
		m.status.Catalog = cfg.DataSource
	}
	// The real CRM tree arrives later via hierarchyLoadedMsg (see Init); the
	// explorer starts out re-rooted on just the selected project so startup
	// never waits on Resource Manager.
	if m.scope.ID != "" {
		m.tree = m.scope
		m.revealPath(m.scope.ID)
	}
	if src != nil {
		m.startup = m.startupLoads()
	}
	if m.tree.ID != "" {
		m.rebuildOutline()
	}
	if src != nil {
		m.status.SetFlash("loading…")
	}
	if cfg.Intro && renderer.Name() != "none" && src != nil {
		m.intro = splash.New(renderer, m.status.Project, time.Now())
	}
	if ident.Err != nil {
		slog.Warn("auth unavailable", "err", ident.Err, "impersonate", ident.Impersonate)
	}
	if ident.ADCProject != "" && m.scope.Kind == model.KindProject && ident.ADCProject != cfg.Project {
		slog.Info("adc project differs from scope",
			"adc_project", ident.ADCProject,
			"scope", cfg.Project,
		)
	}
	slog.Info("app constructed",
		"scope", m.scope.ID,
		"account", ident.DisplayAccount(),
		"auth_source", ident.Source,
		"icon_mode", cfg.IconMode,
		"renderer", renderer.Name(),
		"caps_kitty", caps.KittyGraphics,
		"multiplexer", caps.Multiplexer,
		"caps_color", caps.Color,
		"caps_truecolor", caps.TrueColor,
		"term", os.Getenv("TERM"),
		"term_program", os.Getenv("TERM_PROGRAM"),
		"kitty_window_id", os.Getenv("KITTY_WINDOW_ID") != "",
		"ghostty_resources", os.Getenv("GHOSTTY_RESOURCES_DIR") != "",
		"herdr_env", os.Getenv("HERDR_ENV") == "1",
		"debug", cfg.Debug,
		"intro", !m.intro.Done(),
	)
	return m
}

// ProgramOptions are the tea.Program options the model needs. Kitty icons
// carry their image ID in a 24-bit foreground (38;2;r;g;b); if Bubble Tea
// downsampled it (no COLORTERM over SSH, say) the ID would name another
// image or none, so the kitty renderer forces a TrueColor profile. icons.New
// already picked text when the user set NO_COLOR.
func (m Model) ProgramOptions() []tea.ProgramOption {
	if p, ok := m.colorProfile(); ok {
		slog.Info("forcing color profile", "profile", p.String(), "renderer", m.icons.Name(), "caps_truecolor", m.caps.TrueColor)
		return []tea.ProgramOption{tea.WithColorProfile(p)}
	}
	return nil
}

// colorProfile is the one place the program's color profile is decided. It
// returns false to let Bubble Tea detect the profile from the terminal and
// environment; today only Kitty icons override that, with TrueColor.
func (m Model) colorProfile() (colorprofile.Profile, bool) {
	if icons.Graphics(m.icons) {
		return colorprofile.TrueColor, true
	}
	return 0, false
}

// uploadIconsMsg runs a Kitty upload. A resend (gen > 0) forgets what was
// sent first, so every image goes again; only the latest scheduled resend
// runs.
type uploadIconsMsg struct{ gen uint64 }

// resendTrigger is what asks for a Kitty re-upload.
type resendTrigger int

const (
	// resendRefresh is the manual refresh key; it always resends.
	resendRefresh resendTrigger = iota
	// resendFocus and resendResize are what a tmux reattach looks like
	// (focus-in with focus-events on, and usually a new client size).
	resendFocus
	resendResize
)

func (t resendTrigger) String() string {
	switch t {
	case resendFocus:
		return "focus"
	case resendResize:
		return "resize"
	}
	return "refresh"
}

// scheduleResend re-uploads every Kitty image after icons.UploadDelay. The
// terminal can drop images the placeholders still name: a terminal reset,
// or a tmux reattach from another client. A full upload is about 1 MB, so
// focus and resize trigger it only under tmux (Kitty and Ghostty keep their
// images across both), and a burst of triggers (a window drag) uploads once,
// after the last. Before the first upload there is nothing to resend.
func (m *Model) scheduleResend(t resendTrigger) tea.Cmd {
	if !icons.Graphics(m.icons) || !m.iconsUploaded {
		return nil
	}
	if t != resendRefresh && m.caps.Multiplexer != terminal.MultiplexerTmux {
		return nil
	}
	m.resendGen++
	gen := m.resendGen
	slog.Debug("scheduling kitty re-upload", "trigger", t.String(), "delay", icons.UploadDelay)
	return tea.Tick(icons.UploadDelay, func(time.Time) tea.Msg {
		return uploadIconsMsg{gen: gen}
	})
}

func (m Model) Init() tea.Cmd {
	slog.Info("app started", "project", m.cfg.Project, "icons", m.icons.Name())
	// The theme follows the terminal's background; a terminal that does not
	// answer keeps the dark palette.
	cmds := []tea.Cmd{m.startup, m.intro.Init(), tea.RequestBackgroundColor}
	if m.src != nil {
		cmds = append(cmds, m.loadHierarchyCmd())
	}
	if m.icons != nil && m.icons.Name() == "kitty" {
		slog.Debug("scheduling kitty upload", "delay", icons.UploadDelay)
		cmds = append(cmds, tea.Tick(icons.UploadDelay, func(time.Time) tea.Msg {
			return uploadIconsMsg{}
		}))
	} else {
		slog.Debug("skipping kitty upload", "renderer", m.icons.Name())
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncLayout()
		m.intro.SetSize(msg.Width, msg.Height)
		cmd := m.scheduleResend(resendResize)
		return m, cmd
	case tea.FocusMsg:
		cmd := m.scheduleResend(resendFocus)
		return m, cmd
	case splash.FrameMsg:
		var cmd tea.Cmd
		m.intro, cmd = m.intro.Update(msg)
		return m, cmd
	case reopenedMsg:
		cmd := m.applyReopened(msg)
		return m, cmd
	case tea.BackgroundColorMsg:
		dark := msg.IsDark()
		slog.Info("terminal background", "color", fmt.Sprint(msg.Color), "dark", dark)
		styles.SetDark(dark)
		m.messages.Restyle()
		m.intro.Restyle()
		return m, nil
	case hierarchyLoadedMsg:
		if pe, ok := partialHierarchy(msg); ok {
			// Usable but missing ancestors: show it, warn, and let
			// refresh rebuild it.
			slog.Warn("hierarchy incomplete", "err", msg.err)
			m.hierFailed = true
			if !m.noteAuth(pe) {
				m.logf(messages.Warn, "Resource hierarchy may be incomplete: %s; refresh to retry", pe.Message)
			}
			m.applyHierarchy(msg.root)
			cmd := m.loadPath(m.pathToID(m.scope.ID), true)
			return m, cmd
		}
		if msg.err != nil {
			slog.Warn("hierarchy unavailable", "err", msg.err)
			m.hierFailed = true
			var pe model.ProviderError
			if errors.As(msg.err, &pe) && m.noteAuth(pe) {
				return m, nil
			}
			if pe.Kind == model.ErrKindPermissionDenied {
				m.status.SetFlash(pe.Error())
			}
			m.logf(messages.Warn, "Resource hierarchy unavailable: %v", msg.err)
			return m, nil
		}
		if m.hierFailed {
			m.logf(messages.Info, "Resource hierarchy loaded")
		}
		m.hierFailed = false
		m.applyHierarchy(msg.root)
		cmd := m.loadPath(m.pathToID(m.scope.ID), true)
		return m, cmd
	case uploadIconsMsg:
		resend := msg.gen != 0
		switch {
		case resend && msg.gen != m.resendGen:
			return m, nil // a later trigger scheduled another
		case resend:
			m.icons.Reset()
		case m.iconsUploaded:
			slog.Debug("kitty upload already done")
			return m, nil
		}
		m.iconsUploaded = true
		seq := m.icons.Upload()
		if seq == "" {
			slog.Warn("kitty upload produced no payload")
			return m, nil
		}
		slog.Info("kitty graphics uploaded",
			"resend", resend,
			"bytes", len(seq),
			"has_at", strings.Contains(seq, "a=t"),
			"has_U1", strings.Contains(seq, "U=1"),
		)
		if m.cfg.Debug {
			m.status.SetFlash(fmt.Sprintf("kitty: uploaded %d bytes", len(seq)))
		}
		return m, tea.Raw(seq)
	case providerLoadedMsg:
		if !m.outline.current(scopeKey(msg.scopeID), msg.gen) {
			return m, nil
		}
		m.applyLoaded(msg.scopeID, msg.result)
		if msg.result.Service != "" {
			m.intro.Arrive(msg.result.Service, len(msg.result.Resources), msg.result.Err)
		}
		return m, msg.next
	case scopeLoadDoneMsg:
		if !m.outline.current(scopeKey(msg.scopeID), msg.gen) {
			return m, nil
		}
		m.finishLoad(msg.scopeID)
		if msg.scopeID == m.scope.ID {
			m.intro.Loaded()
		}
		return m, nil
	case relatedLoadedMsg:
		if !m.outline.current(facetLoadKey(msg.parentID, msg.facetID), msg.gen) {
			return m, nil
		}
		m.applyRelated(msg)
		return m, msg.next
	case relatedDoneMsg:
		if !m.outline.current(facetLoadKey(msg.parentID, msg.facetID), msg.gen) {
			return m, nil
		}
		return m, m.finishRelated(msg)
	case tea.MouseClickMsg:
		if !m.intro.Done() {
			m.intro.Skip()
			return m, nil
		}
		return m.handleMouseClick(msg)
	case tea.MouseMotionMsg:
		return m.handleMouseMotion(msg)
	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg)
	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.PasteMsg:
		if m.search.Paste(msg.Content) {
			m.rebuildOutline()
			m.syncLayout()
		}
		return m, nil
	}
	return m, nil
}

// quit cancels every running load and ends the program. Kitty images are
// freed first, while the alt screen they were uploaded on is still active.
func (m *Model) quit() tea.Cmd {
	m.outline.cancelAll()
	if seq := m.icons.Delete(); seq != "" {
		slog.Debug("kitty images deleted on quit", "bytes", len(seq))
		return tea.Sequence(tea.Raw(seq), tea.Quit)
	}
	return tea.Quit
}

// paneLayout is the current pane layout.
func (m Model) paneLayout() paneSizes {
	return allocatePanes(m.width, m.height, m.split, m.msgH, m.showMessages, layoutMode(m.width), m.search.Active || m.search.Query != "", len(m.banners()))
}

func (m *Model) syncLayout() {
	if m.split > 0 {
		m.split = clampSplit(m.width, m.split)
	}
	sizes := m.paneLayout()
	if sizes.msgH == 0 && m.focus == PaneMessages {
		m.focus = PaneDetail
	}
	m.nav.SetSize(sizes.navW, sizes.bodyH)
	detailW := sizes.detailW
	if detailW == 0 {
		detailW = m.width
	}
	m.details.SetSize(detailW, sizes.detailH)
	m.messages.SetSize(detailW, sizes.msgH)
	m.status.SetWidth(m.width)
	m.header.SetWidth(m.width)

	m.nav.SetFocused(m.focus == PaneNav)
	m.details.SetFocused(m.focus == PaneDetail)
	m.messages.SetFocused(m.focus == PaneMessages)
}

// rightColumn is details over the messages pane.
func (m Model) rightColumn() string {
	det := m.details.View()
	if m.paneLayout().msgH == 0 {
		return det
	}
	return lipgloss.JoinVertical(lipgloss.Left, det, m.messages.View())
}

func (m Model) View() tea.View {
	var content string
	if m.width == 0 {
		content = "loading…"
	} else if !m.intro.Done() {
		var under string
		if m.intro.Revealing() {
			under = m.render()
		}
		content = m.intro.View(under)
	} else if m.showHelp {
		content = help.View(m.width, m.height, m.keys)
	} else if m.showCopy {
		content = m.copyMenu.View(m.width, m.height, m.copyRes)
	} else {
		content = m.render()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "Seiner"
	// Under tmux, focus regain (a reattach, with focus-events on) re-uploads
	// Kitty images (scheduleResend). Nothing else needs focus events.
	v.ReportFocus = icons.Graphics(m.icons) && m.caps.Multiplexer == terminal.MultiplexerTmux
	if !m.showHelp && !m.showCopy {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

// banners are the persistent lines between the header and the body: the
// auth warning, then the informational notice. Layout and mouse hit-testing
// count them, so every line here must be one terminal row.
func (m Model) banners() []string {
	var out []string
	if b := m.identity.Banner(); b != "" {
		out = append(out, styles.Banner().Width(m.width).Render(text.Truncate(b, m.width-2)))
	} else if m.authErr != nil {
		b := m.identity.RejectedBanner(m.authErr.Summary())
		out = append(out, styles.Banner().Width(m.width).Render(text.Truncate(b, m.width-2)))
	}
	if n := m.identity.Notice; n != "" {
		out = append(out, styles.Notice().Width(m.width).Render(text.Truncate(n, m.width-2)))
	}
	return out
}

func (m Model) render() string {
	hdr := m.header
	hdr.Crumb = m.crumb()
	hdr.ID = m.selectedID()
	hdr.Account = m.status.Account
	header := hdr.View()

	mode := layoutMode(m.width)
	var body string
	switch {
	case mode == LayoutSmall:
		if m.focus == PaneNav {
			body = m.nav.View()
		} else {
			body = m.rightColumn()
		}
	default:
		nav := m.nav.View()
		det := m.rightColumn()
		h := max(lipgloss.Height(nav), lipgloss.Height(det))
		nav = lipgloss.NewStyle().Height(h).MaxHeight(h).Render(nav)
		det = lipgloss.NewStyle().Height(h).MaxHeight(h).Render(det)
		body = lipgloss.JoinHorizontal(lipgloss.Top, nav, det)
	}

	footer := m.status.View()
	searchLine := m.search.View(m.width)
	parts := append([]string{header}, m.banners()...)
	parts = append(parts, body)
	if searchLine != "" {
		parts = append(parts, searchLine)
	}
	parts = append(parts, footer)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
