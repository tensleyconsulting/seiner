package model

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// permissionTree draws nodes one per line, groups as "name (count)" and
// children indented two spaces.
func permissionTree(nodes []PermissionNode, indent string) []string {
	var out []string
	for _, n := range nodes {
		if n.Resource.IsPermissionGroup() {
			out = append(out, fmt.Sprintf("%s%s (%s)", indent, n.Resource.Name, n.Resource.Badge))
			out = append(out, permissionTree(n.Children, indent+"  ")...)
			continue
		}
		out = append(out, indent+n.Resource.Name)
	}
	return out
}

// numbered is service.resource.verbN for n verbs.
func numbered(service, resource string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s.%s.verb%02d", service, resource, i)
	}
	return out
}

func TestGroupPermissions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		perms []string
		want  []string
	}{
		{
			name:  "few list flat and sorted",
			perms: []string{"storage.objects.list", "iam.roles.get", "storage.buckets.get"},
			want:  []string{"iam.roles.get", "storage.buckets.get", "storage.objects.list"},
		},
		{
			name:  "many split by service; a small service lists flat",
			perms: slices.Concat(numbered("compute", "instances", 15), []string{"iam.roles.get", "iam.roles.list"}, numbered("storage", "objects", 5)),
			want: slices.Concat(
				[]string{"compute (15)"}, indent(numbered("compute", "instances", 15)),
				[]string{"iam (2)", "  iam.roles.get", "  iam.roles.list"},
				[]string{"storage (5)"}, indent(numbered("storage", "objects", 5)),
			),
		},
		{
			name:  "a large service splits by resource",
			perms: slices.Concat(numbered("compute", "disks", 12), numbered("compute", "instances", 12), []string{"iam.roles.get"}),
			want: slices.Concat(
				[]string{"compute (24)", "  compute.disks (12)"}, indent(indent(numbered("compute", "disks", 12))),
				[]string{"  compute.instances (12)"}, indent(indent(numbered("compute", "instances", 12))),
				[]string{"iam (1)", "  iam.roles.get"},
			),
		},
		{
			name:  "one service skips the service level",
			perms: slices.Concat(numbered("storage", "buckets", 11), numbered("storage", "objects", 11)),
			want: slices.Concat(
				[]string{"storage.buckets (11)"}, indent(numbered("storage", "buckets", 11)),
				[]string{"storage.objects (11)"}, indent(numbered("storage", "objects", 11)),
			),
		},
		{
			name:  "one resource lists flat",
			perms: numbered("storage", "objects", 30),
			want:  numbered("storage", "objects", 30),
		},
	}
	parent := Resource{ID: "projects/p/roles/r", ScopeID: "projects/p"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			perms := make([]Resource, len(tc.perms))
			for i, p := range tc.perms {
				perms[len(perms)-1-i] = PermissionResource(parent, RoleInfo{Name: "projects/p/roles/r"}, p) // reversed: the layout sorts
			}
			got := permissionTree(GroupPermissions("base", perms), "")
			if !slices.Equal(got, tc.want) {
				t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

func indent(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = "  " + l
	}
	return out
}

func TestPermissionGroupResource(t *testing.T) {
	t.Parallel()
	parent := Resource{ID: "projects/p/roles/r", ScopeID: "projects/p"}
	var perms []Resource
	for _, p := range slices.Concat(numbered("compute", "instances", 21), []string{"iam.roles.get"}) {
		perms = append(perms, PermissionResource(parent, RoleInfo{Name: "roles/viewer", Title: "Viewer"}, p))
	}
	nodes := GroupPermissions("base", perms)
	g := nodes[0].Resource
	if !g.IsPermissionGroup() || g.IsPermission() || g.ID != PermissionGroupID("base", "compute") || g.ParentID != "base" || g.FacetID != FacetRolePermissions || g.ScopeID != "projects/p" {
		t.Errorf("group %+v", g)
	}
	if g.Metadata[MetaRole] != "roles/viewer" || g.Metadata[ListPermissions] != "21" || len(g.List(ListPermissions)) != 21 {
		t.Errorf("group metadata %v, list %v", g.Metadata, g.List(ListPermissions))
	}
	leaf := nodes[1].Children[0].Resource
	if !leaf.IsPermission() || leaf.ParentID != parent.ID || leaf.FacetID != FacetRolePermissions || leaf.Metadata[MetaRole] != "roles/viewer" {
		t.Errorf("leaf %+v", leaf)
	}
}

func TestRoleOf(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		r    Resource
		want string
	}{
		{"binding", BindingResource("projects/p", "projects/p", "roles/viewer", nil, ""), "roles/viewer"},
		{"custom role", Resource{ID: "projects/p/roles/r", Service: ServiceCustomRole, Metadata: map[string]string{MetaRole: "projects/p/roles/r"}}, "projects/p/roles/r"},
		{"principal", Resource{Service: ServiceIAMBinding, Metadata: map[string]string{MetaKind: KindPrincipal, MetaRole: "roles/viewer"}}, ""},
		{"other", Resource{Service: ServiceCloudStorage}, ""},
	}
	for _, tc := range cases {
		if got, ok := RoleOf(tc.r); got != tc.want || ok != (tc.want != "") {
			t.Errorf("%s: %q %v", tc.name, got, ok)
		}
	}
}

func TestWithRoleDetails(t *testing.T) {
	t.Parallel()
	b := BindingResource("projects/p", "projects/p", "roles/viewer", nil, "Business hours")
	perm := PermissionResource(b, RoleInfo{Name: "roles/viewer", Title: "Viewer", Description: "Read access"}, "compute.instances.get")
	if perm.Metadata[MetaRoleTitle] != "Viewer" || len(perm.Fields) != 1 {
		t.Fatalf("permission %+v", perm)
	}
	got := WithRoleDetails(b, []Resource{perm})
	want := []Field{{Key: "role", Value: "roles/viewer"}, {Key: "title", Value: "Viewer"}, {Key: "condition", Value: "Business hours"}}
	if !slices.Equal(got.Fields, want) || got.Description != "Read access" {
		t.Errorf("fields %v, description %q", got.Fields, got.Description)
	}
	if len(b.Fields) != 2 {
		t.Errorf("binding's fields changed: %v", b.Fields)
	}
	if got := WithRoleDetails(b, nil); !slices.Equal(got.Fields, b.Fields) || got.Description != "" {
		t.Errorf("nothing loaded: %+v", got)
	}
}
