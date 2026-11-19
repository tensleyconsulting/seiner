package model

import "testing"

func TestNestPrefixes(t *testing.T) {
	t.Parallel()
	owner := Resource{
		ID:      "projects/demo/buckets/b",
		Name:    "b",
		Service: ServiceCloudStorage,
		ScopeID: "demo",
	}
	items := []Resource{
		{ID: "o1", Name: "logos/logo.png", Service: ServiceCloudStorage, ParentID: owner.ID, FacetID: FacetObjects},
		{ID: "o2", Name: "logos/icons/app.png", Service: ServiceCloudStorage, ParentID: owner.ID, FacetID: FacetObjects},
		{ID: "o3", Name: "readme.txt", Service: ServiceCloudStorage, ParentID: owner.ID, FacetID: FacetObjects},
	}
	got := NestPrefixes(owner, items)
	if len(got) != 2 {
		t.Fatalf("top-level %d want 2 (folder + readme)", len(got))
	}
	if !got[0].Resource.IsPrefix() || got[0].Resource.Name != "logos" {
		t.Fatalf("folder %+v", got[0].Resource)
	}
	if got[0].Resource.Metadata[MetaPrefix] != "logos/" {
		t.Fatalf("prefix %q", got[0].Resource.Metadata[MetaPrefix])
	}
	if len(got[0].Children) != 2 {
		t.Fatalf("logos children %d", len(got[0].Children))
	}
	if got[0].Children[0].Resource.Name != "icons" || !got[0].Children[0].Resource.IsPrefix() {
		t.Fatalf("nested folder %+v", got[0].Children[0].Resource)
	}
	if got[0].Children[0].Children[0].Resource.Name != "app.png" {
		t.Fatalf("nested leaf %+v", got[0].Children[0].Children[0].Resource)
	}
	if got[0].Children[1].Resource.Name != "logo.png" {
		t.Fatalf("leaf %+v", got[0].Children[1].Resource)
	}
	if got[0].Children[1].Resource.Metadata[MetaPath] != "logos/logo.png" {
		t.Fatalf("path %q", got[0].Children[1].Resource.Metadata[MetaPath])
	}
	if got[1].Resource.Name != "readme.txt" || got[1].Resource.IsPrefix() {
		t.Fatalf("root leaf %+v", got[1].Resource)
	}
	if items[0].Name != "logos/logo.png" {
		t.Fatal("NestPrefixes must not mutate inputs")
	}
}

func TestIsPrefix(t *testing.T) {
	t.Parallel()
	if (Resource{}).IsPrefix() {
		t.Fatal("empty")
	}
	r := PrefixResource(Resource{ID: "b", Service: ServiceCloudStorage}, "a/", "a", "b")
	if !r.IsPrefix() || r.Name != "a" {
		t.Fatalf("%+v", r)
	}
}

func TestNestPrefixesAppendsPathField(t *testing.T) {
	t.Parallel()
	owner := Resource{ID: "projects/demo/buckets/b"}
	item := Normalize(Resource{
		ID: "o1", Name: "logos/logo.png", Service: ServiceCloudStorage, ParentID: owner.ID, FacetID: FacetObjects,
		Metadata: map[string]string{"size": "12"},
	})
	leaf := NestPrefixes(owner, []Resource{item})[0].Children[0].Resource
	want := []Field{{"size", "12"}, {MetaPath, "logos/logo.png"}}
	if len(leaf.Fields) != len(want) || leaf.Fields[0] != want[0] || leaf.Fields[1] != want[1] {
		t.Fatalf("fields %v want %v", leaf.Fields, want)
	}
	if len(item.Fields) != 1 {
		t.Fatalf("input fields mutated: %v", item.Fields)
	}
}

func TestIsSynthetic(t *testing.T) {
	t.Parallel()
	project := Resource{ID: "projects/p", Service: ServiceProject}
	bucket := Resource{ID: "projects/p/buckets/b", Service: ServiceCloudStorage}
	tests := []struct {
		name string
		r    Resource
		want bool
	}{
		{"project", project, false},
		{"bucket", bucket, false},
		{"collection", CollectionResource(project, ServiceCloudStorage, 1), true},
		{"product", ProductGroupResource(project, ServiceCloudStorage, 1), true},
		{"enabled services", EnabledServicesResource(project), true},
		{"prefix", PrefixResource(bucket, "a/", "a", bucket.ID), true},
	}
	for _, tc := range tests {
		if got := tc.r.IsSynthetic(); got != tc.want {
			t.Errorf("%s: IsSynthetic=%v, want %v", tc.name, got, tc.want)
		}
	}
}
