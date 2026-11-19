package gcp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/assuredworkloads"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/crm"
	"github.com/tensleyconsulting/seiner/internal/gcp/cache"
	"github.com/tensleyconsulting/seiner/internal/gcp/fake"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// maxConcurrentProviders bounds how many provider calls one fan-out runs at once.
const maxConcurrentProviders = 8

// providerTimeout caps a single provider call so one hung API cannot pin the
// fan-out. It is a variable so tests can lower it.
var providerTimeout = 20 * time.Second

// listCacheTTL bounds how long a provider's List/ListRelated result is reused
// without calling it again. A manual refresh bypasses it via Invalidate.
const listCacheTTL = 60 * time.Second

// Catalog aggregates ResourceProviders. It is the only type cmd should pass into the UI.
type Catalog struct {
	providers []model.ResourceProvider
	hier      *hierarchySource
	// cache holds successful per-provider list results, keyed by scope or
	// facet plus service. It is a pointer so value copies of Catalog share
	// one cache.
	cache *cache.Cache[model.ProviderResult]
	// perms checks providers' ListPermissions before List; nil checks
	// nothing.
	perms model.PermissionTester
	// quotaProject is the project client-based API calls are billed to,
	// by id; "" when unknown.
	quotaProject string
	// invalidators are adapter-level caches Invalidate also drops.
	invalidators []model.Invalidator
	// clients closes the live clients built so far; nil for the fixture
	// catalog.
	clients io.Closer
}

// NewCatalog fans out across providers in the given order.
func NewCatalog(providers ...model.ResourceProvider) Catalog {
	return Catalog{
		providers: append([]model.ResourceProvider(nil), providers...),
		cache:     cache.New[model.ProviderResult](listCacheTTL),
	}
}

// hierarchySource lazily builds the CRM tree and caches a complete result,
// so every value copy of a Catalog built from the same
// WithHierarchy/WithLiveHierarchy call shares one build. A partial tree is
// returned but not cached.
type hierarchySource struct {
	// sem holds one token while a build runs. It is a channel, not a
	// mutex, so a caller waiting on another's build gives up when its own
	// ctx ends.
	sem   chan struct{}
	api   crm.API // nil when tree is already prebuilt
	tree  model.HierarchyNode
	built bool
	// aw marks Assured Workload folders in a live tree, listed at
	// awLocations. nil leaves folders plain.
	aw          assuredworkloads.API
	awLocations []string
	// snap is the tree once built, readable without waiting on mu while
	// a build runs.
	snap atomic.Pointer[model.HierarchyNode]
}

// snapshot returns the built tree, or false before the first build.
func (h *hierarchySource) snapshot() (model.HierarchyNode, bool) {
	if h == nil {
		return model.HierarchyNode{}, false
	}
	t := h.snap.Load()
	if t == nil {
		return model.HierarchyNode{}, false
	}
	return *t, true
}

func (h *hierarchySource) get(ctx context.Context) (model.HierarchyNode, error) {
	select {
	case h.sem <- struct{}{}:
	case <-ctx.Done():
		return model.HierarchyNode{}, ctx.Err()
	}
	defer func() { <-h.sem }()
	if h.built {
		return h.tree, nil
	}
	tree, err := crm.Build(ctx, h.api, expectedLookupError)
	partial := model.IsPartial(err)
	if err != nil && !partial {
		return model.HierarchyNode{}, providerError(model.ServiceOrganization, err)
	}
	tree = assuredworkloads.Annotate(ctx, h.aw, tree, h.awLocations)
	// Lists cut short by ctx would cache a tree missing workloads.
	if err := ctx.Err(); err != nil {
		return model.HierarchyNode{}, err
	}
	if partial {
		// Kept for this caller but not cached, so the next call (the
		// app's refresh) rebuilds it.
		pe := providerError(model.ServiceOrganization, err)
		pe.Partial = true
		return tree, pe
	}
	h.tree = tree
	h.built = true
	h.snap.Store(&tree)
	return tree, nil
}

// expectedLookupError reports whether a failed ancestor read is the usual
// missing permission (or a node gone since the search), which leaves a
// quiet stub, rather than an outage that leaves the tree incomplete.
func expectedLookupError(err error) bool {
	switch kind, _ := Classify(err); kind {
	case model.ErrKindPermissionDenied, model.ErrKindNotFound:
		return true
	}
	return false
}

// WithHierarchy attaches a prebuilt CRM tree (fixtures, or the live
// single-project stub). The original catalog is not modified.
func (c Catalog) WithHierarchy(root model.HierarchyNode) Catalog {
	c.hier = &hierarchySource{sem: make(chan struct{}, 1), tree: root, built: true}
	c.hier.snap.Store(&root)
	return c
}

// WithLiveHierarchy attaches a Resource Manager API. The tree is built on the
// first Hierarchy call and cached after. The original catalog is not
// modified.
func (c Catalog) WithLiveHierarchy(api crm.API) Catalog {
	c.hier = &hierarchySource{sem: make(chan struct{}, 1), api: api}
	return c
}

// WithAssuredWorkloads lists Assured Workloads at locations when the live
// tree is built and marks the folders they own. It must follow
// WithLiveHierarchy; a prebuilt tree is left as is. The original catalog is
// not modified.
func (c Catalog) WithAssuredWorkloads(api assuredworkloads.API, locations []string) Catalog {
	if c.hier == nil || c.hier.built || api == nil || len(locations) == 0 {
		return c
	}
	c.hier = &hierarchySource{sem: make(chan struct{}, 1), api: c.hier.api, aw: api, awLocations: slices.Clone(locations)}
	return c
}

// WithPermissionTester checks each scope's declared list permissions
// (model.PermissionDeclarer) before listing, and skips providers missing
// one. The original catalog is not modified.
func (c Catalog) WithPermissionTester(t model.PermissionTester) Catalog {
	c.perms = t
	return c
}

// WithQuotaProject names the project client-based API calls are billed
// to, so an API-disabled error naming it reads as a setup problem rather
// than the listed scope's. The original catalog is not modified.
func (c Catalog) WithQuotaProject(project string) Catalog {
	c.quotaProject = project
	return c
}

// WithInvalidators makes Invalidate also drop the adapter-level caches of
// invs (such as the shared Compute list memo), so a manual refresh reaches
// the API. The original catalog is not modified.
func (c Catalog) WithInvalidators(invs ...model.Invalidator) Catalog {
	c.invalidators = append(slices.Clip(c.invalidators), invs...)
	return c
}

// withClients makes Close close clients. The original catalog is not
// modified.
func (c Catalog) withClients(clients io.Closer) Catalog {
	c.clients = clients
	return c
}

// Close closes every Google client the catalog built, once each; it builds
// none. Value copies share the clients, so close one copy, after the last
// use of any. The fixture and zero catalogs have nothing to close.
func (c Catalog) Close() error {
	if c.clients == nil {
		return nil
	}
	return c.clients.Close()
}

// FakeCatalog returns an in-memory catalog of fixture providers. It never
// calls Google.
func FakeCatalog() Catalog {
	return NewCatalog(append(fake.Providers(), fake.EnabledAPIs())...).
		WithHierarchy(fake.Hierarchy()).
		WithPermissionTester(fake.Permissions())
}

func (c Catalog) Hierarchy(ctx context.Context) (model.HierarchyNode, error) {
	if err := ctx.Err(); err != nil {
		return model.HierarchyNode{}, err
	}
	if c.hier == nil {
		return model.HierarchyNode{}, errors.New("no resource hierarchy")
	}
	tree, err := c.hier.get(ctx)
	if err != nil && tree.ID == "" {
		return model.HierarchyNode{}, err
	}
	if tree.ID == "" {
		return model.HierarchyNode{}, errors.New("no resource hierarchy")
	}
	// A partial tree comes with its error.
	return tree, err
}

func (c Catalog) ListEach(ctx context.Context, scopeID string, fn func(model.ProviderResult)) {
	enabled := c.enabledSet(ctx, scopeID)
	// Checked on the first cache miss, so a scope served from the cache
	// makes no call.
	granted := sync.OnceValue(func() map[string]bool { return c.grantedSet(ctx, scopeID, enabled) })
	c.fanOut(ctx, fn, func(svc model.ServiceType) string {
		return listCacheKey(scopeID, svc)
	}, func(ctx context.Context, p model.ResourceProvider) (listed, bool) {
		if gated(p.Service(), enabled) {
			return listed{}, true
		}
		if missing := missingPermissions(p, scopeID, granted()); len(missing) > 0 {
			return listed{missing: missing}, true
		}
		items, err := p.List(ctx, scopeID)
		if err != nil {
			switch c.disabledIn(scopeID, p.Service(), err) {
			case inScope:
				// The API is off in this project: like a gated provider,
				// list nothing and report no error.
				slog.Debug("api disabled in scope; listing nothing", "scope", scopeID, "service", p.Service())
				return listed{}, true
			case inQuotaProject:
				return listed{err: err, quotaProject: true}, true
			}
		}
		return listed{items: items, err: err}, true
	})
}

func (c Catalog) ListRelatedEach(ctx context.Context, parent model.Resource, facetID string, fn func(model.ProviderResult)) {
	c.fanOut(ctx, fn, func(svc model.ServiceType) string {
		return relatedCacheKey(parent.ID, facetID, svc)
	}, func(ctx context.Context, p model.ResourceProvider) (listed, bool) {
		rl, ok := p.(model.RelatedLister)
		if !ok {
			return listed{}, false
		}
		items, truncated, err := rl.ListRelated(ctx, parent, facetID)
		return listed{items: items, truncated: truncated, err: err}, true
	})
}

// Invalidate drops every cached list result under scopeID: its own scope-level
// entries, and the facet entries of resources within it (their keys start
// with the resource's full name, which in turn starts with its scope's).
// It matches on a separator so projects/p leaves projects/p2 cached. It
// then passes scopeID to each WithInvalidators cache, and for a project
// scope drops the project's cached enabled-API set, so an API enabled since
// the last lookup ungates its provider.
func (c Catalog) Invalidate(scopeID string) {
	c.cache.DeletePrefix(scopeID + "|")
	c.cache.DeletePrefix(scopeID + "/")
	for _, inv := range c.invalidators {
		inv.Invalidate(scopeID)
	}
	if project, ok := model.ProjectID(scopeID); ok {
		for _, p := range c.providers {
			if inv, ok := p.(enabledInvalidator); ok {
				inv.InvalidateEnabled(project)
			}
		}
	}
}

// enabledInvalidator is an EnabledLister that caches each project's enabled
// set (Service Usage) and can drop one on refresh.
type enabledInvalidator interface {
	InvalidateEnabled(project string)
}

func listCacheKey(scopeID string, svc model.ServiceType) string {
	return scopeID + "|" + string(svc)
}

func relatedCacheKey(parentID, facetID string, svc model.ServiceType) string {
	return parentID + "|" + facetID + "|" + string(svc)
}

// enabledSet returns the enabled API names for a project scope, or nil when
// the scope should not be gated: org and folder scopes, no EnabledLister
// among the providers, or a failed lookup (fail open).
func (c Catalog) enabledSet(ctx context.Context, scopeID string) map[string]bool {
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil
	}
	var lister model.EnabledLister
	for _, p := range c.providers {
		if l, ok := p.(model.EnabledLister); ok {
			lister = l
			break
		}
	}
	if lister == nil {
		return nil
	}
	lctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	apis, err := lister.EnabledAPIs(lctx, project)
	if err != nil {
		slog.Warn("enabled APIs unavailable; not gating providers", "project", project, "err", err)
		return nil
	}
	set := make(map[string]bool, len(apis))
	for _, api := range apis {
		set[api] = true
	}
	return set
}

// gated reports whether svc's List should be skipped because its API is not
// in enabled. A nil enabled set gates nothing.
func gated(svc model.ServiceType, enabled map[string]bool) bool {
	if enabled == nil {
		return false
	}
	spec, ok := model.Spec(svc)
	return ok && spec.Gate && spec.API != "" && !enabled[spec.API]
}

// grantedSet returns which of the providers' declared list permissions the
// caller holds on scopeID, or nil when nothing should be gated: no tester,
// nothing declared, or a failed check (fail open). Providers gated off by
// enabled are not asked.
func (c Catalog) grantedSet(ctx context.Context, scopeID string, enabled map[string]bool) map[string]bool {
	if c.perms == nil {
		return nil
	}
	var want []string
	for _, p := range c.providers {
		if d, ok := p.(model.PermissionDeclarer); ok && !gated(p.Service(), enabled) {
			want = append(want, d.ListPermissions(scopeID)...)
		}
	}
	slices.Sort(want)
	want = slices.Compact(want)
	if len(want) == 0 {
		return nil
	}
	tctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	got, err := c.perms.TestPermissions(tctx, scopeID, want)
	if err != nil {
		slog.Warn("permission check failed; not gating providers", "scope", scopeID, "err", err)
		return nil
	}
	set := make(map[string]bool, len(got))
	for _, perm := range got {
		set[perm] = true
	}
	slog.Debug("permission check", "scope", scopeID, "tested", len(want), "granted", len(set))
	return set
}

// missingPermissions lists p's declared permissions on scopeID that granted
// lacks. A nil granted gates nothing.
func missingPermissions(p model.ResourceProvider, scopeID string, granted map[string]bool) []string {
	d, ok := p.(model.PermissionDeclarer)
	if !ok || granted == nil {
		return nil
	}
	var out []string
	for _, perm := range d.ListPermissions(scopeID) {
		if !granted[perm] {
			out = append(out, perm)
		}
	}
	return out
}

// listed is one provider call's raw outcome.
type listed struct {
	items     []model.Resource
	truncated bool
	err       error
	missing   []string // the provider was skipped for lack of these
	// quotaProject marks err as an API disabled in the quota project.
	quotaProject bool
}

// disabledWhere says which project an API-disabled error is about.
type disabledWhere int

const (
	notDisabled    disabledWhere = iota // another error, or a project it cannot place
	inScope                             // the listed project: the API is off there
	inQuotaProject                      // the caller's quota project: a setup problem
)

// disabledIn places an API-disabled error from listing svc in scopeID by
// the project Google says it checked, matched by id or number. A
// client-based API is checked in the quota project, so when the two are
// the same project its error is the quota project's.
func (c Catalog) disabledIn(scopeID string, svc model.ServiceType, err error) disabledWhere {
	pe := providerError(svc, err)
	if pe.Kind != model.ErrKindAPIDisabled {
		return notDisabled
	}
	if pe.Project == "" {
		if isRestrictedError(err) {
			return inScope
		}
		return notDisabled
	}
	quota := c.quotaProject != "" && c.isProject(c.quotaProject, pe.Project)
	spec, _ := model.Spec(svc)
	if project, ok := model.ProjectID(scopeID); ok && c.isProject(project, pe.Project) && !(spec.ClientBased && quota) {
		return inScope
	}
	if quota {
		return inQuotaProject
	}
	return notDisabled
}

// isProject reports whether named, a project id or number from an error,
// is the project with id. The number comes from the built tree, so before
// it is built only the id matches.
func (c Catalog) isProject(id, named string) bool {
	if named == id {
		return true
	}
	tree, ok := c.hier.snapshot()
	if !ok {
		return false
	}
	n, ok := tree.FindScope(id)
	return ok && n.Number != "" && n.Number == named
}

// listFunc calls one provider. ok is false when the provider does not apply
// and should not report at all.
type listFunc func(ctx context.Context, p model.ResourceProvider) (out listed, ok bool)

// keyFunc returns the session cache key for a provider's service, used to
// skip a repeat call within listCacheTTL.
type keyFunc func(svc model.ServiceType) string

func (c Catalog) fanOut(ctx context.Context, fn func(model.ProviderResult), key keyFunc, call listFunc) {
	var g errgroup.Group
	g.SetLimit(maxConcurrentProviders)
	for _, p := range c.providers {
		g.Go(func() error {
			if err := ctx.Err(); err != nil {
				fn(model.ProviderResult{Service: p.Service(), Err: providerError(p.Service(), err)})
				return nil
			}
			k := key(p.Service())
			if cached, ok := c.cache.Get(k); ok {
				fn(cached)
				return nil
			}
			pctx, cancel := context.WithTimeout(ctx, providerTimeout)
			defer cancel()
			start := time.Now()
			out, ok := call(pctx, p)
			if !ok {
				return nil
			}
			slog.Debug("provider call", "service", p.Service(), "key", k, "elapsed", time.Since(start).Round(time.Millisecond),
				"items", len(out.items), "truncated", out.truncated, "err", out.err, "missing", out.missing)
			res := normalized(p.Service(), out)
			if res.Err == nil {
				c.cache.Set(k, res)
			}
			fn(res)
			return nil
		})
	}
	_ = g.Wait()
}

// normalized applies Normalize and Validate. Invalid items are dropped and
// reported as the result's Err; valid items are kept. A model.Partial error
// keeps the items too and is reported with Partial set. A non-nil Err is
// always a classified model.ProviderError.
func normalized(svc model.ServiceType, out listed) model.ProviderResult {
	res := model.ProviderResult{Service: svc, Missing: out.missing}
	partial := model.IsPartial(out.err)
	if out.err != nil && !partial {
		pe := providerError(svc, out.err)
		pe.QuotaProject = out.quotaProject
		res.Err = pe
		return res
	}
	res.Truncated = out.truncated
	var invalid []error
	for _, r := range out.items {
		r = model.Normalize(r)
		if vErr := r.Validate(); vErr != nil {
			invalid = append(invalid, vErr)
			continue
		}
		res.Resources = append(res.Resources, r)
	}
	model.UniqueBindingIDs(res.Resources)
	switch {
	case partial:
		pe := providerError(svc, errors.Join(append([]error{out.err}, invalid...)...))
		pe.Partial = true
		res.Err = pe
	case len(invalid) > 0:
		res.Err = providerError(svc, errors.Join(invalid...))
	}
	return res
}

var _ model.Catalog = Catalog{}
var _ model.Invalidator = Catalog{}
