package app

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/resources"
)

func (m *Model) rebuildOutline() {
	var rows []resources.Row
	if m.tree.ID != "" {
		rows = m.walkScope(m.tree, 0)
	} else {
		for _, r := range model.Roots(m.catalog) {
			rows = append(rows, m.outlineRows(r, 0)...)
		}
	}
	if q := strings.ToLower(strings.TrimSpace(m.search.Query)); q != "" {
		rows = filterExplorer(rows, q)
	}
	m.nav.SetRows(rows)
	m.syncDetails()
}

func (m *Model) walkScope(n model.HierarchyNode, depth int) []resources.Row {
	r := n.Resource()
	key := scopeKey(n.ID)
	searching := strings.TrimSpace(m.search.Query) != ""
	// While searching, a collapsed scope opens when its resources are
	// already loaded, like collections do, so matches in it are found.
	// Search never starts a load.
	pinned := m.pinned(n.ID)
	open := pinned || m.outline.showKids[key] || (searching && m.outline.complete[key])
	kids := model.Roots(m.outline.items(key))
	expandable := len(n.Children) > 0 || !m.outline.complete[key] || len(kids) > 0 || len(m.outline.missing[key]) > 0
	out := []resources.Row{{
		Kind:       resources.RowResource,
		Resource:   r,
		Depth:      depth,
		Expanded:   open && expandable,
		Expandable: expandable,
		Pinned:     pinned,
		Loading:    m.outline.loading(key),
		Problems:   m.outline.failed(key),
	}}
	if !open {
		return out
	}
	owner := r
	if n.Kind == model.KindRoot {
		return append(out, m.rootRows(n, owner, depth)...)
	}
	// The scope's own products list ahead of its child projects, First
	// products (IAM) leading, so they read as the scope's rather than as
	// rows after its children. Child folders come last, together.
	first, rest := model.SplitFirst(model.GroupByProduct(model.GroupByService(kids)))
	if n.Kind == model.KindOrg {
		// An organization holds Workspace and Cloud Platform side by
		// side, so a reader who wants only one can close the other.
		var ws []model.ProductSection
		rest = slices.DeleteFunc(rest, func(sec model.ProductSection) bool {
			if sec.Product != model.ServiceWorkspace {
				return false
			}
			ws = append(ws, sec)
			return true
		})
		for _, sec := range ws {
			out = append(out, m.sectionRows(owner, sec, depth+1)...)
		}
		cloud := 0
		for _, sec := range slices.Concat(first, rest) {
			cloud += sec.Items()
		}
		plat := model.ProductGroupResource(owner, model.ServiceCloudPlatform, cloud)
		platKey := nodeKey(plat)
		// Cloud Platform starts open; the map holds only what the user
		// has toggled, so seed it once.
		if _, seen := m.outline.showKids[platKey]; !seen {
			m.outline.showKids[platKey] = true
		}
		platOpen := m.outline.showKids[platKey] || searching
		out = append(out, resources.Row{
			Kind:       resources.RowResource,
			Resource:   plat,
			Depth:      depth + 1,
			Expanded:   platOpen,
			Expandable: true,
		})
		if !platOpen {
			return out
		}
		depth++
	}
	for _, sec := range first {
		out = append(out, m.sectionRows(owner, sec, depth+1)...)
	}
	if n.Kind == model.KindProject {
		en := model.EnabledServicesResource(owner)
		enKey := nodeKey(en)
		enOpen := m.outline.showKids[enKey] || searching
		out = append(out, resources.Row{
			Kind:       resources.RowResource,
			Resource:   en,
			Depth:      depth + 1,
			Expanded:   enOpen,
			Expandable: true,
			Loading:    m.outline.loading(enKey),
			Problems:   m.outline.failed(enKey),
		})
		if enOpen {
			apis := m.outline.items(enKey)
			model.SortByName(apis)
			for _, api := range apis {
				out = append(out, m.outlineRows(api, depth+2)...)
			}
			out = append(out, m.outline.noteRows(enKey, en, model.Facet{}, depth+2)...)
		}
	}
	for _, sec := range rest {
		out = append(out, m.sectionRows(owner, sec, depth+1)...)
	}
	for _, ch := range n.ChildrenFoldersLast() {
		out = append(out, m.walkScope(ch, depth+1)...)
	}
	return out
}

// rootRows lists the synthetic root's children. Organizations sit directly
// under it, each with its own Workspace and Cloud Platform. Projects and
// folders outside any organization go in a Cloud Platform node of the
// root's, so the tree reads the same with or without an organization.
func (m *Model) rootRows(n model.HierarchyNode, owner model.Resource, depth int) []resources.Row {
	var out []resources.Row
	loose := model.HierarchyNode{}
	for _, ch := range n.Children {
		if ch.Kind == model.KindOrg {
			out = append(out, m.walkScope(ch, depth+1)...)
			continue
		}
		loose.Children = append(loose.Children, ch)
	}
	if len(loose.Children) == 0 {
		return out
	}
	plat := model.ProductGroupResource(owner, model.ServiceCloudPlatform, 0)
	platKey := nodeKey(plat)
	// Cloud Platform starts open; the map holds only what the user has
	// toggled, so seed it once.
	if _, seen := m.outline.showKids[platKey]; !seen {
		m.outline.showKids[platKey] = true
	}
	platOpen := m.outline.showKids[platKey] || strings.TrimSpace(m.search.Query) != ""
	out = append(out, resources.Row{
		Kind:       resources.RowResource,
		Resource:   plat,
		Depth:      depth + 1,
		Expanded:   platOpen,
		Expandable: true,
	})
	if !platOpen {
		return out
	}
	for _, ch := range loose.ChildrenFoldersLast() {
		out = append(out, m.walkScope(ch, depth+2)...)
	}
	return out
}

// pinned reports whether the scope id is the top of the CRM tree when that
// is an organization (or the root over several). It is always expanded.
func (m Model) pinned(id string) bool {
	return id != "" && id == m.tree.ID && (m.tree.Kind == model.KindOrg || m.tree.Kind == model.KindRoot)
}

// sectionRows renders one product section under owner: an ungrouped
// collection, or a product node holding its collections.
func (m *Model) sectionRows(owner model.Resource, sec model.ProductSection, depth int) []resources.Row {
	if sec.Product == "" {
		return m.collectionRows(owner, sec.Groups[0], depth)
	}
	prod := model.ProductGroupResource(owner, sec.Product, m.shownItems(owner.ID, sec))
	open := m.outline.showKids[nodeKey(prod)] || strings.TrimSpace(m.search.Query) != ""
	out := []resources.Row{{
		Kind:       resources.RowResource,
		Resource:   prod,
		Depth:      depth,
		Expanded:   open,
		Expandable: true,
	}}
	if !open {
		return out
	}
	for _, g := range sec.Groups {
		out = append(out, m.collectionRows(prod, g, depth+1)...)
	}
	for _, sub := range sec.Subs {
		out = append(out, m.sectionRows(prod, sub, depth+1)...)
	}
	return out
}

// collectionRows renders one service group's collection row under parent
// (a CRM node or a product node) and, when open, its resources.
func (m *Model) collectionRows(parent model.Resource, g model.ServiceGroup, depth int) []resources.Row {
	g.Items = m.shown(parent.ID, g)
	coll := model.CollectionResource(parent, g.Service, len(g.Items))
	open := m.outline.showKids[nodeKey(coll)] || strings.TrimSpace(m.search.Query) != ""
	row := resources.Row{
		Kind:       resources.RowResource,
		Resource:   coll,
		Depth:      depth,
		Expanded:   open,
		Expandable: true,
	}
	byPrincipal := m.outline.byPrincipal[coll.ID]
	switch g.Service {
	case model.ServiceIAMBinding:
		row.Choices = bindingViews
		if byPrincipal {
			row.Chosen = 1
		}
	case model.ServiceServiceAccount:
		row.Choices = accountViews
		if m.outline.showGoogleSAs(coll.ID) {
			row.Chosen = 1
		}
	}
	out := []resources.Row{row}
	if !open {
		return out
	}
	if g.Service == model.ServiceIAMBinding {
		return append(out, m.bindingRows(coll, g.Items, depth+1, byPrincipal)...)
	}
	if model.Bucketed(g.Service) {
		return append(out, m.bucketRows(model.Buckets(coll, "", g.Items), depth+1)...)
	}
	for _, res := range g.Items {
		out = append(out, m.outlineRows(res, depth+1)...)
	}
	return out
}

// shown is g's items as the explorer lists them under the node parentID:
// without the service accounts Google created unless that collection
// shows them.
func (m *Model) shown(parentID string, g model.ServiceGroup) []model.Resource {
	if g.Service != model.ServiceServiceAccount || m.outline.showGoogleSAs(model.CollectionID(parentID, g.Service)) {
		return g.Items
	}
	return model.WithoutGoogleServiceAccounts(g.Items)
}

// shownItems counts the items of sec, a product section under the node
// ownerID, as the explorer lists them.
func (m *Model) shownItems(ownerID string, sec model.ProductSection) int {
	prodID := model.ProductGroupID(ownerID, sec.Product)
	n := 0
	for _, g := range sec.Groups {
		n += len(m.shown(prodID, g))
	}
	for _, sub := range sec.Subs {
		n += m.shownItems(prodID, sub)
	}
	return n
}

func filterExplorer(rows []resources.Row, q string) []resources.Row {
	keep := make([]bool, len(rows))
	var ancestors []int
	for i, row := range rows {
		if row.Depth >= len(ancestors) {
			ancestors = append(ancestors, make([]int, row.Depth-len(ancestors)+1)...)
		}
		ancestors = ancestors[:row.Depth+1]
		ancestors[row.Depth] = i

		if row.Kind == resources.RowNote {
			continue
		}
		var matched bool
		if row.Kind == resources.RowFacet {
			matched = model.ContainsFold(row.Facet.Label, q)
		} else {
			matched = row.Resource.Matches(q)
		}
		if !matched {
			continue
		}
		for d := row.Depth; d >= 0 && !keep[ancestors[d]]; d-- {
			keep[ancestors[d]] = true
		}
	}
	out := make([]resources.Row, 0, len(rows))
	for i, row := range rows {
		if keep[i] {
			out = append(out, row)
		}
	}
	return out
}

func (m *Model) outlineRows(r model.Resource, depth int) []resources.Row {
	return m.outlineRowsVia(r, "", depth)
}

// maxOptionalPrefetch caps how many optional-only child resources are
// prefetched automatically when their parent collection or facet opens.
// Larger lists fall back to on-demand loading so opening a huge collection
// does not burst GetIamPolicy calls.
const maxOptionalPrefetch = 50

// optionalOnly reports whether r has facets and every one of them is Optional.
func optionalOnly(r model.Resource) bool {
	if len(r.Facets) == 0 {
		return false
	}
	for _, f := range r.Facets {
		if !f.Optional {
			return false
		}
	}
	return true
}

// prefetchOptionalOnly starts background prefetches for the optional facets
// of optional-only resources in rs so empty ones never show an expand arrow.
// Skips prefetching when more than maxOptionalPrefetch resources qualify.
func (m *Model) prefetchOptionalOnly(rs []model.Resource) tea.Cmd {
	var targets []model.Resource
	for _, r := range rs {
		if optionalOnly(r) {
			targets = append(targets, r)
		}
	}
	if len(targets) == 0 || len(targets) > maxOptionalPrefetch {
		return nil
	}
	var cmds []tea.Cmd
	for _, r := range targets {
		for _, f := range r.Facets {
			key := facetLoadKey(r.ID, f.ID)
			if !m.outline.complete[key] && !m.outline.loading(key) {
				if cmd := m.startRelatedPrefetch(r, f); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
	}
	return tea.Batch(cmds...)
}

// collectionItems returns the top-level resources belonging to collection row c.
func (m *Model) collectionItems(c model.Resource) []model.Resource {
	var out []model.Resource
	for _, r := range m.outline.items(scopeKey(c.ScopeID)) {
		if r.ParentID == "" && r.Service == c.Service {
			out = append(out, r)
		}
	}
	return out
}

func (m *Model) facetsState(r model.Resource) (visible []model.Facet, expandable, loading bool) {
	anyPending := false
	for _, f := range r.Facets {
		if !f.Optional {
			visible = append(visible, f)
			continue
		}
		key := facetLoadKey(r.ID, f.ID)
		if m.outline.loading(key) {
			loading = true
		}
		if !m.outline.complete[key] {
			anyPending = true
			continue
		}
		if len(m.outline.items(key)) > 0 || m.outline.failed(key) {
			visible = append(visible, f)
		}
	}
	expandable = len(visible) > 0 || (anyPending && !loading)
	return visible, expandable, loading
}

func (m *Model) outlineRowsVia(r model.Resource, via string, depth int) []resources.Row {
	if r.Service == model.ServiceURLList {
		return m.urlListRows(r, via, depth)
	}
	if f, ok := r.InlineFacet(); ok {
		return m.inlineRows(r, f, via, depth)
	}
	open := m.outline.showFacets[r.ID]
	visible, expandable, loading := m.facetsState(r)
	row := resources.Row{
		Kind:       resources.RowResource,
		Resource:   r,
		Depth:      depth,
		Via:        via,
		Expanded:   open && len(visible) > 0,
		Expandable: expandable,
		Loading:    open && loading,
	}
	out := []resources.Row{row}
	if !open || len(visible) == 0 {
		return out
	}
	for _, f := range visible {
		key := facetLoadKey(r.ID, f.ID)
		kidsOpen := m.outline.showKids[key]
		expandable := m.outline.mayHaveChildren(key)
		out = append(out, resources.Row{
			Kind:       resources.RowFacet,
			Resource:   r,
			Facet:      f,
			Depth:      depth + 1,
			Expanded:   kidsOpen && expandable,
			Expandable: expandable,
			Loading:    m.outline.loading(key),
			Problems:   m.outline.failed(key),
		})
		if !kidsOpen || !expandable {
			continue
		}
		out = append(out, m.facetChildRowsVia(row.Key(), r, f, m.outline.items(key), depth+2)...)
		out = append(out, m.outline.noteRows(key, r, f, depth+2)...)
	}
	return out
}

// urlListRows renders a Secure Web Proxy URL list and, when open, one row per
// pattern in its URLs list. While searching, loaded URL lists open so a match
// on an allowlisted host shows.
func (m *Model) urlListRows(r model.Resource, via string, depth int) []resources.Row {
	urls := r.List(model.ListURLs)
	expandable := len(urls) > 0
	row := resources.Row{
		Kind:       resources.RowResource,
		Resource:   r,
		Depth:      depth,
		Via:        via,
		Expandable: expandable,
	}
	searching := strings.TrimSpace(m.search.Query) != ""
	row.Expanded = expandable && (m.outline.showKids[openKey(row)] || searching)
	out := []resources.Row{row}
	if !row.Expanded {
		return out
	}
	parentKey := row.Key()
	for i, u := range urls {
		out = append(out, resources.Row{
			Kind:     resources.RowResource,
			Resource: model.URLListItemResource(r, u, i),
			Depth:    depth + 1,
			Via:      parentKey,
		})
	}
	return out
}

func inlineKey(row resources.Row, f model.Facet) loadKey {
	if row.Via != "" {
		return openKey(row)
	}
	return facetLoadKey(row.Resource.ID, f.ID)
}

// inlineRows renders a resource whose sole facet is inline: the resource
// row expands straight to the facet's children.
func (m *Model) inlineRows(r model.Resource, f model.Facet, via string, depth int) []resources.Row {
	load := facetLoadKey(r.ID, f.ID)
	row := resources.Row{
		Kind:     resources.RowResource,
		Resource: r,
		Depth:    depth,
		Via:      via,
		Loading:  m.outline.loading(load),
		Problems: m.outline.failed(load),
	}
	open := m.outline.showKids[inlineKey(row, f)]
	expandable := m.outline.mayHaveChildren(load)
	row.Expanded = open && expandable
	row.Expandable = expandable
	out := []resources.Row{row}
	if !open || !expandable {
		return out
	}
	out = append(out, m.facetChildRowsVia(row.Key(), r, f, m.outline.items(load), depth+1)...)
	notes := m.outline.noteRows(load, r, f, depth+1)
	if via != "" {
		for i := range notes {
			notes[i].Via = row.Key()
		}
	}
	return append(out, notes...)
}

func (m *Model) facetChildRowsVia(parentKey string, parent model.Resource, facet model.Facet, kids []model.Resource, depth int) []resources.Row {
	if facet.Nested {
		return m.prefixRows(model.NestPrefixes(parent, kids), depth)
	}
	if hasBindings(kids) {
		return m.bindingRows(parent, kids, depth, false)
	}
	if facet.ID == model.FacetVulnerabilities {
		return m.severityRows(parent, kids, depth)
	}
	if facet.ID == model.FacetRolePermissions {
		return m.permissionRows(model.GroupPermissions(parent.ID, kids), "", depth)
	}
	if len(kids) > 0 && model.Bucketed(kids[0].Service) {
		return m.bucketRows(model.Buckets(parent, facet.ID, kids), depth)
	}
	var out []resources.Row
	for _, c := range kids {
		via := ""
		if !strings.HasPrefix(c.ID, parent.ID+"/") {
			via = parentKey
		}
		out = append(out, m.outlineRowsVia(c, via, depth)...)
	}
	return out
}

// severityRows lists an image's vulnerabilities under one collapsed node
// per severity, most severe first, so a long scan opens one severity at a
// time. While searching, every group is open so a match shows.
func (m *Model) severityRows(parent model.Resource, vulns []model.Resource, depth int) []resources.Row {
	searching := strings.TrimSpace(m.search.Query) != ""
	groups, rest := model.GroupBySeverity(parent, vulns)
	var out []resources.Row
	for _, g := range groups {
		open := m.outline.showKids[nodeKey(g.Node)] || searching
		out = append(out, resources.Row{Kind: resources.RowResource, Resource: g.Node, Depth: depth, Expanded: open, Expandable: true})
		if open {
			for _, v := range g.Vulnerabilities {
				out = append(out, m.outlineRows(v, depth+1)...)
			}
		}
	}
	for _, r := range rest {
		out = append(out, m.outlineRows(r, depth)...)
	}
	return out
}

// bucketRows lays out a long list in org unit folders and letter buckets
// (model.Buckets). Each bucket opens on its own; while searching, every
// bucket is open so a match shows.
func (m *Model) bucketRows(nodes []model.BucketNode, depth int) []resources.Row {
	searching := strings.TrimSpace(m.search.Query) != ""
	var out []resources.Row
	for _, n := range nodes {
		if !n.Resource.IsBucket() {
			out = append(out, m.outlineRows(n.Resource, depth)...)
			continue
		}
		open := m.outline.showKids[nodeKey(n.Resource)] || searching
		out = append(out, resources.Row{Kind: resources.RowResource, Resource: n.Resource, Depth: depth, Expanded: open, Expandable: true})
		if open {
			out = append(out, m.bucketRows(n.Children, depth+1)...)
		}
	}
	return out
}

func (m *Model) prefixRows(nodes []model.PrefixNode, depth int) []resources.Row {
	var out []resources.Row
	for _, n := range nodes {
		open := m.outline.showKids[nodeKey(n.Resource)]
		expandable := n.Resource.IsPrefix() && len(n.Children) > 0
		out = append(out, resources.Row{
			Kind:       resources.RowResource,
			Resource:   n.Resource,
			Depth:      depth,
			Expanded:   open && expandable,
			Expandable: expandable,
		})
		if expandable && open {
			out = append(out, m.prefixRows(n.Children, depth+1)...)
		}
	}
	return out
}
