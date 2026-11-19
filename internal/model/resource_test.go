package model

import "testing"

func TestChildrenByFacet(t *testing.T) {
	t.Parallel()
	rs := []Resource{
		{ID: "obj", Name: "obj", Service: ServiceCloudStorage, ParentID: "b", FacetID: FacetObjects},
		{ID: "iam", Name: "iam", Service: ServiceIAM, ParentID: "b", FacetID: FacetPermissions},
	}
	got := Children(rs, "b", FacetObjects)
	if len(got) != 1 || got[0].Name != "obj" {
		t.Fatalf("%+v", got)
	}
}

func TestRootsHidesNested(t *testing.T) {
	t.Parallel()
	rs := []Resource{
		{ID: "cluster", Name: "cluster", Service: ServiceGKE, ParentID: ""},
		{ID: "pool", Name: "pool", Service: ServiceGKE, ParentID: "cluster"},
	}
	got := Roots(rs)
	if len(got) != 1 || got[0].Name != "cluster" {
		t.Fatalf("%+v", got)
	}
}

func TestMatchesLabelsStateAndFields(t *testing.T) {
	t.Parallel()
	r := Resource{
		Name:   "web-1",
		State:  "TERMINATED",
		Labels: map[string]string{"team": "payments"},
		Fields: []Field{{Key: "machineType", Value: "e2-medium"}},
		// Internal keys and values do not match.
		Metadata: map[string]string{MetaKind: KindCollection},
	}
	cases := []struct {
		query string
		want  bool
	}{
		{"terminated", true},
		{"payments", true},
		{"team", true},
		{"e2-medium", true},
		{"running", false},
		{"collection", false},
		{"kind", false},
	}
	for _, tc := range cases {
		if got := r.Matches(tc.query); got != tc.want {
			t.Errorf("Matches(%q)=%v want %v", tc.query, got, tc.want)
		}
	}

	// Non-ASCII case-folding still works via the Unicode fallback.
	intl := Resource{Name: "München-Cluster", Description: "Zürich"}
	if !intl.Matches("münchen") || !intl.Matches("ZÜRICH") {
		t.Error("Unicode case-insensitive match failed")
	}
}

// Not parallel: testing.AllocsPerRun panics when parallel tests are active.
func TestMatchesZeroAllocs(t *testing.T) {
	r := Resource{
		Name:    "web-1",
		State:   "TERMINATED",
		Service: ServiceComputeEngine,
		Labels:  map[string]string{"team": "payments"},
		Fields:  []Field{{Key: "machineType", Value: "e2-medium"}},
	}
	// Warm the sync.OnceValue service registry before measuring.
	_ = r.Matches("warmup")
	allocs := testing.AllocsPerRun(100, func() {
		_ = r.Matches("e2-medium")
		_ = r.Matches("nonexistent-query")
	})
	if allocs != 0 {
		t.Errorf("ASCII Matches allocated %.1f times per run, want 0", allocs)
	}
}

func TestInlineFacet(t *testing.T) {
	t.Parallel()
	inline := Facet{ID: "a", Label: "A", Inline: true}
	cases := []struct {
		name   string
		facets []Facet
		want   bool
	}{
		{"none", nil, false},
		{"sole inline", []Facet{inline}, true},
		{"sole regular", []Facet{{ID: "a", Label: "A"}}, false},
		{"inline among several", []Facet{inline, {ID: "b", Label: "B"}}, false},
	}
	for _, tc := range cases {
		f, ok := Resource{Facets: tc.facets}.InlineFacet()
		if ok != tc.want || (ok && f.ID != "a") {
			t.Errorf("%s: InlineFacet = %+v, %v", tc.name, f, ok)
		}
	}
}
