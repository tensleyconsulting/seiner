package model

import (
	"slices"
	"testing"
)

func TestBindingID(t *testing.T) {
	t.Parallel()
	cases := []struct{ resource, role, condition, want string }{
		{"projects/p", "roles/owner", "", "projects/p/iam/bindings/roles%2Fowner"},
		{"organizations/1", "organizations/1/roles/custom", "", "organizations/1/iam/bindings/organizations%2F1%2Froles%2Fcustom"},
		{"projects/p", "roles/viewer", "Business hours", "projects/p/iam/bindings/roles%2Fviewer/conditions/Business%20hours"},
		{"projects/p/buckets/b", "roles/storage.admin", "", "projects/p/buckets/b/iam/bindings/roles%2Fstorage.admin"},
	}
	for _, tc := range cases {
		if got := BindingID(tc.resource, tc.role, tc.condition); got != tc.want {
			t.Errorf("BindingID(%q, %q, %q) = %q, want %q", tc.resource, tc.role, tc.condition, got, tc.want)
		}
	}
}

func TestUniqueBindingIDs(t *testing.T) {
	t.Parallel()
	rs := []Resource{
		BindingResource("projects/p", "projects/p", "roles/viewer", []string{"user:a@x"}, "Business hours"),
		BindingResource("projects/p", "projects/p", "roles/viewer", []string{"user:b@x"}, "Business hours"),
		BindingResource("projects/p", "projects/p", "roles/viewer", []string{"user:c@x"}, "Business hours"),
		BindingResource("projects/p", "projects/p", "roles/owner", []string{"user:a@x"}, ""),
		{ID: "projects/p/x", Service: ServiceCloudStorage},
		{ID: "projects/p/x", Service: ServiceCloudStorage},
	}
	UniqueBindingIDs(rs)
	base := BindingID("projects/p", "roles/viewer", "Business hours")
	want := []string{base, base + "#2", base + "#3", BindingID("projects/p", "roles/owner", ""), "projects/p/x", "projects/p/x"}
	var got []string
	for _, r := range rs {
		got = append(got, r.ID)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("ids = %q\nwant %q", got, want)
	}
}

func TestGroupByPrincipal(t *testing.T) {
	t.Parallel()
	parent := Resource{ID: "projects/p/collections/iam-binding", ScopeID: "projects/p"}
	owner := BindingResource("projects/p", "projects/p", "roles/owner", []string{"user:bob@x", "group:ops@x"}, "")
	viewer := BindingResource("projects/p", "projects/p", "roles/viewer", []string{"user:bob@x", "allUsers"}, "")
	got := GroupByPrincipal(parent, []Resource{owner, viewer, {ID: "x", Name: "not a binding"}})

	var names []string
	for _, g := range got {
		names = append(names, g.Principal.Name)
	}
	if want := []string{"allUsers", "group:ops@x", "user:bob@x"}; !slices.Equal(names, want) {
		t.Fatalf("principals %v, want %v", names, want)
	}
	bob := got[2]
	if !bob.Principal.IsPrincipal() || bob.Principal.ParentID != parent.ID {
		t.Fatalf("%+v", bob.Principal)
	}
	if len(bob.Bindings) != 2 || bob.Bindings[0].ID != owner.ID || bob.Bindings[1].ID != viewer.ID {
		t.Fatalf("bob's bindings %+v", bob.Bindings)
	}
	if roles := bob.Principal.List(ListRoles); !slices.Equal(roles, []string{"owner", "viewer"}) {
		t.Fatalf("roles %v", roles)
	}
	if err := Normalize(bob.Principal).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPrincipalType(t *testing.T) {
	t.Parallel()
	cases := []struct{ member, kind, ident string }{
		{"user:a@x", "user", "a@x"},
		{"serviceAccount:sa@p.iam.gserviceaccount.com", "serviceAccount", "sa@p.iam.gserviceaccount.com"},
		{"allUsers", "allUsers", ""},
		{"deleted:user:a@x?uid=1", "deleted", "user:a@x?uid=1"},
	}
	for _, tc := range cases {
		kind, ident := PrincipalType(tc.member)
		if kind != tc.kind || ident != tc.ident {
			t.Errorf("%s: %q %q", tc.member, kind, ident)
		}
	}
}

func TestMatchesSearchesLists(t *testing.T) {
	t.Parallel()
	r := BindingResource("projects/p", "projects/p", "roles/owner", []string{"user:alice@example.com"}, "")
	if !r.Matches("alice") {
		t.Error("member should match")
	}
}

func TestInheritedBindingResource(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ancestor Ancestor
		origin   ServiceType
		from     string
	}{
		{Ancestor{Name: "folders/2", DisplayName: "prod"}, ServiceFolder, "prod (folders/2)"},
		{Ancestor{Name: "organizations/1", DisplayName: "example.com"}, ServiceOrganization, "example.com (organizations/1)"},
		{Ancestor{Name: "folders/3"}, ServiceFolder, "folders/3"},
	}
	for _, tc := range cases {
		r := InheritedBindingResource("projects/p", tc.ancestor, "roles/owner", []string{"user:a"}, "")
		if r.Origin != tc.origin || r.ScopeID != "projects/p" || r.Name != "owner" || !r.IsBinding() {
			t.Errorf("%s: %+v", tc.ancestor.Name, r)
		}
		if p, ok := ParseName(r.ID); !ok || p.Scope != "projects/p" {
			t.Errorf("%s: id %s is not in projects/p", tc.ancestor.Name, r.ID)
		}
		if got := r.Fields[len(r.Fields)-1]; got != (Field{Key: MetaInheritedFrom, Value: tc.from}) {
			t.Errorf("%s: last field %v", tc.ancestor.Name, got)
		}
		if want := BindingID(InheritedResource("projects/p", tc.ancestor.Name), "roles/owner", ""); r.ID != want {
			t.Errorf("%s: id %s, want %s", tc.ancestor.Name, r.ID, want)
		}
	}
	direct := BindingResource("projects/p", "projects/p", "roles/owner", nil, "")
	if direct.Origin != "" {
		t.Errorf("direct binding has origin %q", direct.Origin)
	}
}

func TestPrincipalRolesNameInheritance(t *testing.T) {
	t.Parallel()
	own := BindingResource("projects/p", "projects/p", "roles/viewer", []string{"user:a"}, "")
	inh := InheritedBindingResource("projects/p", Ancestor{Name: "folders/2"}, "roles/editor", []string{"user:a"}, "")
	g := GroupByPrincipal(Resource{ID: "projects/p/collections/iam-binding", ScopeID: "projects/p"}, []Resource{own, inh})
	if len(g) != 1 {
		t.Fatalf("%+v", g)
	}
	want := []string{"viewer", "editor (inherited from folders/2)"}
	if got := g[0].Principal.List(ListRoles); !slices.Equal(got, want) {
		t.Errorf("roles %v, want %v", got, want)
	}
}

func TestSplitInherited(t *testing.T) {
	t.Parallel()
	parent := Resource{ID: "projects/p/collections/iam-binding", ScopeID: "projects/p"}
	org := Ancestor{Name: "organizations/1", DisplayName: "example.com", Level: 2}
	folder := Ancestor{Name: "folders/2", Level: 1}
	items := []Resource{
		InheritedBindingResource("projects/p", org, "roles/billing.admin", []string{"user:a"}, ""),
		BindingResource("projects/p", "projects/p", "roles/owner", []string{"user:a"}, ""),
		InheritedBindingResource("projects/p", folder, "roles/editor", []string{"user:b"}, ""),
		{ID: "x", Name: "not a binding", Metadata: map[string]string{MetaKind: KindPrincipal, MetaInheritedFrom: "folders/2"}},
		InheritedBindingResource("projects/p", org, "roles/viewer", []string{"user:c"}, ""),
	}
	own, groups := SplitInherited(parent, items)
	if len(own) != 2 || own[0].Name != "owner" || own[1].ID != "x" {
		t.Errorf("own %+v", own)
	}
	want := []struct {
		name     string
		origin   ServiceType
		bindings []string
		from     string
	}{
		{"From folders/2 (1)", ServiceFolder, []string{"editor"}, "folders/2"},
		{"From example.com (2)", ServiceOrganization, []string{"billing.admin", "viewer"}, "example.com (organizations/1)"},
	}
	if len(groups) != len(want) {
		t.Fatalf("groups %+v", groups)
	}
	for i, w := range want {
		g := groups[i]
		var names []string
		for _, b := range g.Bindings {
			names = append(names, b.Name)
		}
		if g.Node.Name != w.name || g.Node.Origin != w.origin || !slices.Equal(names, w.bindings) {
			t.Errorf("[%d] %q %q %v, want %q %q %v", i, g.Node.Name, g.Node.Origin, names, w.name, w.origin, w.bindings)
		}
		if !g.Node.IsInheritedGroup() || g.Node.IsBinding() || g.Node.ParentID != parent.ID || g.Node.ScopeID != "projects/p" {
			t.Errorf("[%d] node %+v", i, g.Node)
		}
		if g.Node.Fields[0] != (Field{Key: MetaInheritedFrom, Value: w.from}) {
			t.Errorf("[%d] fields %v", i, g.Node.Fields)
		}
	}
	if groups[0].Node.ID == groups[1].Node.ID {
		t.Error("groups share an id")
	}
}
