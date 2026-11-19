package crm

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// Project is the Google-free slice of a CRM project.
type Project struct {
	ID          string // user project id (prj-…)
	Number      string // project number; "" if unknown
	DisplayName string
	Parent      string // organizations/… or folders/…
	Created     time.Time
	Labels      map[string]string
}

// Folder is a CRM folder.
type Folder struct {
	Name        string // folders/123
	DisplayName string
	Parent      string
	Created     time.Time
}

// Org is a CRM organization.
type Org struct {
	Name        string // organizations/123
	DisplayName string
	Created     time.Time
}

// API is the Resource Manager boundary. Tests inject a fake.
type API interface {
	SearchProjects(ctx context.Context) ([]Project, error)
	GetFolder(ctx context.Context, name string) (Folder, error)
	GetOrg(ctx context.Context, name string) (Org, error)
}

type node struct {
	id, name, parent string
	kind             model.NodeKind
	number           string
	created          time.Time
	labels           map[string]string
}

// maxConcurrentLookups bounds how many GetFolder/GetOrg calls run at once.
const maxConcurrentLookups = 8

// Build assembles a HierarchyNode tree from accessible projects and their
// ancestors. A folder or org that cannot be read yields a stub node. When
// expected(err) is true for the lookup's error (a missing permission), the
// stub is quiet; any other failure (an outage, a quota) is logged, and Build
// returns the tree with a model.Partial error, since a stub folder hides its
// own parent. A nil expected treats every failure as unexpected. Ancestors
// are looked up a level at a time, concurrently within a level. If ctx ends
// before every lookup has run, Build fails rather than return a tree with
// unread folders as stray roots. Multiple roots are wrapped in a synthetic
// KindRoot.
func Build(ctx context.Context, api API, expected func(error) bool) (model.HierarchyNode, error) {
	if err := ctx.Err(); err != nil {
		return model.HierarchyNode{}, err
	}
	if api == nil {
		return model.HierarchyNode{}, fmt.Errorf("crm: no API")
	}
	projects, err := api.SearchProjects(ctx)
	if err != nil {
		return model.HierarchyNode{}, err
	}
	if len(projects) == 0 {
		return model.HierarchyNode{}, fmt.Errorf("crm: no accessible projects")
	}

	byID := map[string]*node{}
	for _, p := range projects {
		if p.ID == "" {
			continue
		}
		id := "projects/" + p.ID
		n := &node{
			id:      id,
			name:    p.DisplayName,
			parent:  p.Parent,
			kind:    model.KindProject,
			number:  p.Number,
			created: p.Created,
			labels:  p.Labels,
		}
		if n.name == "" {
			n.name = p.ID
		}
		byID[id] = n
	}

	var failed []error
	for pending := unseenParents(byID, maps.Values(byID)); len(pending) > 0; {
		if err := ctx.Err(); err != nil {
			return model.HierarchyNode{}, fmt.Errorf("crm: ancestor lookups: %w", err)
		}
		found := make([]*node, len(pending))
		errs := make([]error, len(pending))
		var g errgroup.Group
		g.SetLimit(maxConcurrentLookups)
		for i, name := range pending {
			g.Go(func() error {
				found[i], errs[i] = lookup(ctx, api, name)
				return nil
			})
		}
		_ = g.Wait()
		for i, n := range found {
			byID[n.id] = n
			if err := errs[i]; err != nil && ctx.Err() == nil && (expected == nil || !expected(err)) {
				slog.Warn("crm: ancestor lookup failed; tree may be incomplete", "name", n.id, "err", err)
				failed = append(failed, fmt.Errorf("%s: %w", n.id, err))
			}
		}
		pending = unseenParents(byID, slices.Values(found))
	}
	// A lookup that failed because ctx ended left a stub; do not return it.
	if err := ctx.Err(); err != nil {
		return model.HierarchyNode{}, fmt.Errorf("crm: ancestor lookups: %w", err)
	}

	children := map[string][]*node{}
	var roots []*node
	for _, n := range byID {
		if n.parent == "" || byID[n.parent] == nil {
			roots = append(roots, n)
			continue
		}
		children[n.parent] = append(children[n.parent], n)
	}
	sortNodes(roots)
	for k := range children {
		sortNodes(children[k])
	}

	var conv func(*node) model.HierarchyNode
	conv = func(n *node) model.HierarchyNode {
		out := model.HierarchyNode{
			ID: n.id, Name: n.name, Kind: n.kind, ParentID: n.parent,
			Number: n.number, Created: n.created, Labels: n.labels,
		}
		for _, ch := range children[n.id] {
			kid := conv(ch)
			kid.ParentID = n.id
			out.Children = append(out.Children, kid)
		}
		return out
	}

	var partial error
	if len(failed) > 0 {
		partial = model.Partial(fmt.Errorf("could not read %d ancestors: %w", len(failed), errors.Join(failed...)))
	}
	if len(roots) == 1 {
		return conv(roots[0]), partial
	}
	root := model.HierarchyNode{ID: model.RootID, Name: "GCP", Kind: model.KindRoot}
	for _, r := range roots {
		ch := conv(r)
		ch.ParentID = root.ID
		root.Children = append(root.Children, ch)
	}
	return root, partial
}

// unseenParents returns the distinct parents of nodes that are not yet in
// byID, sorted.
func unseenParents(byID map[string]*node, nodes iter.Seq[*node]) []string {
	set := map[string]bool{}
	for n := range nodes {
		if n.parent != "" && byID[n.parent] == nil {
			set[n.parent] = true
		}
	}
	return slices.Sorted(maps.Keys(set))
}

// lookup reads one ancestor. A failed read yields a stub named by the last
// segment of name, with no parent, and the read's error.
func lookup(ctx context.Context, api API, name string) (*node, error) {
	n := &node{id: name, name: lastSeg(name)}
	switch {
	case strings.HasPrefix(name, "organizations/"):
		n.kind = model.KindOrg
		o, err := api.GetOrg(ctx, name)
		if err != nil {
			return n, err
		}
		if o.DisplayName != "" {
			n.name = o.DisplayName
		}
		n.created = o.Created
	case strings.HasPrefix(name, "folders/"):
		n.kind = model.KindFolder
		f, err := api.GetFolder(ctx, name)
		if err != nil {
			return n, err
		}
		if f.DisplayName != "" {
			n.name = f.DisplayName
		}
		n.parent = f.Parent
		n.created = f.Created
	default:
		n.kind = model.KindFolder
	}
	return n, nil
}

func sortNodes(ns []*node) {
	sort.Slice(ns, func(i, j int) bool {
		if ns[i].name == ns[j].name {
			return ns[i].id < ns[j].id
		}
		return ns[i].name < ns[j].name
	})
}

func lastSeg(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 && i+1 < len(name) {
		return name[i+1:]
	}
	return name
}
