package model

import "testing"

func TestProjectID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in string
		id string
		ok bool
	}{
		{"demo-project", "demo-project", true},
		{"projects/demo-project", "demo-project", true},
		{"organizations/123", "", false},
		{"folders/456", "", false},
		{"projects/", "", false},
		{"projects/p/buckets/b", "", false},
		{"crm", "crm", true},
		{"", "", false},
	}
	for _, tc := range cases {
		id, ok := ProjectID(tc.in)
		if id != tc.id || ok != tc.ok {
			t.Errorf("%q -> %q %v want %q %v", tc.in, id, ok, tc.id, tc.ok)
		}
	}
}

func TestProjectScope(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"demo-project", "projects/demo-project"},
		{" demo-project ", "projects/demo-project"},
		{"projects/demo-project", "projects/demo-project"},
		{"organizations/1", "organizations/1"},
		{"folders/2", "folders/2"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := ProjectScope(tc.in); got != tc.want {
			t.Errorf("ProjectScope(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestScopeKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want NodeKind
	}{
		{"organizations/1", KindOrg},
		{"folders/2", KindFolder},
		{"projects/p", KindProject},
		{"projects/", ""},
		{"projects/p/buckets/b", ""},
		{"demo-project", ""},
		{"cloud/root", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := ScopeKind(tc.in); got != tc.want {
			t.Errorf("ScopeKind(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestIAMResourceName(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"projects/x", "projects/x"},
		{"organizations/123", "organizations/123"},
		{"folders/456", "folders/456"},
		{"demo-project", ""},
		{"example-org", ""},
		{"cloud/root", ""},
		{"projects/x/buckets/b", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := IAMResourceName(tc.in); got != tc.want {
			t.Errorf("IAMResourceName(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestScopeNode(t *testing.T) {
	t.Parallel()
	cases := map[string]HierarchyNode{
		"my-proj":           {ID: "projects/my-proj", Name: "my-proj", Kind: KindProject},
		"projects/my-proj":  {ID: "projects/my-proj", Name: "my-proj", Kind: KindProject},
		"organizations/123": {ID: "organizations/123", Name: "123", Kind: KindOrg},
		"folders/456":       {ID: "folders/456", Name: "456", Kind: KindFolder},
	}
	for in, want := range cases {
		if got := ScopeNode(in); got.ID != want.ID || got.Name != want.Name || got.Kind != want.Kind {
			t.Errorf("%q: %+v, want %+v", in, got, want)
		}
	}
}
