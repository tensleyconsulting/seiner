package app

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/resources"
)

// bindingViews are the choices on an Role Bindings collection row.
var bindingViews = []string{"role", "principal"}

// accountViews are the choices on a Service Accounts collection row: the
// accounts you created, or those plus the ones Google created.
var accountViews = []string{"yours", "all"}

// bindingRows lists IAM bindings under parent: those set on the scope
// itself, then one collapsed node per ancestor holding the bindings
// inherited from it, nearest first, so the scope's own are not lost among
// them. Each list is laid out by role or by principal alike.
func (m *Model) bindingRows(parent model.Resource, bindings []model.Resource, depth int, byPrincipal bool) []resources.Row {
	searching := strings.TrimSpace(m.search.Query) != ""
	own, inherited := model.SplitInherited(parent, bindings)
	out := m.bindingList(parent, own, depth, byPrincipal)
	for _, g := range inherited {
		open := m.outline.showKids[nodeKey(g.Node)] || searching
		out = append(out, resources.Row{Kind: resources.RowResource, Resource: g.Node, Depth: depth, Expanded: open, Expandable: true})
		if open {
			out = append(out, m.bindingList(g.Node, withoutOrigin(g.Bindings), depth+1, byPrincipal)...)
		}
	}
	return out
}

// withoutOrigin copies bindings without their Origin, so rows inside an
// inherited group do not repeat the group's own origin mark.
func withoutOrigin(bindings []model.Resource) []model.Resource {
	out := slices.Clone(bindings)
	for i := range out {
		out[i].Origin = ""
	}
	return out
}

// bindingList lays out bindings under parent. By role, each binding opens
// to its members; by principal, each member opens to its bindings, and
// each of those to its role's permissions. The nested rows carry Via so a
// resource listed under several parents keeps a unique row key. While
// searching, every binding or principal is open so a match on a member or
// role shows.
func (m *Model) bindingList(parent model.Resource, bindings []model.Resource, depth int, byPrincipal bool) []resources.Row {
	searching := strings.TrimSpace(m.search.Query) != ""
	groups := model.GroupByPrincipal(parent, bindings)
	var out []resources.Row
	if byPrincipal {
		for _, g := range groups {
			p := g.Principal
			open := m.outline.showKids[nodeKey(p)] || searching
			out = append(out, resources.Row{Kind: resources.RowResource, Resource: p, Depth: depth, Expanded: open, Expandable: true})
			if !open {
				continue
			}
			for _, b := range g.Bindings {
				out = append(out, m.roleRows(b, p.ID, depth+1)...)
			}
		}
		return out
	}
	principals := make(map[string]model.Resource, len(groups))
	for _, g := range groups {
		principals[g.Principal.Name] = g.Principal
	}
	for _, b := range bindings {
		if !b.IsBinding() {
			out = append(out, m.outlineRows(b, depth)...)
			continue
		}
		members := b.List(model.ListMembers)
		expandable := len(members) > 0
		open := expandable && (m.outline.showKids[nodeKey(b)] || searching)
		out = append(out, resources.Row{Kind: resources.RowResource, Resource: b, Depth: depth, Expanded: open, Expandable: expandable})
		if !open {
			continue
		}
		for _, mem := range members {
			out = append(out, resources.Row{Kind: resources.RowResource, Resource: principals[mem], Depth: depth + 1, Via: b.ID})
		}
	}
	return out
}

// roleRows lists binding b under the principal row via. It opens to the
// permissions b's role grants, loaded on first open and shared by every
// principal row that holds b. While searching, a loaded role is open.
func (m *Model) roleRows(b model.Resource, via string, depth int) []resources.Row {
	load := facetLoadKey(b.ID, model.FacetRolePermissions)
	row := resources.Row{
		Kind:       resources.RowResource,
		Resource:   b,
		Depth:      depth,
		Via:        via,
		Expandable: m.outline.mayHaveChildren(load),
		Loading:    m.outline.loading(load),
		Problems:   m.outline.failed(load),
	}
	searching := strings.TrimSpace(m.search.Query) != ""
	row.Expanded = row.Expandable && (m.outline.showKids[openKey(row)] || (searching && m.outline.complete[load]))
	out := []resources.Row{row}
	if !row.Expanded {
		return out
	}
	nodes := model.GroupPermissions(via+"/"+b.ID, m.outline.items(load))
	out = append(out, m.permissionRows(nodes, row.Key(), depth+1)...)
	return append(out, m.outline.noteRows(load, b, model.RolePermissionsFacet(), depth+1)...)
}

// permissionRows lays out a role's permissions under the row keyed via
// ("" for a role listed once). Groups open on their own; while searching,
// every group is open so a match shows.
func (m *Model) permissionRows(nodes []model.PermissionNode, via string, depth int) []resources.Row {
	searching := strings.TrimSpace(m.search.Query) != ""
	var out []resources.Row
	for _, n := range nodes {
		row := resources.Row{Kind: resources.RowResource, Resource: n.Resource, Depth: depth, Via: via}
		if !n.Resource.IsPermissionGroup() {
			out = append(out, row)
			continue
		}
		row.Expandable = true
		row.Expanded = m.outline.showKids[openKey(row)] || searching
		out = append(out, row)
		if row.Expanded {
			out = append(out, m.permissionRows(n.Children, row.Key(), depth+1)...)
		}
	}
	return out
}

// toggleRole opens or closes a role under its principal, loading its
// permissions first if needed. A finished load with nothing in it only
// flashes.
func (m Model) toggleRole(row resources.Row) (Model, tea.Cmd) {
	key := openKey(row)
	if m.outline.showKids[key] {
		m.outline.showKids[key] = false
		m.rebuildOutline()
		return m, nil
	}
	f := model.RolePermissionsFacet()
	load := facetLoadKey(row.Resource.ID, f.ID)
	if m.outline.loaded(load) && !m.outline.mayHaveChildren(load) {
		m.flashResult(m.outline.errs[load], emptyFlash(f))
		return m, nil
	}
	m.outline.showKids[key] = true
	if m.outline.loaded(load) || m.outline.loading(load) {
		m.rebuildOutline()
		return m, nil
	}
	m.status.SetFlash("loading " + strings.ToLower(f.Label) + "…")
	cmd := m.startRelated(row.Resource, f, false)
	m.rebuildOutline()
	return m, cmd
}

// hasBindings reports whether any of items is an IAM binding.
func hasBindings(items []model.Resource) bool {
	return slices.ContainsFunc(items, model.Resource.IsBinding)
}

// chooseView sets the selected row's choice to i (or the next one when i
// is negative). Role Bindings collections choose their layout, and Service
// Accounts collections whether Google's accounts show; the collection opens
// so the new view shows.
func (m Model) chooseView(i int) (Model, tea.Cmd) {
	row, ok := m.nav.SelectedRow()
	if !ok || len(row.Choices) == 0 {
		m.status.SetFlash("no other view here")
		return m, nil
	}
	if i < 0 {
		i = (row.Chosen + 1) % len(row.Choices)
	}
	switch row.Resource.Service {
	case model.ServiceServiceAccount:
		m.outline.googleSAs[row.Resource.ID] = i == 1
		if i == 1 {
			m.status.SetFlash("showing Google service accounts")
		} else {
			m.status.SetFlash("hiding Google service accounts")
		}
	default:
		m.outline.byPrincipal[row.Resource.ID] = i == 1
		m.status.SetFlash("bindings by " + row.Choices[i])
	}
	m.outline.showKids[nodeKey(row.Resource)] = true
	m.rebuildOutline()
	return m, nil
}
