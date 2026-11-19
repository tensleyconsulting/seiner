package model

import (
	"fmt"
	"strings"
	"testing"
)

// sketch draws nodes as "Name(badge)[children]", leaves as their names.
func sketch(nodes []BucketNode) string {
	parts := make([]string, len(nodes))
	for i, n := range nodes {
		if !n.Resource.IsBucket() {
			parts[i] = n.Resource.Name
			continue
		}
		parts[i] = fmt.Sprintf("%s(%s)[%s]", n.Resource.Name, n.Resource.Badge, sketch(n.Children))
	}
	return strings.Join(parts, " ")
}

func wsUser(email, ou string) Resource {
	return Resource{
		ID:       "organizations/1/users/" + email,
		Name:     email,
		Service:  ServiceWorkspaceUser,
		ScopeID:  "organizations/1",
		Metadata: map[string]string{MetaOrgUnit: ou},
	}
}

func wsGroup(email string) Resource {
	return Resource{ID: "organizations/1/groups/" + email, Name: email, Service: ServiceWorkspaceGroup, ScopeID: "organizations/1"}
}

// bucketItems is n resources named prefix0, prefix1, … made by mk.
func bucketItems(prefix string, n int, mk func(string) Resource) []Resource {
	out := make([]Resource, n)
	for i := range n {
		out[i] = mk(fmt.Sprintf("%s%02d", prefix, i))
	}
	return out
}

func TestBuckets(t *testing.T) {
	t.Parallel()
	inRoot := func(name string) Resource { return wsUser(name, "/") }
	cases := []struct {
		name  string
		items []Resource
		want  string
	}{
		{name: "empty", want: ""},
		{
			name:  "few list flat, sorted",
			items: []Resource{wsGroup("b"), wsGroup("A"), wsGroup("c")},
			want:  "A b c",
		},
		{
			name:  "many by first letter",
			items: append(bucketItems("a", 30, wsGroup), bucketItems("b", 30, wsGroup)...),
			want:  "A(30)[" + sketch(leaves(bucketItems("a", 30, wsGroup))) + "] B(30)[" + sketch(leaves(bucketItems("b", 30, wsGroup))) + "]",
		},
		{
			name:  "one letter goes to two",
			items: append(bucketItems("ja", 30, wsGroup), bucketItems("jo", 30, wsGroup)...),
			want:  "Ja(30)[" + sketch(leaves(bucketItems("ja", 30, wsGroup))) + "] Jo(30)[" + sketch(leaves(bucketItems("jo", 30, wsGroup))) + "]",
		},
		{
			name:  "past two letters lists flat",
			items: bucketItems("jan", 60, wsGroup),
			want:  sketch(leaves(bucketItems("jan", 60, wsGroup))),
		},
		{
			name: "org units, root items in a / folder",
			items: []Resource{
				wsUser("root-b", "/"), wsUser("root-a", ""),
				wsUser("eng-b", "/Eng"), wsUser("eng-a", "/Eng"),
				wsUser("plat", "/Eng/Platform"),
				wsUser("sales", "/sales"),
			},
			want: "Eng(3)[Platform(1)[plat] eng-a eng-b] sales(1)[sales] /(2)[root-a root-b]",
		},
		{
			name:  "everyone in the root lists flat",
			items: []Resource{wsUser("b", "/"), wsUser("a", "/")},
			want:  "a b",
		},
		{
			name:  "a crowded org unit gets letters",
			items: append(bucketItems("a", 30, inRoot), append(bucketItems("b", 30, inRoot), wsUser("x", "/Eng"))...),
			want:  "Eng(1)[x] /(60)[A(30)[" + sketch(leaves(bucketItems("a", 30, inRoot))) + "] B(30)[" + sketch(leaves(bucketItems("b", 30, inRoot))) + "]]",
		},
	}
	coll := CollectionResource(Resource{ID: "organizations/1", ScopeID: "organizations/1"}, ServiceWorkspaceUser, 0)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := sketch(Buckets(coll, "", tc.items)); got != tc.want {
				t.Errorf("Buckets =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func leaves(items []Resource) []BucketNode {
	out := make([]BucketNode, len(items))
	for i, r := range items {
		out[i] = BucketNode{Resource: r}
	}
	return out
}

func TestBucketNodes(t *testing.T) {
	t.Parallel()
	group := Resource{ID: "organizations/1/groups/g1", Name: "all@example.com", Service: ServiceWorkspaceGroup, ScopeID: "organizations/1", Location: "global"}
	member := func(name string) Resource {
		return Resource{ID: group.ID + "/memberships/" + name, Name: name + "@example.com", Service: ServiceWorkspaceMember, ScopeID: group.ScopeID, ParentID: group.ID, FacetID: FacetMembers}
	}
	members := append(bucketItems("m", 40, member), bucketItems("z", 15, member)...)
	nodes := Buckets(group, FacetMembers, members)
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes: %s", len(nodes), sketch(nodes))
	}
	seen := map[string]bool{}
	for _, n := range nodes {
		r := n.Resource
		if !r.IsBucket() || !r.IsSynthetic() {
			t.Errorf("%s is not a synthetic bucket", r.Name)
		}
		if r.Service != ServiceWorkspaceMember || r.ParentID != group.ID || r.FacetID != FacetMembers || r.ScopeID != group.ScopeID {
			t.Errorf("bucket %+v does not sit under the group's members", r)
		}
		if !strings.HasPrefix(r.ID, group.ID+"/buckets/") || seen[r.ID] {
			t.Errorf("bucket id %q is not unique under the group", r.ID)
		}
		seen[r.ID] = true
		if r.Badge != fmt.Sprint(len(n.Children)) {
			t.Errorf("%s badge %q, %d children", r.Name, r.Badge, len(n.Children))
		}
		if got := r.Fields[len(r.Fields)-1]; got.Key != "members" || got.Value != r.Badge {
			t.Errorf("%s count field %+v", r.Name, got)
		}
		for _, c := range n.Children {
			if c.Resource.ParentID != group.ID {
				t.Errorf("leaf %s ParentID changed to %q", c.Resource.Name, c.Resource.ParentID)
			}
		}
	}
	if nodes[0].Resource.Fields[0] != (Field{Key: "startsWith", Value: "M"}) {
		t.Errorf("letter field %+v", nodes[0].Resource.Fields[0])
	}
}

func TestBucketsKeepsInput(t *testing.T) {
	t.Parallel()
	items := []Resource{wsGroup("b"), wsGroup("a")}
	Buckets(Resource{ID: "c"}, "", items)
	if items[0].Name != "b" {
		t.Error("Buckets sorted its input")
	}
}
