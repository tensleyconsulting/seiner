package iam

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func TestServiceAccountPermissions(t *testing.T) {
	t.Parallel()
	const email = "deployer@p.iam.gserviceaccount.com"
	admin := fakeAdmin{
		accounts: map[string][]ServiceAccount{"p": {{Email: email}}},
		policies: map[string]Policy{
			email: {Bindings: []Binding{
				{Role: "roles/iam.serviceAccountTokenCreator", Members: []string{"user:alice@example.com"}},
				{Role: "roles/iam.workloadIdentityUser", Members: []string{"serviceAccount:p.svc.id.goog[prod/app]"}, Condition: "prod-only"},
			}},
		},
	}
	p := NewServiceAccounts(admin, nil)
	accounts, err := p.List(context.Background(), "projects/p")
	if err != nil || len(accounts) != 1 {
		t.Fatalf("%+v %v", accounts, err)
	}
	got, truncated, err := p.ListRelated(context.Background(), accounts[0], model.FacetPermissions)
	if err != nil || truncated || len(got) != 2 {
		t.Fatalf("got=%+v truncated=%v err=%v", got, truncated, err)
	}
	if got[0].Service != model.ServiceIAMBinding || got[0].ParentID != accounts[0].ID ||
		got[0].FacetID != model.FacetPermissions || got[0].ScopeID != "projects/p" ||
		got[0].Name != "iam.serviceAccountTokenCreator" ||
		!slices.Equal(got[0].List(model.ListMembers), []string{"user:alice@example.com"}) {
		t.Fatalf("binding[0] = %+v", got[0])
	}
	if got[1].Name != "iam.workloadIdentityUser · prod-only" {
		t.Fatalf("binding[1] name = %q", got[1].Name)
	}

	// Google-managed service agents carry no facets.
	agent := agentResource("projects/p", "service-123@compute-system.iam.gserviceaccount.com", []string{"roles/compute.serviceAgent"})
	if len(agent.Facets) != 0 {
		t.Fatalf("service agent facets = %+v, want none", agent.Facets)
	}

	boom := errors.New("boom")
	if _, _, err := NewServiceAccounts(fakeAdmin{err: boom}, nil).ListRelated(context.Background(), accounts[0], model.FacetPermissions); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}
