package iam

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	policies map[string]Policy
	nodes    map[string]Node
	// fail makes GetPolicy and GetNode fail for the named resources.
	fail map[string]error
	err  error // returned from every GetPolicy when set
}

func (f fakeAPI) GetPolicy(_ context.Context, resource string) (Policy, error) {
	if f.err != nil {
		return Policy{}, f.err
	}
	if err := f.fail[resource]; err != nil {
		return Policy{}, err
	}
	if p, ok := f.policies[resource]; ok {
		return p, nil
	}
	return Policy{}, nil
}

func (f fakeAPI) GetNode(_ context.Context, name string) (Node, error) {
	if err := f.fail["node:"+name]; err != nil {
		return Node{}, err
	}
	return f.nodes[name], nil
}

// hierarchyAPI is projects/p under folders/2 (prod) under organizations/1
// (example.com), each with one binding.
func hierarchyAPI() fakeAPI {
	return fakeAPI{
		policies: map[string]Policy{
			"projects/p":      {Bindings: []Binding{{Role: "roles/viewer", Members: []string{"user:a@x"}}}},
			"folders/2":       {Bindings: []Binding{{Role: "roles/editor", Members: []string{"group:g@x"}}}},
			"organizations/1": {Bindings: []Binding{{Role: "roles/viewer", Members: []string{"user:b@x"}, Condition: "Weekdays"}}},
		},
		nodes: map[string]Node{
			"projects/p":      {DisplayName: "P", Parent: "folders/2"},
			"folders/2":       {DisplayName: "prod", Parent: "organizations/1"},
			"organizations/1": {DisplayName: "example.com"},
		},
		fail: map[string]error{},
	}
}

func TestListInheritedBindings(t *testing.T) {
	t.Parallel()
	p := New(hierarchyAPI())
	got, err := p.List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name, from string
		origin     model.ServiceType
		field      string
	}{
		{"viewer", "", "", ""},
		{"editor", "folders/2", model.ServiceFolder, "prod (folders/2)"},
		{"viewer · Weekdays", "organizations/1", model.ServiceOrganization, "example.com (organizations/1)"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d bindings: %+v", len(got), got)
	}
	seen := map[string]bool{}
	for i, w := range want {
		r := got[i]
		if r.Name != w.name || r.Origin != w.origin || r.Metadata[model.MetaInheritedFrom] != w.from {
			t.Errorf("[%d] name %q origin %q from %q, want %q %q %q", i, r.Name, r.Origin, r.Metadata[model.MetaInheritedFrom], w.name, w.origin, w.from)
		}
		if wantLevel := map[string]string{"": "", "folders/2": "1", "organizations/1": "2"}[w.from]; r.Metadata[model.MetaInheritedLevel] != wantLevel {
			t.Errorf("[%d] level %q, want %q", i, r.Metadata[model.MetaInheritedLevel], wantLevel)
		}
		if r.ScopeID != "projects/p" || !strings.HasPrefix(r.ID, "projects/p/") {
			t.Errorf("[%d] id %s scope %s: not in projects/p", i, r.ID, r.ScopeID)
		}
		if seen[r.ID] {
			t.Errorf("[%d] duplicate id %s", i, r.ID)
		}
		seen[r.ID] = true
		var field string
		for _, f := range r.Fields {
			if f.Key == model.MetaInheritedFrom {
				field = f.Value
			}
		}
		if field != w.field {
			t.Errorf("[%d] inheritedFrom field %q, want %q", i, field, w.field)
		}
	}
}

func TestListInheritedFolderScope(t *testing.T) {
	t.Parallel()
	got, err := New(hierarchyAPI()).List(context.Background(), "folders/2")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Origin != "" || got[1].Origin != model.ServiceOrganization {
		t.Fatalf("%+v", got)
	}
}

func TestListInheritedPartialAncestry(t *testing.T) {
	t.Parallel()
	boom := errors.New("permission denied")
	cases := []struct {
		name  string
		fail  string
		names []string
	}{
		// The folder cannot be read, so its parent is unknown; its policy
		// still loads.
		{"folder unreadable", "node:folders/2", []string{"viewer", "editor"}},
		{"project unreadable", "node:projects/p", []string{"viewer"}},
		{"folder policy denied", "folders/2", []string{"viewer", "viewer · Weekdays"}},
		{"org policy denied", "organizations/1", []string{"viewer", "editor"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := hierarchyAPI()
			api.fail[tc.fail] = boom
			got, err := New(api).List(context.Background(), "projects/p")
			if err != nil {
				t.Fatalf("an ancestor failure must not fail the list: %v", err)
			}
			var names []string
			for _, r := range got {
				names = append(names, r.Name)
			}
			if !slices.Equal(names, tc.names) {
				t.Errorf("names %v, want %v", names, tc.names)
			}
		})
	}
}

func TestListOrgSkipsAncestry(t *testing.T) {
	t.Parallel()
	api := hierarchyAPI()
	api.fail["node:organizations/1"] = errors.New("must not be called")
	got, err := New(api).List(context.Background(), "organizations/1")
	if err != nil || len(got) != 1 || got[0].Origin != "" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestListBindings(t *testing.T) {
	t.Parallel()
	p := New(fakeAPI{policies: map[string]Policy{
		"projects/demo-project": {Bindings: []Binding{
			{Role: "roles/owner", Members: []string{"user:a@x", "user:b@x"}},
			{Role: "roles/iam.serviceAccountUser", Members: []string{"serviceAccount:sa@x"}},
		}},
	}})
	got, err := p.List(context.Background(), "projects/demo-project")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len %d", len(got))
	}
	if got[0].Service != model.ServiceIAMBinding || got[0].Name != "owner" {
		t.Fatalf("%+v", got[0])
	}
	if m := got[0].List(model.ListMembers); !slices.Equal(m, []string{"user:a@x", "user:b@x"}) {
		t.Fatalf("members %v", m)
	}
	if got[1].Name != "iam.serviceAccountUser" {
		t.Fatalf("name %s", got[1].Name)
	}
}

func TestListOrgScope(t *testing.T) {
	t.Parallel()
	p := New(fakeAPI{policies: map[string]Policy{
		"organizations/9": {Bindings: []Binding{
			{Role: "roles/resourcemanager.organizationAdmin", Members: []string{"user:a@x"}},
		}},
	}})
	got, err := p.List(context.Background(), "organizations/9")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.Contains(got[0].ID, "organizations/9") {
		t.Fatalf("%+v", got)
	}
}

func TestListSkipsSyntheticRoot(t *testing.T) {
	t.Parallel()
	p := New(fakeAPI{policies: map[string]Policy{
		"projects/crm": {Bindings: []Binding{{Role: "roles/owner"}}},
	}})
	got, err := p.List(context.Background(), "cloud/root")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestListBareScopeEmpty(t *testing.T) {
	t.Parallel()
	p := New(fakeAPI{policies: map[string]Policy{
		"projects/example-org": {Bindings: []Binding{{Role: "roles/owner"}}},
	}})
	got, err := p.List(context.Background(), "example-org")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("bare scope must not be read as a project: %+v", got)
	}
}

func TestListError(t *testing.T) {
	t.Parallel()
	boom := errors.New("permission denied")
	p := New(fakeAPI{err: boom})
	_, err := p.List(context.Background(), "projects/demo-project")
	if !errors.Is(err, boom) {
		t.Fatalf("%v", err)
	}
}

func TestBindingFields(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		b    Binding
		want []model.Field
	}{
		{"plain", Binding{Role: "roles/viewer", Members: []string{"user:a", "user:b"}},
			[]model.Field{{Key: "role", Value: "roles/viewer"}}},
		{"conditional", Binding{Role: "roles/viewer", Members: []string{"user:a"}, Condition: "expires-2027"},
			[]model.Field{{Key: "role", Value: "roles/viewer"}, {Key: "condition", Value: "expires-2027"}}},
		{"empty", Binding{Role: "roles/viewer"},
			[]model.Field{{Key: "role", Value: "roles/viewer"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := resourceFrom("projects/p", "projects/p", tc.b)
			if !slices.Equal(r.Fields, tc.want) {
				t.Errorf("fields %v, want %v", r.Fields, tc.want)
			}
		})
	}
}

func TestConditionalBinding(t *testing.T) {
	t.Parallel()
	p := New(fakeAPI{policies: map[string]Policy{
		"projects/p": {Bindings: []Binding{
			{Role: "roles/viewer", Members: []string{"user:a"}},
			{Role: "roles/viewer", Members: []string{"user:b"}, Condition: "Business hours"},
		}},
	}})
	got, err := p.List(context.Background(), "projects/p")
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	if got[0].ID == got[1].ID {
		t.Fatalf("same role with and without a condition share id %s", got[0].ID)
	}
	if got[1].Name != "viewer · Business hours" {
		t.Errorf("name %q", got[1].Name)
	}
	for i, want := range []string{"user:a", "user:b"} {
		if m := got[i].List(model.ListMembers); !slices.Equal(m, []string{want}) {
			t.Errorf("%s members %v, want %s", got[i].ID, m, want)
		}
	}
}
