package model

import (
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const (
	// ListMembers is an IAM binding's principals.
	ListMembers = "members"
	// ListRoles is the roles a principal holds.
	ListRoles = "roles"
	// KindPrincipal is a synthetic node for one IAM principal.
	KindPrincipal = "principal"
	// KindInherited is a synthetic node holding the bindings inherited
	// from one ancestor.
	KindInherited = "inherited"
	// MetaInheritedFrom is the CRM name of the ancestor an inherited
	// binding is set on.
	MetaInheritedFrom = "inheritedFrom"
	// MetaInheritedName is the display name of that ancestor, when known.
	MetaInheritedName = "inheritedFromName"
	// MetaInheritedLevel is how far up that ancestor is: 1 for the
	// scope's parent.
	MetaInheritedLevel = "inheritedLevel"
	// MetaUniqueID is a service account's numeric unique id.
	MetaUniqueID = "uniqueId"
)

// BindingResource is the IAM role binding of role to members on resource
// (a CRM name or a bucket's relative name). condition is the condition
// title, "" when unconditional. Callers set ParentID and FacetID for
// bindings that sit under another resource.
func BindingResource(resource, scopeID, role string, members []string, condition string) Resource {
	name := strings.TrimPrefix(role, "roles/")
	if condition != "" {
		name += " · " + condition
	}
	md := map[string]string{
		"role":      role,
		ListMembers: strconv.Itoa(len(members)),
	}
	fields := []Field{{Key: "role", Value: role}}
	if condition != "" {
		md["condition"] = condition
		fields = append(fields, Field{Key: "condition", Value: condition})
	}
	return Resource{
		ID:       BindingID(resource, role, condition),
		Name:     name,
		Service:  ServiceIAMBinding,
		ScopeID:  scopeID,
		Location: "global",
		Metadata: md,
		Fields:   fields,
		Lists:    []List{{ID: ListMembers, Label: "Members", Items: slices.Clone(members)}},
	}
}

// Ancestor is a CRM node above a scope, nearest first in a scope's
// ancestry. DisplayName is "" when it could not be read.
type Ancestor struct {
	Name        string // organizations/1 or folders/2
	DisplayName string
	Level       int // 1 for the scope's parent, 2 for its grandparent, …
}

// InheritedBindingResource is ancestor's binding of role to members as
// scopeID inherits it: listed under scopeID, with Origin set to the
// ancestor's kind so the explorer can mark where it comes from.
func InheritedBindingResource(scopeID string, ancestor Ancestor, role string, members []string, condition string) Resource {
	return InheritedBindingUnder(scopeID, scopeID, ancestor, ScopeService(ancestor.Name), role, members, condition)
}

// InheritedBindingUnder is ancestor's binding of role to members as the
// resource under (a scope, or a resource in scopeID) inherits it. origin
// marks the ancestor's kind in the explorer. Callers set ParentID and
// FacetID for bindings that sit under another resource.
func InheritedBindingUnder(under, scopeID string, ancestor Ancestor, origin ServiceType, role string, members []string, condition string) Resource {
	r := BindingResource(InheritedResource(under, ancestor.Name), scopeID, role, members, condition)
	r.Origin = origin
	r.Metadata[MetaInheritedFrom] = ancestor.Name
	r.Metadata[MetaInheritedLevel] = strconv.Itoa(ancestor.Level)
	if ancestor.DisplayName != "" {
		r.Metadata[MetaInheritedName] = ancestor.DisplayName
	}
	from := ancestor.Name
	if ancestor.DisplayName != "" && ancestor.DisplayName != ancestor.Name {
		from = ancestor.DisplayName + " (" + ancestor.Name + ")"
	}
	r.Fields = append(r.Fields, Field{Key: MetaInheritedFrom, Value: from})
	return r
}

// InheritedResource is the resource part of the id of a binding scope (or
// a resource) inherits from ancestor. It keeps scope as the id's prefix,
// so the binding sits in scope like any other.
func InheritedResource(scope, ancestor string) string {
	return scope + "/inherited/" + url.PathEscape(ancestor)
}

// IsInheritedGroup reports whether r is a synthetic node holding the
// bindings inherited from one ancestor.
func (r Resource) IsInheritedGroup() bool {
	return r.Metadata[MetaKind] == KindInherited
}

// InheritedGroup is the bindings inherited from one ancestor, under its
// synthetic node.
type InheritedGroup struct {
	Node     Resource
	Bindings []Resource
}

// SplitInherited separates the bindings set on the scope itself from the
// inherited ones, which it groups by ancestor, nearest first. Items keep
// their order within own and within each group; anything that is not an
// inherited binding stays in own. Group nodes sit under parent, the node
// the bindings are listed under.
func SplitInherited(parent Resource, items []Resource) (own []Resource, inherited []InheritedGroup) {
	at := map[string]int{}
	for _, r := range items {
		from := r.Metadata[MetaInheritedFrom]
		if !r.IsBinding() || from == "" {
			own = append(own, r)
			continue
		}
		i, ok := at[from]
		if !ok {
			i = len(inherited)
			at[from] = i
			inherited = append(inherited, InheritedGroup{})
		}
		inherited[i].Bindings = append(inherited[i].Bindings, r)
	}
	for i := range inherited {
		inherited[i].Node = inheritedGroupResource(parent, inherited[i].Bindings)
	}
	slices.SortStableFunc(inherited, func(a, b InheritedGroup) int {
		return inheritedLevel(a.Bindings[0]) - inheritedLevel(b.Bindings[0])
	})
	return own, inherited
}

// inheritedLevel is r's ancestor level, with unknown levels last.
func inheritedLevel(r Resource) int {
	if n, err := strconv.Atoi(r.Metadata[MetaInheritedLevel]); err == nil && n > 0 {
		return n
	}
	return math.MaxInt32
}

// inheritedGroupResource is the node for bindings, all inherited from one
// ancestor, under parent.
func inheritedGroupResource(parent Resource, bindings []Resource) Resource {
	first := bindings[0]
	from := first.Metadata[MetaInheritedFrom]
	label := first.Metadata[MetaInheritedName]
	if label == "" {
		label = from
	}
	n := strconv.Itoa(len(bindings))
	fromField := from
	if label != from {
		fromField = label + " (" + from + ")"
	}
	return Resource{
		ID:       parent.ID + "/inherited/" + url.PathEscape(from),
		Name:     "From " + label + " (" + n + ")",
		Service:  ServiceIAMBinding,
		ScopeID:  parent.ScopeID,
		Location: "global",
		ParentID: parent.ID,
		Origin:   first.Origin,
		Metadata: map[string]string{MetaKind: KindInherited, MetaInheritedFrom: from, "bindings": n},
		Fields:   []Field{{Key: MetaInheritedFrom, Value: fromField}, {Key: "bindings", Value: n}},
	}
}

// BindingID names the binding of role on resource. A conditional binding
// adds its condition title, so the same role bound with two conditions
// gets two ids.
func BindingID(resource, role, condition string) string {
	id := resource + "/iam/bindings/" + url.PathEscape(role)
	if condition != "" {
		id += "/conditions/" + url.PathEscape(condition)
	}
	return id
}

// UniqueBindingIDs gives each binding in rs whose ID an earlier one already
// took a "#2", "#3", … suffix. BindingID keys on the condition title, and
// two conditions on one role can share a title. The escaped title cannot
// contain '#', so a suffixed ID never meets a real one.
func UniqueBindingIDs(rs []Resource) {
	seen := map[string]int{}
	for i, r := range rs {
		if !r.IsBinding() {
			continue
		}
		seen[r.ID]++
		if n := seen[r.ID]; n > 1 {
			rs[i].ID = r.ID + "#" + strconv.Itoa(n)
		}
	}
}

// IsBinding reports whether r is an IAM role binding.
func (r Resource) IsBinding() bool {
	return r.Service == ServiceIAMBinding && r.Metadata[MetaKind] == ""
}

// IsPrincipal reports whether r is a synthetic principal node.
func (r Resource) IsPrincipal() bool {
	return r.Metadata[MetaKind] == KindPrincipal
}

// PrincipalGroup is one principal and the bindings that name it.
type PrincipalGroup struct {
	Principal Resource
	Bindings  []Resource
}

// GroupByPrincipal inverts bindings into one group per member, sorted by
// member (so user:, group:, and serviceAccount: principals cluster).
// Bindings keep their order within a group. Principal nodes sit under
// parent, the node the bindings are listed under. Non-bindings are skipped.
func GroupByPrincipal(parent Resource, bindings []Resource) []PrincipalGroup {
	by := map[string][]Resource{}
	for _, b := range bindings {
		if !b.IsBinding() {
			continue
		}
		for _, m := range b.List(ListMembers) {
			by[m] = append(by[m], b)
		}
	}
	members := make([]string, 0, len(by))
	for m := range by {
		members = append(members, m)
	}
	slices.SortFunc(members, func(a, b string) int {
		if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	out := make([]PrincipalGroup, len(members))
	for i, m := range members {
		out[i] = PrincipalGroup{Principal: PrincipalResource(parent, m, by[m]), Bindings: by[m]}
	}
	return out
}

// PrincipalResource is the node for member under parent, listing the roles
// it holds through bindings.
func PrincipalResource(parent Resource, member string, bindings []Resource) Resource {
	kind, ident := PrincipalType(member)
	md := map[string]string{MetaKind: KindPrincipal, "type": kind}
	fields := []Field{{Key: "type", Value: kind}}
	if ident != "" {
		md["principal"] = ident
		fields = append(fields, Field{Key: "principal", Value: ident})
	}
	md[ListRoles] = strconv.Itoa(len(bindings))
	roles := make([]string, len(bindings))
	for i, b := range bindings {
		roles[i] = b.Name
		if from := b.Metadata[MetaInheritedFrom]; from != "" {
			roles[i] += " (inherited from " + from + ")"
		}
	}
	return Resource{
		ID:       parent.ID + "/principals/" + url.PathEscape(member),
		Name:     member,
		Service:  ServiceIAMBinding,
		ScopeID:  parent.ScopeID,
		Location: "global",
		ParentID: parent.ID,
		Metadata: md,
		Fields:   fields,
		Lists:    []List{{ID: ListRoles, Label: "Roles", Items: roles}},
	}
}

// PrincipalType splits an IAM member into its type and identifier:
// user:a@x is ("user", "a@x"), allUsers is ("allUsers", ""), and
// deleted:user:a@x?uid=1 is ("deleted", "user:a@x?uid=1").
func PrincipalType(member string) (kind, ident string) {
	kind, ident, _ = strings.Cut(member, ":")
	return kind, ident
}
