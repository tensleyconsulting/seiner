package model

import (
	"reflect"
	"testing"
	"time"
)

func sampleTree() HierarchyNode {
	return HierarchyNode{
		ID:   "organizations/example-org",
		Name: "example-org",
		Kind: KindOrg,
		Children: []HierarchyNode{
			{
				ID:       "folders/platform",
				Name:     "platform",
				Kind:     KindFolder,
				ParentID: "organizations/example-org",
				Children: []HierarchyNode{
					{
						ID:       "folders/prod",
						Name:     "prod",
						Kind:     KindFolder,
						ParentID: "folders/platform",
						Children: []HierarchyNode{
							{
								ID:       "folders/prod-apps",
								Name:     "prod-apps",
								Kind:     KindFolder,
								ParentID: "folders/prod",
								Children: []HierarchyNode{
									{ID: "projects/demo-project", Name: "demo-project", Kind: KindProject, ParentID: "folders/prod-apps"},
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestPathToDepth(t *testing.T) {
	t.Parallel()
	root := sampleTree()
	path := root.PathTo("projects/demo-project")
	if len(path) != 5 {
		t.Fatalf("depth=%d want 5", len(path))
	}
	if Breadcrumb(path) != "example-org / platform / prod / prod-apps / demo-project" {
		t.Fatalf("breadcrumb=%q", Breadcrumb(path))
	}
	if _, ok := root.Find("missing"); ok {
		t.Fatal("missing should not be found")
	}
	p, ok := root.FirstProject()
	if !ok || p.ID != "projects/demo-project" {
		t.Fatalf("first project %+v", p)
	}
}

func TestFindScope(t *testing.T) {
	t.Parallel()
	root := HierarchyNode{
		ID:   "crm",
		Kind: KindRoot,
		Children: []HierarchyNode{
			{ID: "projects/prj-ops", Name: "ops", Kind: KindProject, ParentID: "crm"},
		},
	}
	n, ok := root.FindScope("prj-ops")
	if !ok || n.ID != "projects/prj-ops" {
		t.Fatalf("%+v ok=%v", n, ok)
	}
	if n, ok := root.FindScope("projects/prj-ops"); !ok || n.Name != "ops" {
		t.Fatalf("full name: %+v ok=%v", n, ok)
	}
	if _, ok := root.FindScope("missing"); ok {
		t.Fatal("missing")
	}
}

func TestNodeResource(t *testing.T) {
	t.Parallel()
	n := HierarchyNode{ID: "organizations/example-org", Name: "example-org", Kind: KindOrg}
	r := n.Resource()
	if r.Service != ServiceOrganization || !r.IsScope() {
		t.Fatalf("%+v", r)
	}
	f := HierarchyNode{ID: "folders/platform", Name: "platform", Kind: KindFolder, ParentID: "organizations/example-org"}
	fr := f.Resource()
	if fr.Service != ServiceFolder || fr.ParentID != "organizations/example-org" {
		t.Fatalf("%+v", fr)
	}
}

func TestNodeResourceDetails(t *testing.T) {
	t.Parallel()
	created := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		n    HierarchyNode
		want []Field
	}{
		{"project with display name", HierarchyNode{ID: "projects/prj-a", Name: "Alpha", Kind: KindProject, ParentID: "folders/1", Number: "111"},
			[]Field{{"projectId", "prj-a"}, {"projectNumber", "111"}, {"parent", "folders/1"}}},
		{"project named by id", HierarchyNode{ID: "projects/prj-a", Name: "prj-a", Kind: KindProject},
			nil},
		{"top-level under synthetic root", HierarchyNode{ID: "organizations/1", Name: "example", Kind: KindOrg, ParentID: RootID},
			nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.n.Created = created
			r := tc.n.Resource()
			if !reflect.DeepEqual(r.Fields, tc.want) {
				t.Errorf("fields %v, want %v", r.Fields, tc.want)
			}
			if r.Description != "" {
				t.Errorf("description %q repeats the service label", r.Description)
			}
			if !r.Created.Equal(created) {
				t.Errorf("created %v", r.Created)
			}
		})
	}
}
