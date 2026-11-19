package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/resources"
)

// openInlineRow shows row's inline facet children, loading them first if
// needed. A finished load with nothing in it only flashes.
func (m Model) openInlineRow(row resources.Row, f model.Facet) (Model, tea.Cmd) {
	load := facetLoadKey(row.Resource.ID, f.ID)
	open := inlineKey(row, f)
	if m.outline.loaded(load) {
		if !m.outline.mayHaveChildren(load) {
			m.flashResult(m.outline.errs[load], emptyFlash(f))
			return m, nil
		}
		m.outline.showKids[open] = true
		m.rebuildOutline()
		return m, nil
	}
	if row.Via != "" {
		m.outline.showKids[open] = true
	}
	if m.outline.loading(load) {
		m.rebuildOutline()
		return m, nil
	}
	m.status.SetFlash("loading " + strings.ToLower(f.Label) + "…")
	cmd := m.startRelated(row.Resource, f, false)
	m.rebuildOutline()
	return m, cmd
}

// emptyFlash is the status line for a facet load that found nothing.
func emptyFlash(f model.Facet) string {
	return "no " + strings.ToLower(f.Label)
}

// inlineParent reports whether the explorer row keyed parentID lists its
// children inline.
func (m Model) inlineParent(parentID string) bool {
	for _, row := range m.nav.Rows() {
		if row.Kind == resources.RowResource && row.Via == "" && row.Resource.ID == parentID {
			_, ok := row.Resource.InlineFacet()
			return ok
		}
	}
	return false
}

func (m Model) toggleOutline() (Model, tea.Cmd) {
	row, ok := m.nav.SelectedRow()
	if !ok {
		return m, nil
	}
	switch row.Kind {
	case resources.RowFacet:
		key := facetLoadKey(row.Resource.ID, row.Facet.ID)
		if m.outline.loaded(key) && !m.outline.mayHaveChildren(key) {
			m.flashResult(m.outline.errs[key], emptyFlash(row.Facet))
			return m, nil
		}
		if m.outline.showKids[key] {
			m.outline.showKids[key] = false
			m.rebuildOutline()
			return m, nil
		}
		if m.outline.loaded(key) {
			m.outline.showKids[key] = true
			m.rebuildOutline()
			return m, nil
		}
		m.status.SetFlash("loading " + strings.ToLower(row.Facet.Label) + "…")
		cmd := m.startRelated(row.Resource, row.Facet, false)
		m.rebuildOutline()
		return m, cmd
	default:
		r := row.Resource
		key := nodeKey(r)
		if r.IsScope() {
			if row.Pinned {
				return m, nil
			}
			if m.outline.showKids[key] {
				m.outline.showKids[key] = false
				m.rebuildOutline()
				return m, nil
			}
			m.outline.showKids[key] = true
			if m.outline.loaded(key) {
				m.rebuildOutline()
				return m, nil
			}
			m.status.SetFlash("loading…")
			cmd := m.startScope(r.ID, false)
			m.rebuildOutline()
			return m, cmd
		}
		if r.IsCollection() || r.IsProductGroup() || r.IsPrefix() || r.IsBucket() {
			m.outline.showKids[key] = !m.outline.showKids[key]
			m.rebuildOutline()
			return m, nil
		}
		if r.IsBinding() && row.Via != "" {
			if !row.Expandable {
				m.flashResult(m.outline.errs[facetLoadKey(r.ID, model.FacetRolePermissions)], emptyFlash(model.RolePermissionsFacet()))
				return m, nil
			}
			return m.toggleRole(row)
		}
		if r.IsPermissionGroup() {
			key := openKey(row)
			m.outline.showKids[key] = !m.outline.showKids[key]
			m.rebuildOutline()
			return m, nil
		}
		if (r.IsBinding() || r.IsPrincipal() || r.IsInheritedGroup() || r.IsSeverityGroup() || r.Service == model.ServiceURLList) && row.Expandable {
			open := openKey(row)
			m.outline.showKids[open] = !m.outline.showKids[open]
			m.rebuildOutline()
			return m, nil
		}
		if r.IsEnabledGroup() {
			if m.outline.showKids[key] {
				m.outline.showKids[key] = false
				m.rebuildOutline()
				return m, nil
			}
			if m.outline.loaded(key) {
				m.outline.showKids[key] = true
				m.rebuildOutline()
				return m, nil
			}
			m.status.SetFlash("loading enabled services…")
			cmd := m.startRelated(r, model.Facet{ID: model.FacetEnabledServices, Label: "Enabled Services"}, false)
			m.rebuildOutline()
			return m, cmd
		}
		if f, ok := r.InlineFacet(); ok {
			if open := inlineKey(row, f); m.outline.showKids[open] {
				m.outline.showKids[open] = false
				m.rebuildOutline()
				return m, nil
			}
			return m.openInlineRow(row, f)
		}
		if len(r.Facets) == 0 {
			if layoutMode(m.width) != LayoutLarge {
				m.focus = PaneDetail
				m.syncLayout()
			}
			return m, nil
		}
		visible, expandable, _ := m.facetsState(r)
		if m.outline.showFacets[r.ID] && len(visible) > 0 {
			m.outline.showFacets[r.ID] = false
			m.rebuildOutline()
			return m, nil
		}
		if !expandable {
			m.status.SetFlash("nothing to show")
			return m, nil
		}
		m.outline.showFacets[r.ID] = true
		var cmds []tea.Cmd
		for _, f := range r.Facets {
			if !f.Optional {
				continue
			}
			key := facetLoadKey(r.ID, f.ID)
			if !m.outline.complete[key] && !m.outline.loading(key) {
				if cmd := m.startRelatedPrefetch(r, f); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
		m.rebuildOutline()
		return m, tea.Batch(cmds...)
	}
}

func (m Model) collapseOutline() (Model, tea.Cmd) {
	row, ok := m.nav.SelectedRow()
	if !ok {
		return m, nil
	}
	if row.Via != "" && row.Expanded {
		// A role under its principal, or a permission group under its
		// role: close it.
		m.outline.showKids[openKey(row)] = false
		m.rebuildOutline()
		return m, nil
	}
	if row.Via != "" {
		// A row listed under another parent (a member under its binding, a
		// binding under its principal, a permission under its role or group,
		// a rule under a web proxy, or a URL list / item): close its parent.
		if holder, ok := m.holderRow(); ok && holder.Key() == row.Via {
			m.outline.showKids[holderKey(holder)] = false
		} else {
			m.outline.showKids[loadKey{parent: row.Via}] = false
		}
		m.nav.SelectKey(row.Via)
		m.rebuildOutline()
		return m, nil
	}
	if (row.Resource.IsBinding() || row.Resource.IsPrincipal() || row.Resource.IsInheritedGroup() || row.Resource.IsSeverityGroup() || row.Resource.IsPermissionGroup() || row.Resource.Service == model.ServiceURLList) && row.Expanded {
		m.outline.showKids[nodeKey(row.Resource)] = false
		m.rebuildOutline()
		return m, nil
	}
	if row.Resource.IsScope() {
		if row.Pinned {
			return m, nil
		}
		if m.outline.showKids[scopeKey(row.Resource.ID)] {
			m.outline.showKids[scopeKey(row.Resource.ID)] = false
			m.rebuildOutline()
			return m, nil
		}
		if parent := row.Resource.ParentID; parent != "" {
			if m.pinned(parent) {
				m.nav.SelectKey(navParent(row.Resource))
			} else {
				m.nav.SelectKey(parent)
			}
			if !m.pinned(parent) {
				m.outline.showKids[scopeKey(parent)] = false
			}
			m.rebuildOutline()
			return m, nil
		}
		return m, nil
	}
	if row.Resource.IsCollection() || row.Resource.IsProductGroup() || row.Resource.IsEnabledGroup() {
		key := nodeKey(row.Resource)
		if m.outline.showKids[key] {
			m.outline.showKids[key] = false
			m.rebuildOutline()
			return m, nil
		}
		if row.Resource.ParentID != "" {
			m.nav.SelectKey(navParent(row.Resource))
			m.rebuildOutline()
			return m, nil
		}
		return m, nil
	}
	if row.Kind == resources.RowFacet {
		key := facetLoadKey(row.Resource.ID, row.Facet.ID)
		if m.outline.showKids[key] {
			m.outline.showKids[key] = false
			m.rebuildOutline()
			return m, nil
		}
		m.outline.showFacets[row.Resource.ID] = false
		m.rebuildOutline()
		return m, nil
	}
	if row.Resource.IsPrefix() {
		key := nodeKey(row.Resource)
		if m.outline.showKids[key] {
			m.outline.showKids[key] = false
			m.rebuildOutline()
			return m, nil
		}
		m.nav.SelectKey(row.Resource.ParentID)
		if strings.Contains(row.Resource.ParentID, "/prefixes/") {
			m.outline.showKids[loadKey{parent: row.Resource.ParentID}] = false
		} else if row.Resource.ParentID != "" {
			m.outline.showKids[facetLoadKey(row.Resource.ParentID, row.Resource.FacetID)] = false
		}
		m.rebuildOutline()
		return m, nil
	}
	if row.Resource.IsBucket() && row.Expanded {
		m.outline.showKids[nodeKey(row.Resource)] = false
		m.rebuildOutline()
		return m, nil
	}
	if f, ok := row.Resource.InlineFacet(); ok {
		if key := facetLoadKey(row.Resource.ID, f.ID); m.outline.showKids[key] {
			m.outline.showKids[key] = false
			m.rebuildOutline()
			return m, nil
		}
	}
	if holder, ok := m.holderRow(); ok && (row.Resource.IsBucket() || holder.Resource.IsBucket()) {
		// A closed bucket closes what holds it; a row in a bucket closes
		// the bucket.
		m.outline.showKids[holderKey(holder)] = false
		m.nav.SelectKey(holder.Key())
		m.rebuildOutline()
		return m, nil
	}
	if r := row.Resource; r.Service == model.ServiceVulnerability && !r.IsSeverityGroup() && r.ParentID != "" {
		// A vulnerability closes its severity group.
		id := model.SeverityGroupID(r.ParentID, r.State)
		m.outline.showKids[loadKey{parent: id}] = false
		m.nav.SelectKey(id)
		m.rebuildOutline()
		return m, nil
	}
	if row.Depth > 0 {
		parent := row.Resource.ParentID
		facet := row.Resource.FacetID
		if parent != "" && facet != "" {
			if m.inlineParent(parent) {
				m.nav.SelectKey(parent)
			}
			m.outline.showKids[facetLoadKey(parent, facet)] = false
			m.rebuildOutline()
			return m, nil
		}
	}
	if m.outline.showFacets[row.Resource.ID] {
		m.outline.showFacets[row.Resource.ID] = false
		m.rebuildOutline()
		return m, nil
	}
	return m, nil
}

// navParent is the row r is drawn under. That is r.ParentID, except that
// an organization's rows other than Workspace, and the root's projects and
// folders, sit under that node's Cloud Platform node.
func navParent(r model.Resource) string {
	p := r.ParentID
	if model.ScopeKind(p) == model.KindOrg && r.Service != model.ServiceWorkspace && r.Service != model.ServiceCloudPlatform {
		return model.ProductGroupID(p, model.ServiceCloudPlatform)
	}
	if p == model.RootID && r.Service != model.ServiceOrganization && r.Service != model.ServiceCloudPlatform {
		return model.ProductGroupID(p, model.ServiceCloudPlatform)
	}
	return p
}

// holderRow is the row the selected row is listed under: the nearest
// row above it that is indented less.
func (m Model) holderRow() (resources.Row, bool) {
	rows, i := m.nav.Rows(), m.nav.Cursor()
	if i < 0 || i >= len(rows) {
		return resources.Row{}, false
	}
	for j := i - 1; j >= 0; j-- {
		if rows[j].Depth < rows[i].Depth {
			return rows[j], true
		}
	}
	return resources.Row{}, false
}

// holderKey is the key that keeps holder open: a cross-parent row's openKey,
// a facet row's load, an inline resource's facet, or the node itself.
func holderKey(holder resources.Row) loadKey {
	if holder.Via != "" {
		return openKey(holder)
	}
	if holder.Kind == resources.RowFacet {
		return facetLoadKey(holder.Resource.ID, holder.Facet.ID)
	}
	if f, ok := holder.Resource.InlineFacet(); ok {
		return facetLoadKey(holder.Resource.ID, f.ID)
	}
	return nodeKey(holder.Resource)
}

func (m Model) expandFromDetails() (Model, tea.Cmd) {
	row, ok := m.nav.SelectedRow()
	if !ok {
		m.status.SetFlash("select a resource")
		return m, nil
	}
	r := row.Resource
	f, ok := m.details.SelectedFacet()
	if !ok {
		m.status.SetFlash("nothing to open")
		return m, nil
	}
	m.focus = PaneNav
	m.syncLayout()
	if f.Inline {
		m.nav.SelectKey(row.Key())
		return m.openInlineRow(row, f)
	}
	m.outline.showFacets[r.ID] = true
	key := facetLoadKey(r.ID, f.ID)
	if f.Optional && m.outline.complete[key] && len(m.outline.items(key)) == 0 && !m.outline.failed(key) {
		m.status.SetFlash(emptyFlash(f))
		return m, nil
	}
	m.nav.SelectKey(facetRowKey(r.ID, f.ID))
	if m.outline.showKids[key] {
		m.rebuildOutline()
		return m, nil
	}
	if m.outline.loaded(key) {
		m.outline.showKids[key] = true
		m.rebuildOutline()
		return m, nil
	}
	m.status.SetFlash("loading " + strings.ToLower(f.Label) + "…")
	cmd := m.startRelated(r, f, false)
	m.rebuildOutline()
	return m, cmd
}
