package clouddns

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func TestPermissions(t *testing.T) {
	t.Parallel()
	api := sample()
	api.policies = map[string][]Binding{
		"example-com": {
			{Role: "roles/dns.admin", Members: []string{"group:netops@example.com"}},
			{Role: "roles/dns.reader", Members: []string{"user:alice@example.com"}, Condition: "temp"},
		},
	}
	p := New(api)
	zones, err := p.List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	got, truncated, err := p.ListRelated(context.Background(), zones[0], model.FacetPermissions)
	if err != nil || truncated || len(got) != 2 {
		t.Fatalf("got=%+v truncated=%v err=%v", got, truncated, err)
	}
	if got[0].Service != model.ServiceIAMBinding || got[0].ParentID != zones[0].ID ||
		got[0].FacetID != model.FacetPermissions || got[0].ScopeID != "projects/p" ||
		got[0].Name != "dns.admin" ||
		!slices.Equal(got[0].List(model.ListMembers), []string{"group:netops@example.com"}) {
		t.Fatalf("binding[0] = %+v", got[0])
	}
	if got[1].Name != "dns.reader · temp" {
		t.Fatalf("binding[1] name = %q", got[1].Name)
	}

	boom := errors.New("boom")
	if _, _, err := New(fakeAPI{err: boom}).ListRelated(context.Background(), zones[0], model.FacetPermissions); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}
