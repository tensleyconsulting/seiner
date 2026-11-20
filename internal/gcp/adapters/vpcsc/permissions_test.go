package vpcsc

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func TestAccessPolicyPermissions(t *testing.T) {
	t.Parallel()
	api := sample()
	api.policyIAM = map[string][]Binding{
		"100": {
			{Role: "roles/accesscontextmanager.policyAdmin", Members: []string{"group:netsec@example.com"}},
		},
	}
	p := NewPolicies(api)
	pols, err := p.List(context.Background(), "organizations/9")
	if err != nil || len(pols) == 0 {
		t.Fatalf("pols=%+v err=%v", pols, err)
	}
	got, truncated, err := p.ListRelated(context.Background(), pols[0], model.FacetPermissions)
	if err != nil || truncated || len(got) != 1 {
		t.Fatalf("got=%+v truncated=%v err=%v", got, truncated, err)
	}
	if got[0].Service != model.ServiceIAMBinding || got[0].ParentID != pols[0].ID ||
		got[0].FacetID != model.FacetPermissions || got[0].ScopeID != "organizations/9" ||
		got[0].Name != "accesscontextmanager.policyAdmin" ||
		!slices.Equal(got[0].List(model.ListMembers), []string{"group:netsec@example.com"}) {
		t.Fatalf("binding[0] = %+v", got[0])
	}

	boom := errors.New("boom")
	if _, _, err := NewPolicies(fakeAPI{err: boom}).ListRelated(context.Background(), pols[0], model.FacetPermissions); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}
