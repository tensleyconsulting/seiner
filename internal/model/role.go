package model

import (
	"slices"
	"strconv"
	"strings"
)

const (
	// MetaRole is the role name (roles/viewer, projects/p/roles/r) on a
	// binding, a custom role, and a permission.
	MetaRole = "role"
	// ListPermissions is the permissions a permission group holds.
	ListPermissions = "permissions"
	// KindPermissionGroup is a synthetic node holding a role's permissions
	// that share a prefix (storage, or storage.buckets).
	KindPermissionGroup = "permission-group"
	// MetaRoleTitle and MetaRoleDescription are the granting role's title
	// and description on a permission, so the role's details can show
	// them once its permissions load.
	MetaRoleTitle       = "roleTitle"
	MetaRoleDescription = "roleDescription"
)

// RoleInfo names the role a permission comes from.
type RoleInfo struct {
	Name        string // roles/viewer, projects/p/roles/r
	Title       string
	Description string
}

// RolePermissionsFacet is the facet that lists a role's permissions. It
// is inline: the role's row expands straight to them.
func RolePermissionsFacet() Facet {
	return Facet{ID: FacetRolePermissions, Label: "Permissions", Inline: true}
}

// RoleOf is the role whose permissions r lists under
// FacetRolePermissions: a binding's role, or a custom role itself.
func RoleOf(r Resource) (string, bool) {
	switch {
	case r.IsBinding(), r.Service == ServiceCustomRole:
		role := r.Metadata[MetaRole]
		return role, role != ""
	}
	return "", false
}

// PermissionResource is the permission perm, granted by role, as a
// FacetRolePermissions child of parent. Its details show only the role's
// name; the title and description are for the role's own details.
func PermissionResource(parent Resource, role RoleInfo, perm string) Resource {
	md := map[string]string{MetaRole: role.Name}
	if role.Title != "" {
		md[MetaRoleTitle] = role.Title
	}
	if role.Description != "" {
		md[MetaRoleDescription] = role.Description
	}
	return Resource{
		ID:       parent.ID + "/permissions/" + perm,
		Name:     perm,
		Service:  ServiceIAMPermission,
		ScopeID:  parent.ScopeID,
		Location: "global",
		ParentID: parent.ID,
		FacetID:  FacetRolePermissions,
		Metadata: md,
		Fields:   []Field{{Key: MetaRole, Value: role.Name}},
	}
}

// WithRoleDetails is binding with the title and description of its role,
// read from perms, the role's loaded permissions. The title follows the
// role field; a binding has no description of its own. binding is
// returned unchanged when perms is empty.
func WithRoleDetails(binding Resource, perms []Resource) Resource {
	if len(perms) == 0 {
		return binding
	}
	md := perms[0].Metadata
	if binding.Description == "" {
		binding.Description = md[MetaRoleDescription]
	}
	if title := md[MetaRoleTitle]; title != "" {
		at := 0
		if i := slices.IndexFunc(binding.Fields, func(f Field) bool { return f.Key == MetaRole }); i >= 0 {
			at = i + 1
		}
		binding.Fields = slices.Insert(slices.Clone(binding.Fields), at, Field{Key: "title", Value: title})
	}
	return binding
}

// IsPermission reports whether r is a single IAM permission.
func (r Resource) IsPermission() bool {
	return r.Service == ServiceIAMPermission && r.Metadata[MetaKind] == ""
}

// IsPermissionGroup reports whether r is a synthetic node holding
// permissions that share a prefix.
func (r Resource) IsPermissionGroup() bool {
	return r.Metadata[MetaKind] == KindPermissionGroup
}

// PermissionNode is one row of a role's permission outline: a group with
// Children, or a single permission.
type PermissionNode struct {
	Resource Resource
	Children []PermissionNode
}

// permissionGroupMax is the most permissions listed flat. Past it they
// are grouped by service (storage), then by resource (storage.buckets).
const permissionGroupMax = 20

// GroupPermissions lays out perms, sorted by name, for one role. Up to
// permissionGroupMax list flat; more are grouped by service, and a service
// with more than that by resource. A level with a single group is skipped,
// since it would only add a row. Group ids start with base, which must be
// unique to where the role is shown.
func GroupPermissions(base string, perms []Resource) []PermissionNode {
	perms = slices.Clone(perms)
	slices.SortFunc(perms, func(a, b Resource) int { return strings.Compare(a.Name, b.Name) })
	return groupPermissions(base, perms, 1)
}

// groupPermissions groups perms by their first level name segments.
func groupPermissions(base string, perms []Resource, level int) []PermissionNode {
	if len(perms) <= permissionGroupMax || level > 2 {
		out := make([]PermissionNode, len(perms))
		for i, p := range perms {
			out[i] = PermissionNode{Resource: p}
		}
		return out
	}
	var prefixes []string
	by := map[string][]Resource{}
	for _, p := range perms {
		k := permissionPrefix(p.Name, level)
		if _, ok := by[k]; !ok {
			prefixes = append(prefixes, k)
		}
		by[k] = append(by[k], p)
	}
	if len(prefixes) == 1 {
		return groupPermissions(base, perms, level+1)
	}
	out := make([]PermissionNode, len(prefixes))
	for i, k := range prefixes {
		out[i] = PermissionNode{
			Resource: permissionGroupResource(base, k, by[k]),
			Children: groupPermissions(base, by[k], level+1),
		}
	}
	return out
}

// permissionPrefix is perm's first n dot-separated segments, never the
// whole name: storage.buckets.get is "storage" at 1 and
// "storage.buckets" at 2.
func permissionPrefix(perm string, n int) string {
	segs := strings.Split(perm, ".")
	n = min(n, len(segs)-1)
	return strings.Join(segs[:n], ".")
}

// PermissionGroupID is the id of base's group node for prefix.
func PermissionGroupID(base, prefix string) string {
	return base + "/permission-groups/" + prefix
}

// permissionGroupResource is the node for perms, which all share prefix.
// Its badge is the count, and it lists perms for the details pane.
func permissionGroupResource(base, prefix string, perms []Resource) Resource {
	n := strconv.Itoa(len(perms))
	names := make([]string, len(perms))
	for i, p := range perms {
		names[i] = p.Name
	}
	md := map[string]string{MetaKind: KindPermissionGroup, MetaRole: perms[0].Metadata[MetaRole], ListPermissions: n}
	return Resource{
		ID:       PermissionGroupID(base, prefix),
		Name:     prefix,
		Service:  ServiceIAMPermission,
		ScopeID:  perms[0].ScopeID,
		Location: "global",
		Badge:    n,
		ParentID: base,
		FacetID:  FacetRolePermissions,
		Metadata: md,
		Fields:   FieldsOf(md, MetaRole, ListPermissions),
		Lists:    []List{{ID: ListPermissions, Label: "Permissions", Items: names}},
	}
}
