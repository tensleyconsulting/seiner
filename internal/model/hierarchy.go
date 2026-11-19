package model

import (
	"cmp"
	"maps"
	"slices"
	"time"
)

// NodeKind is a CRM node type. It is not a GCP API enum.
type NodeKind string

const (
	KindRoot    NodeKind = "root"
	KindOrg     NodeKind = "organization"
	KindFolder  NodeKind = "folder"
	KindProject NodeKind = "project"
)

// RootID is the synthetic KindRoot node that wraps several top-level nodes.
const RootID = "cloud/root"

// HierarchyNode is one org, folder, or project in the resource manager tree.
// Children are CRM nodes only — project contents live in the resource list.
type HierarchyNode struct {
	ID       string
	Name     string
	Kind     NodeKind
	ParentID string
	// Number is the project number; "" for other kinds or when unknown.
	Number string
	// Created is the creation time; zero if unknown.
	Created time.Time
	Labels  map[string]string
	// Workload is the Assured Workload a folder belongs to; nil for other
	// folders and kinds.
	Workload *AssuredWorkload
	Children []HierarchyNode
}

// Find returns the node with the given id, including n itself.
func (n HierarchyNode) Find(id string) (HierarchyNode, bool) {
	if n.ID == id {
		return n, true
	}
	for _, ch := range n.Children {
		if found, ok := ch.Find(id); ok {
			return found, true
		}
	}
	return HierarchyNode{}, false
}

// PathTo returns the chain from n (inclusive) to id, or nil if missing.
func (n HierarchyNode) PathTo(id string) []HierarchyNode {
	if n.ID == id {
		return []HierarchyNode{n}
	}
	for _, ch := range n.Children {
		if rest := ch.PathTo(id); rest != nil {
			out := make([]HierarchyNode, 1+len(rest))
			out[0] = n
			copy(out[1:], rest)
			return out
		}
	}
	return nil
}

// ChildrenFoldersLast is n's children with its folders after the rest
// (projects), each keeping tree order, so a node's folders list together
// below everything else under it.
func (n HierarchyNode) ChildrenFoldersLast() []HierarchyNode {
	out := slices.Clone(n.Children)
	slices.SortStableFunc(out, func(a, b HierarchyNode) int {
		return cmp.Compare(boolRank(a.Kind == KindFolder), boolRank(b.Kind == KindFolder))
	})
	return out
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// FindScope matches a CRM name, or projects/{id} for a bare project id from
// the CLI.
func (n HierarchyNode) FindScope(id string) (HierarchyNode, bool) {
	return n.Find(ProjectScope(id))
}

// FirstProject is a depth-first search for a project node.
func (n HierarchyNode) FirstProject() (HierarchyNode, bool) {
	if n.Kind == KindProject {
		return n, true
	}
	for _, ch := range n.Children {
		if found, ok := ch.FirstProject(); ok {
			return found, true
		}
	}
	return HierarchyNode{}, false
}

// Resource maps a CRM node onto the UI resource model.
func (n HierarchyNode) Resource() Resource {
	svc := ServiceProject
	switch n.Kind {
	case KindOrg, KindRoot:
		svc = ServiceOrganization
	case KindFolder:
		svc = ServiceFolder
	}
	name := cleanLine(n.Name)
	md := map[string]string{MetaKind: string(n.Kind)}
	if id, ok := ProjectID(n.ID); ok && n.Kind == KindProject && id != name {
		md["projectId"] = id
	}
	if n.Number != "" {
		md["projectNumber"] = n.Number
	}
	if n.ParentID != "" && n.ParentID != RootID {
		md["parent"] = n.ParentID
	}
	r := Resource{
		ID:       n.ID,
		Name:     name,
		Service:  svc,
		ScopeID:  n.ID,
		Location: "global",
		ParentID: n.ParentID,
		Metadata: md,
		Created:  n.Created,
		Labels:   maps.Clone(n.Labels),
	}
	if n.Workload != nil && n.Kind == KindFolder {
		n.Workload.annotate(&r)
	}
	r.Fields = FieldsOf(md, append([]string{"projectId", "projectNumber", "parent"}, workloadFields...)...)
	return Normalize(r)
}

// IsScope reports whether r is an org, folder, or project node.
func (r Resource) IsScope() bool {
	spec, ok := Spec(r.Service)
	return ok && spec.Scope
}

// Breadcrumb joins node names with " / ".
func Breadcrumb(path []HierarchyNode) string {
	if len(path) == 0 {
		return ""
	}
	out := cleanLine(path[0].Name)
	for i := 1; i < len(path); i++ {
		out += " / " + cleanLine(path[i].Name)
	}
	return out
}
