package app

import (
	"context"
	"fmt"
	"slices"

	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/resources"
)

// loadKey identifies a node's children in the outline. A scope sets scope;
// a facet load (including a project's enabled services) sets parent and
// facet. Nodes that only expand, like collections and prefixes, set parent
// alone, so they are neither scope nor facet loads.
type loadKey struct{ scope, parent, facet string }

func scopeKey(id string) loadKey { return loadKey{scope: id} }

func facetLoadKey(parentID, facetID string) loadKey {
	return loadKey{parent: parentID, facet: facetID}
}

// nodeKey is the key that expands r in the explorer.
func nodeKey(r model.Resource) loadKey {
	switch {
	case r.IsScope():
		return scopeKey(r.ID)
	case r.IsEnabledGroup():
		return facetLoadKey(r.ID, model.FacetEnabledServices)
	}
	return loadKey{parent: r.ID}
}

// openKey is the key that expands row. A row listed under several parents
// (a role under each principal holding it) has its own, so each opens on
// its own; for any other row it is nodeKey.
func openKey(row resources.Row) loadKey {
	if row.Via != "" {
		return loadKey{parent: row.Key()}
	}
	return nodeKey(row.Resource)
}

func (k loadKey) isScope() bool { return k.scope != "" }
func (k loadKey) isFacet() bool { return k.facet != "" }

// String is for logging only.
func (k loadKey) String() string {
	switch {
	case k.isScope():
		return k.scope
	case k.isFacet():
		return k.parent + "#" + k.facet
	}
	return k.parent
}

// facetRowKey is the explorer row key of a facet, as resources.Row.Key
// builds it.
func facetRowKey(parentID, facetID string) string {
	return parentID + "\x1f" + facetID
}

// inflightLoad is one running load. gen tags its messages so a canceled
// load's late results are dropped.
type inflightLoad struct {
	ctx    context.Context
	cancel context.CancelFunc
	gen    uint64
}

// relatedReq is what a facet load needs to run again on refresh.
type relatedReq struct {
	parent   model.Resource
	facet    model.Facet
	prefetch bool
}

type outlineState struct {
	showFacets map[string]bool
	showKids   map[loadKey]bool
	// cache holds loaded children per scope or facet key, split by the
	// provider that reported them so each arrival replaces only its own.
	cache map[loadKey]map[model.ServiceType][]model.Resource
	// complete marks keys whose load has heard from every provider.
	complete map[loadKey]bool
	// errs holds the latest provider errors per key, keyed like cache. A
	// provider's next report for the key replaces its entry.
	errs map[loadKey][]model.ProviderError
	// truncated marks the providers whose latest report for a key stopped
	// at a limit, keyed like cache.
	truncated map[loadKey]map[model.ServiceType]bool
	// missing holds, per scope key, the permissions each provider was
	// skipped for. A provider's next report for the key replaces its entry.
	missing map[loadKey]map[model.ServiceType][]string
	// scopes is the order scope keys first loaded; Model.catalog follows it.
	scopes []loadKey
	// inflight holds at most one running load per key.
	inflight map[loadKey]inflightLoad
	// related remembers each facet load's request for refresh.
	related map[loadKey]relatedReq
	// gen is the last generation handed to a load.
	gen uint64
	// byPrincipal marks Role Bindings collections, by ID, that list
	// principals with their roles instead of roles with their members.
	byPrincipal map[string]bool
	// googleSAs marks Service Accounts collections, by ID, that show the
	// accounts Google created (defaults and service agents). A collection
	// not toggled yet follows googleSAsDefault.
	googleSAs        map[string]bool
	googleSAsDefault bool
}

func newOutline() outlineState {
	return outlineState{
		showFacets:  map[string]bool{},
		showKids:    map[loadKey]bool{},
		cache:       map[loadKey]map[model.ServiceType][]model.Resource{},
		complete:    map[loadKey]bool{},
		errs:        map[loadKey][]model.ProviderError{},
		truncated:   map[loadKey]map[model.ServiceType]bool{},
		missing:     map[loadKey]map[model.ServiceType][]string{},
		inflight:    map[loadKey]inflightLoad{},
		related:     map[loadKey]relatedReq{},
		byPrincipal: map[string]bool{},
		googleSAs:   map[string]bool{},
	}
}

// begin registers a load for key and returns its context and generation.
// It returns false when key is already in flight, unless refresh is set, in
// which case the running load is canceled and replaced.
func (o *outlineState) begin(key loadKey, refresh bool) (context.Context, uint64, bool) {
	if cur, ok := o.inflight[key]; ok {
		if !refresh {
			return nil, 0, false
		}
		cur.cancel()
	}
	o.gen++
	ctx, cancel := context.WithCancel(context.Background())
	o.inflight[key] = inflightLoad{ctx: ctx, cancel: cancel, gen: o.gen}
	return ctx, o.gen, true
}

// current reports whether gen is the running load for key.
func (o outlineState) current(key loadKey, gen uint64) bool {
	cur, ok := o.inflight[key]
	return ok && cur.gen == gen
}

// end releases key's running load.
func (o *outlineState) end(key loadKey) {
	if cur, ok := o.inflight[key]; ok {
		cur.cancel()
		delete(o.inflight, key)
	}
}

func (o outlineState) loading(key loadKey) bool {
	_, ok := o.inflight[key]
	return ok
}

// cancelAll cancels every running load.
func (o *outlineState) cancelAll() {
	for key, cur := range o.inflight {
		cur.cancel()
		delete(o.inflight, key)
	}
}

// loaded reports whether any provider has reported for key.
func (o outlineState) loaded(key loadKey) bool {
	_, ok := o.cache[key]
	return ok
}

// mayHaveChildren reports whether key could show a child: it has not
// finished loading, or it loaded items. A load that came back empty or
// failed has nothing to show, so its row gets no triangle; its errors are
// in the details and messages panes.
func (o outlineState) mayHaveChildren(key loadKey) bool {
	return !o.complete[key] || len(o.items(key)) > 0
}

// failed reports whether key's latest load reported errors.
func (o outlineState) failed(key loadKey) bool {
	return len(o.errs[key]) > 0
}

// items is the union for key in model.KnownServices order, then any
// unregistered services sorted by name.
func (o outlineState) items(key loadKey) []model.Resource {
	bySvc := o.cache[key]
	if len(bySvc) == 0 {
		return nil
	}
	total := 0
	for _, list := range bySvc {
		total += len(list)
	}
	if total == 0 {
		return nil
	}
	svcs := make([]model.ServiceType, 0, len(bySvc))
	for svc, list := range bySvc {
		if len(list) > 0 {
			svcs = append(svcs, svc)
		}
	}
	model.SortServices(svcs)
	out := make([]model.Resource, 0, total)
	for _, svc := range svcs {
		out = append(out, bySvc[svc]...)
	}
	return out
}

// merge replaces the items one provider reported for key and reports whether
// its items or any outline-visible state (items, truncation, missing
// permissions, or errors) changed.
func (o *outlineState) merge(key loadKey, res model.ProviderResult) (itemsChanged, stateChanged bool) {
	bySvc, ok := o.cache[key]
	if !ok {
		bySvc = map[model.ServiceType][]model.Resource{}
		o.cache[key] = bySvc
	}
	itemsChanged = len(bySvc[res.Service]) > 0 || len(res.Resources) > 0
	bySvc[res.Service] = res.Resources

	truncChanged := o.truncated[key][res.Service] != res.Truncated
	if res.Truncated {
		if o.truncated[key] == nil {
			o.truncated[key] = map[model.ServiceType]bool{}
		}
		o.truncated[key][res.Service] = true
	} else if t := o.truncated[key]; t != nil {
		delete(t, res.Service)
		if len(t) == 0 {
			delete(o.truncated, key)
		}
	}

	missingChanged := !slices.Equal(o.missing[key][res.Service], res.Missing)
	if len(res.Missing) > 0 {
		if o.missing[key] == nil {
			o.missing[key] = map[model.ServiceType][]string{}
		}
		o.missing[key][res.Service] = res.Missing
	} else if m := o.missing[key]; m != nil {
		delete(m, res.Service)
		if len(m) == 0 {
			delete(o.missing, key)
		}
	}

	hadErr := slices.ContainsFunc(o.errs[key], func(e model.ProviderError) bool { return e.Service == res.Service })
	pe := model.AsProviderError(res.Service, res.Err)
	// An API off in the quota project is the caller's setup, reported once
	// by noteQuotaProject; it does not mark every scope.
	addErr := res.Err != nil && !pe.QuotaProject
	errsChanged := hadErr || addErr
	if errsChanged {
		errs := slices.DeleteFunc(slices.Clone(o.errs[key]), func(e model.ProviderError) bool { return e.Service == res.Service })
		if addErr {
			errs = append(errs, pe)
		}
		if len(errs) == 0 {
			delete(o.errs, key)
		} else {
			o.errs[key] = errs
		}
	}
	return itemsChanged, itemsChanged || truncChanged || missingChanged || errsChanged
}

// finish marks key complete and returns its current errors.
func (o *outlineState) finish(key loadKey) []model.ProviderError {
	if _, ok := o.cache[key]; !ok {
		o.cache[key] = map[model.ServiceType][]model.Resource{}
	}
	o.complete[key] = true
	return o.errs[key]
}

// denials are the services skipped under key for missing permissions, in
// model.KnownServices order.
func (o outlineState) denials(key loadKey) []model.Denial {
	bySvc := o.missing[key]
	if len(bySvc) == 0 {
		return nil
	}
	svcs := make([]model.ServiceType, 0, len(bySvc))
	for svc := range bySvc {
		if _, ok := model.ServiceOrder(svc); ok {
			svcs = append(svcs, svc)
		}
	}
	model.SortServices(svcs)
	out := make([]model.Denial, len(svcs))
	for i, svc := range svcs {
		out[i] = model.Denial{Service: svc, Missing: bySvc[svc]}
	}
	return out
}

// noteRows renders a muted "more not shown" row when any provider's
// latest report for key was truncated. The count is how many items those
// providers returned before stopping.
func (o outlineState) noteRows(key loadKey, owner model.Resource, facet model.Facet, depth int) []resources.Row {
	if len(o.truncated[key]) == 0 {
		return nil
	}
	shown := 0
	for svc := range o.truncated[key] {
		shown += len(o.cache[key][svc])
	}
	return []resources.Row{{
		Kind:     resources.RowNote,
		Resource: owner,
		Facet:    facet,
		Depth:    depth,
		Note:     fmt.Sprintf("… more not shown (limit %d)", shown),
	}}
}

// showGoogleSAs reports whether the Service Accounts collection collID
// shows the accounts Google created.
func (o *outlineState) showGoogleSAs(collID string) bool {
	if show, ok := o.googleSAs[collID]; ok {
		return show
	}
	return o.googleSAsDefault
}
