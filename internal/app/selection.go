package app

import (
	"slices"

	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/resources"
)

// scopeName is the display name of a CRM scope id, or the id itself.
func (m Model) scopeName(id string) string {
	if n, ok := m.tree.Find(id); ok && n.Name != "" {
		return n.Name
	}
	if id == m.scope.ID && m.scope.Name != "" {
		return m.scope.Name
	}
	return id
}

func (m *Model) syncDetails() {
	m.syncCrumb()
	row, ok := m.nav.SelectedRow()
	if !ok {
		m.details.SetResource(nil)
		m.details.SetProblems(nil)
		m.details.SetDenials(nil)
		return
	}
	cp := row.Resource
	if cp.IsBinding() && row.Via != "" {
		// A role under its principal shows its title and description
		// once its permissions have loaded.
		cp = model.WithRoleDetails(cp, m.outline.items(facetLoadKey(cp.ID, model.FacetRolePermissions)))
	}
	if len(cp.Facets) > 0 {
		var shown []model.Facet
		for _, f := range cp.Facets {
			if f.Optional {
				key := facetLoadKey(cp.ID, f.ID)
				if m.outline.complete[key] && len(m.outline.items(key)) == 0 && !m.outline.failed(key) {
					continue
				}
			}
			shown = append(shown, f)
		}
		cp.Facets = shown
	}
	m.details.SetResource(&cp)
	m.details.SetProblems(m.problems(row))
	m.details.SetDenials(m.denials(row))
}

// denials are the services skipped for missing permissions that the
// selected row covers: all of a scope's, or a product node's own.
func (m Model) denials(row resources.Row) []model.Denial {
	r := row.Resource
	if row.Kind != resources.RowResource {
		return nil
	}
	switch {
	case r.IsScope():
		return m.outline.denials(scopeKey(r.ID))
	case r.IsProductGroup():
		return slices.DeleteFunc(m.outline.denials(scopeKey(r.ScopeID)), func(d model.Denial) bool {
			return !model.InProduct(d.Service, r.Service)
		})
	}
	return nil
}

// problems are the provider errors that explain the selected row: its own
// error, a scope's or group's load errors, a collection's service's errors
// from its scope, a product node's errors for the services it holds
// (nested products included), or a facet's load errors.
func (m Model) problems(row resources.Row) []model.ProviderError {
	r := row.Resource
	if f, ok := r.InlineFacet(); ok && row.Kind == resources.RowResource {
		return m.outline.errs[facetLoadKey(r.ID, f.ID)]
	}
	switch {
	case row.Kind == resources.RowFacet:
		return m.outline.errs[facetLoadKey(r.ID, row.Facet.ID)]
	case r.IsBinding() && row.Via != "":
		// A role under its principal: its permissions' load.
		return m.outline.errs[facetLoadKey(r.ID, model.FacetRolePermissions)]
	case r.IsScope(), r.IsEnabledGroup():
		return m.outline.errs[nodeKey(r)]
	case r.IsCollection():
		var out []model.ProviderError
		for _, e := range m.outline.errs[scopeKey(r.ScopeID)] {
			if e.Service == r.Service {
				out = append(out, e)
			}
		}
		return out
	case r.IsProductGroup():
		var out []model.ProviderError
		for _, e := range m.outline.errs[scopeKey(r.ScopeID)] {
			if model.InProduct(e.Service, r.Service) {
				out = append(out, e)
			}
		}
		return out
	}
	return nil
}

// crumbCache is the breadcrumb text for one scope id.
type crumbCache struct {
	id, text string
	ok       bool
}

// crumb is the header breadcrumb: the path to the selected row's scope.
func (m Model) crumb() string {
	id := m.crumbScope()
	if m.crumbs.ok && m.crumbs.id == id {
		return m.crumbs.text
	}
	return m.crumbFor(id)
}

// syncCrumb keeps the breadcrumb for the selected row's scope.
func (m *Model) syncCrumb() {
	if id := m.crumbScope(); !m.crumbs.ok || m.crumbs.id != id {
		m.crumbs = crumbCache{id: id, text: m.crumbFor(id), ok: true}
	}
}

// crumbScope is the scope the breadcrumb leads to: the selected row if it
// is a scope, else the row's scope, else the explorer's.
func (m Model) crumbScope() string {
	id := m.scope.ID
	if r, ok := m.nav.Selected(); ok {
		if r.IsScope() {
			id = r.ID
		} else if r.ScopeID != "" {
			id = r.ScopeID
		}
	}
	return id
}

// crumbFor names the path to scope id, matching a bare project id as
// projects/{id} first, as FindScope does.
func (m Model) crumbFor(id string) string {
	if m.tree.ID == "" {
		return id
	}
	if path := m.tree.PathTo(model.ProjectScope(id)); len(path) > 0 {
		return model.Breadcrumb(path)
	}
	if model.ProjectScope(id) != id {
		if path := m.tree.PathTo(id); len(path) > 0 {
			return model.Breadcrumb(path)
		}
	}
	if m.scope.Name != "" {
		return m.scope.Name
	}
	return id
}

// selectedID is the header's ID line: the selected resource's ID or, on a
// row with no ID of its own (a facet, a collection, an object prefix), the
// ID of the nearest resource above it in the outline.
func (m Model) selectedID() string {
	rows := m.nav.Rows()
	i := m.nav.Cursor()
	if i < 0 || i >= len(rows) {
		return ""
	}
	depth := rows[i].Depth + 1
	for ; i >= 0; i-- {
		row := rows[i]
		if row.Depth >= depth {
			continue
		}
		depth = row.Depth
		r := row.Resource
		if row.Kind != resources.RowNote && !r.IsSynthetic() && r.ID != "" && r.ID != model.RootID {
			return r.ID
		}
	}
	return ""
}
