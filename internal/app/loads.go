package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/messages"
)

// loadTimeout bounds a whole fan-out. The catalog also caps each provider.
const loadTimeout = 60 * time.Second

// streamBuffer lets providers report without waiting on the UI to read.
const streamBuffer = 64

var errNoCatalog = errors.New("no resource catalog")

// providerLoadedMsg is one provider's List result for a scope.
type providerLoadedMsg struct {
	scopeID string
	gen     uint64
	result  model.ProviderResult
	next    tea.Cmd // reads the next result
}

// scopeLoadDoneMsg follows the last providerLoadedMsg for a scope.
type scopeLoadDoneMsg struct {
	scopeID string
	gen     uint64
}

// relatedLoadedMsg is one provider's ListRelated result for a facet.
type relatedLoadedMsg struct {
	parentID string
	facetID  string
	gen      uint64
	result   model.ProviderResult
	next     tea.Cmd
}

// relatedDoneMsg follows the last relatedLoadedMsg for a facet.
type relatedDoneMsg struct {
	parentID string
	facetID  string
	gen      uint64
}

// hierarchyTimeout bounds the CRM tree build so a large org cannot hang the
// load forever.
const hierarchyTimeout = 30 * time.Second

// hierarchyLoadedMsg carries the CRM tree built by Catalog.Hierarchy.
type hierarchyLoadedMsg struct {
	root model.HierarchyNode
	err  error
}

// partialHierarchy returns msg's error when it came with a usable tree.
func partialHierarchy(msg hierarchyLoadedMsg) (model.ProviderError, bool) {
	var pe model.ProviderError
	if msg.root.ID == "" || !errors.As(msg.err, &pe) || !pe.Partial {
		return model.ProviderError{}, false
	}
	if pe.Message == "" {
		pe.Message = pe.Kind.Label()
	}
	return pe, true
}

// loadHierarchyCmd builds the CRM tree off the UI thread. The demo tree and an
// already-built live tree return immediately; a fresh live tree runs
// Resource Manager calls, bounded by hierarchyTimeout.
func (m Model) loadHierarchyCmd() tea.Cmd {
	src := m.src
	return func() tea.Msg {
		if src == nil {
			return hierarchyLoadedMsg{err: errNoCatalog}
		}
		ctx, cancel := context.WithTimeout(context.Background(), hierarchyTimeout)
		defer cancel()
		root, err := src.Hierarchy(ctx)
		return hierarchyLoadedMsg{root: root, err: err}
	}
}

// stream runs each in the background and returns the first message. Every
// message carries a command that reads the next; done is returned once each
// has returned and every result has been read. Once ctx is canceled,
// results nobody will read are dropped instead of blocking the providers.
func stream(ctx context.Context, each func(context.Context, func(model.ProviderResult)), wrap func(model.ProviderResult, tea.Cmd) tea.Msg, done tea.Msg) tea.Msg {
	ch := make(chan model.ProviderResult, streamBuffer)
	go func() {
		defer close(ch)
		tctx, cancel := context.WithTimeout(ctx, loadTimeout)
		defer cancel()
		each(tctx, func(r model.ProviderResult) {
			select {
			case ch <- r:
			case <-ctx.Done():
			}
		})
	}()
	return readStream(ch, wrap, done)()
}

func readStream(ch <-chan model.ProviderResult, wrap func(model.ProviderResult, tea.Cmd) tea.Msg, done tea.Msg) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return done
		}
		return wrap(r, readStream(ch, wrap, done))
	}
}

// startScope starts a scope load unless one is already in flight for it.
// refresh cancels a running load and starts again.
func (m *Model) startScope(scopeID string, refresh bool) tea.Cmd {
	if scopeID == "" {
		scopeID = m.scope.ID
	}
	if scopeID == "" {
		scopeID = model.ProjectScope(m.cfg.Project)
	}
	key := scopeKey(scopeID)
	ctx, gen, ok := m.outline.begin(key, refresh)
	if !ok {
		slog.Debug("load already in flight", "key", key.String())
		return nil
	}
	return m.loadCmd(ctx, gen, scopeID)
}

func (m Model) loadCmd(ctx context.Context, gen uint64, scopeID string) tea.Cmd {
	src := m.src
	done := scopeLoadDoneMsg{scopeID: scopeID, gen: gen}
	return func() tea.Msg {
		if src == nil {
			return providerLoadedMsg{
				scopeID: scopeID,
				gen:     gen,
				result:  model.ProviderResult{Err: errNoCatalog},
				next:    func() tea.Msg { return done },
			}
		}
		each := func(ctx context.Context, fn func(model.ProviderResult)) {
			src.ListEach(ctx, scopeID, fn)
		}
		return stream(ctx, each, func(r model.ProviderResult, next tea.Cmd) tea.Msg {
			return providerLoadedMsg{scopeID: scopeID, gen: gen, result: r, next: next}
		}, done)
	}
}

// pathToID is the chain from the tree root to id, falling back to a
// scope-aware lookup (a bare CLI project id) when PathTo misses.
func (m *Model) pathToID(id string) []model.HierarchyNode {
	path := m.tree.PathTo(id)
	if len(path) == 0 {
		if n, ok := m.tree.FindScope(id); ok {
			path = m.tree.PathTo(n.ID)
		}
	}
	return path
}

// loadPath starts a scope load for each node in path. When skipLoaded is
// set, a node that already has a cached result (its own load, or an earlier
// stub load under the same id) is left alone.
func (m *Model) loadPath(path []model.HierarchyNode, skipLoaded bool) tea.Cmd {
	if len(path) == 0 {
		return m.startScope(m.scope.ID, false)
	}
	var cmds []tea.Cmd
	for _, n := range path {
		if skipLoaded && m.outline.loaded(scopeKey(n.ID)) {
			continue
		}
		cmds = append(cmds, m.startScope(n.ID, false))
	}
	return tea.Batch(cmds...)
}

func (m *Model) startupLoads() tea.Cmd {
	if m.tree.ID == "" {
		return m.startScope(m.scope.ID, false)
	}
	return m.loadPath(m.pathToID(m.scope.ID), false)
}

func (m *Model) revealPath(id string) {
	for _, n := range m.pathToID(id) {
		m.outline.showKids[scopeKey(n.ID)] = true
	}
}

// applyHierarchy re-roots the explorer on the real CRM tree once it arrives,
// keeping the current scope selected when it still exists.
func (m *Model) applyHierarchy(root model.HierarchyNode) {
	m.tree = root
	m.crumbs = crumbCache{}
	if n, ok := root.Find(m.scope.ID); ok {
		m.scope = n
	} else if n, ok := root.FirstProject(); ok {
		m.scope = n
	} else {
		m.scope = root
	}
	m.revealPath(m.scope.ID)
	m.status.Project = m.scope.Name
	m.rebuildOutline()
}

// startRelated starts a facet load unless one is already in flight for the
// same key. refresh cancels a running load and starts again.
func (m *Model) startRelated(parent model.Resource, facet model.Facet, refresh bool) tea.Cmd {
	return m.startRelatedMode(parent, facet, refresh, false)
}

// startRelatedPrefetch loads an optional facet in the background when its
// parent resource is expanded, keeping the facet collapsed once loaded.
func (m *Model) startRelatedPrefetch(parent model.Resource, facet model.Facet) tea.Cmd {
	return m.startRelatedMode(parent, facet, false, true)
}

func (m *Model) startRelatedMode(parent model.Resource, facet model.Facet, refresh, prefetch bool) tea.Cmd {
	key := facetLoadKey(parent.ID, facet.ID)
	ctx, gen, ok := m.outline.begin(key, refresh)
	if !ok {
		slog.Debug("load already in flight", "key", key.String())
		return nil
	}
	m.outline.related[key] = relatedReq{parent: parent, facet: facet, prefetch: prefetch}
	return m.loadRelatedCmd(ctx, gen, parent, facet)
}

func (m Model) loadRelatedCmd(ctx context.Context, gen uint64, parent model.Resource, facet model.Facet) tea.Cmd {
	src := m.src
	done := relatedDoneMsg{parentID: parent.ID, facetID: facet.ID, gen: gen}
	return func() tea.Msg {
		if src == nil {
			return relatedLoadedMsg{
				parentID: parent.ID,
				facetID:  facet.ID,
				gen:      gen,
				result:   model.ProviderResult{Err: errNoCatalog},
				next:     func() tea.Msg { return done },
			}
		}
		each := func(ctx context.Context, fn func(model.ProviderResult)) {
			src.ListRelatedEach(ctx, parent, facet.ID, fn)
		}
		return stream(ctx, each, func(r model.ProviderResult, next tea.Cmd) tea.Msg {
			return relatedLoadedMsg{parentID: parent.ID, facetID: facet.ID, gen: gen, result: r, next: next}
		}, done)
	}
}

// applyLoaded merges one provider's result into a scope as it arrives.
func (m *Model) applyLoaded(scopeID string, res model.ProviderResult) {
	if res.Err != nil {
		slog.Warn("provider error", "scope", scopeID, "service", res.Service, "err", res.Err)
		if pe := model.AsProviderError(res.Service, res.Err); !m.noteAuth(pe) && !m.noteQuotaProject(pe) {
			m.logError(m.scopeName(scopeID), pe)
		}
	}
	key := scopeKey(scopeID)
	wasShown := m.outline.showKids[key]
	if !m.outline.loaded(key) {
		m.outline.scopes = append(m.outline.scopes, key)
	}
	itemsChanged, stateChanged := m.outline.merge(key, res)
	m.outline.showKids[key] = true
	if itemsChanged {
		m.rebuildCatalog()
	}
	if stateChanged || !wasShown {
		m.rebuildOutline()
	}
}

// finishLoad runs once every provider has reported for a scope.
func (m *Model) finishLoad(scopeID string) {
	key := scopeKey(scopeID)
	m.outline.end(key)
	errs := m.outline.finish(key)
	m.outline.showKids[key] = true
	m.rebuildOutline()
	if scopeID == m.scope.ID {
		m.nav.SelectKey(scopeID)
		m.syncDetails()
	}
	m.flashResult(errs, fmt.Sprintf("loaded %d resources", len(m.outline.items(key))))
}

// logf adds a message to the messages pane. An error also turns the pane
// on, since the pane is otherwise hidden.
func (m *Model) logf(level messages.Level, format string, args ...any) {
	m.messages.Add(level, fmt.Sprintf(format, args...), time.Now())
	if level == messages.Error && !m.showMessages {
		m.showMessages = true
		if m.width > 0 {
			m.syncLayout()
		}
	}
}

// logError adds a provider error for where (a scope, or a resource and
// facet) to the messages pane.
func (m *Model) logError(where string, e model.ProviderError) {
	what := e.Kind.Label()
	if e.Service != "" {
		what = e.Service.CollectionLabel()
	}
	// A partial list still shows what loaded, so it is a warning.
	level := messages.Error
	if e.Partial {
		level = messages.Warn
	}
	m.logf(level, "%s · %s: %s", where, what, e.Summary())
}

// noteAuth reports whether e is rejected credentials. The first such error
// raises the auth banner and adds one message; later ones only report true,
// so a sign-in failure is one message rather than one per service.
func (m *Model) noteAuth(e model.ProviderError) bool {
	if e.Kind != model.ErrKindUnauthenticated {
		return false
	}
	if m.authErr == nil {
		m.authErr = &e
		m.logf(messages.Error, "%s", m.identity.RejectedBanner(e.Summary()))
		m.syncLayout()
	}
	return true
}

// noteQuotaProject reports whether e is an API disabled in the caller's
// quota project. The first one per API adds a warning naming the project
// and the fix; later ones only report true, so the setup problem is one
// message rather than one per scope.
func (m *Model) noteQuotaProject(e model.ProviderError) bool {
	if e.Kind != model.ErrKindAPIDisabled || !e.QuotaProject {
		return false
	}
	if m.quotaAPIs == nil {
		m.quotaAPIs = map[string]bool{}
	}
	if !m.quotaAPIs[e.API] {
		m.quotaAPIs[e.API] = true
		// An impersonated service account bills its own project unless the
		// environment overrides it; user credentials bill their ADC quota
		// project.
		fix := "gcloud auth application-default set-quota-project PROJECT"
		if m.identity.Impersonate != "" {
			fix = "set GOOGLE_CLOUD_QUOTA_PROJECT=PROJECT"
		}
		m.logf(messages.Warn, "%s is not enabled in your quota project %s, so %s are hidden. Enable it there, or bill another project: %s",
			e.API, e.Project, e.Service.CollectionLabel(), fix)
	}
	return true
}

// rebuildCatalog flattens every loaded scope, in first-load order.
func (m *Model) rebuildCatalog() {
	var out []model.Resource
	for _, key := range m.outline.scopes {
		out = append(out, m.outline.items(key)...)
	}
	m.catalog = out
}

func (m *Model) applyRelated(msg relatedLoadedMsg) {
	key := facetLoadKey(msg.parentID, msg.facetID)
	if msg.result.Err != nil {
		slog.Warn("related error", "parent", msg.parentID, "service", msg.result.Service, "err", msg.result.Err)
		where := msg.parentID
		if req, ok := m.outline.related[key]; ok {
			where = req.parent.Name + " › " + req.facet.Label
		}
		if pe := model.AsProviderError(msg.result.Service, msg.result.Err); !m.noteAuth(pe) && !m.noteQuotaProject(pe) {
			m.logError(where, pe)
		}
	}
	wasShown := m.outline.showKids[key]
	_, stateChanged := m.outline.merge(key, msg.result)
	prefetch := m.outline.related[key].prefetch
	if !prefetch {
		m.outline.showKids[key] = true
	}
	if stateChanged || (!prefetch && !wasShown) {
		m.rebuildOutline()
	}
}

func (m *Model) finishRelated(msg relatedDoneMsg) {
	key := facetLoadKey(msg.parentID, msg.facetID)
	m.outline.end(key)
	errs := m.outline.finish(key)
	req, found := m.outline.related[key]
	if !req.prefetch {
		m.outline.showKids[key] = true
	}
	m.rebuildOutline()
	if req.prefetch && len(errs) == 0 {
		return
	}
	ok := fmt.Sprintf("loaded %d", len(m.outline.items(key)))
	if found && len(m.outline.items(key)) == 0 {
		ok = emptyFlash(req.facet)
	}
	m.flashResult(errs, ok)
}

// flashResult names the single error, counts several, or shows ok when
// there were none. The messages pane lists each error.
func (m *Model) flashResult(errs []model.ProviderError, ok string) {
	for _, e := range errs {
		if e.Kind == model.ErrKindUnauthenticated {
			m.status.SetFlash("auth: " + e.Summary())
			return
		}
	}
	switch len(errs) {
	case 0:
		m.status.SetFlash(ok)
	case 1:
		m.status.SetFlash(errs[0].Error())
	default:
		m.status.SetFlash(fmt.Sprintf("%d provider errors; see messages", len(errs)))
	}
}

// refresh cancels and restarts every loaded or loading scope and facet,
// bypassing the session cache (internal/gcp/cache) for each one, and retries
// the CRM tree if it failed to load. Collections and prefixes are never
// loads, so they are not keys here.
func (m *Model) refresh() tea.Cmd {
	cmd := m.refreshLoads()
	if m.hierFailed {
		return tea.Batch(cmd, m.loadHierarchyCmd())
	}
	return cmd
}

func (m *Model) refreshLoads() tea.Cmd {
	keys := map[loadKey]bool{}
	for key := range m.outline.cache {
		keys[key] = true
	}
	for key := range m.outline.inflight {
		keys[key] = true
	}
	inv, _ := m.src.(model.Invalidator)
	invalidated := map[string]bool{}
	invalidate := func(scopeID string) {
		if inv == nil || scopeID == "" || invalidated[scopeID] {
			return
		}
		invalidated[scopeID] = true
		inv.Invalidate(scopeID)
	}
	var cmds []tea.Cmd
	for key := range keys {
		switch {
		case key.isScope():
			invalidate(key.scope)
			cmds = append(cmds, m.startScope(key.scope, true))
		case key.isFacet():
			req, ok := m.outline.related[key]
			if !ok {
				continue
			}
			invalidate(req.parent.ScopeID)
			cmds = append(cmds, m.startRelated(req.parent, req.facet, true))
		}
	}
	if len(cmds) == 0 {
		invalidate(m.scope.ID)
		return m.startScope(m.scope.ID, true)
	}
	return tea.Batch(cmds...)
}
