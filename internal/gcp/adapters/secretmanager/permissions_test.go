package secretmanager

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
		"db-password": {
			{Role: "roles/secretmanager.secretAccessor", Members: []string{"serviceAccount:app@p.iam.gserviceaccount.com"}},
			{Role: "roles/secretmanager.viewer", Members: []string{"group:ops@example.com"}, Condition: "office-hours"},
		},
	}
	p := New(api)
	secrets, err := p.List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	got, truncated, err := p.ListRelated(context.Background(), secrets[0], model.FacetPermissions)
	if err != nil || truncated || len(got) != 2 {
		t.Fatalf("got=%+v truncated=%v err=%v", got, truncated, err)
	}
	if got[0].Service != model.ServiceIAMBinding || got[0].ParentID != secrets[0].ID ||
		got[0].FacetID != model.FacetPermissions || got[0].ScopeID != "projects/p" ||
		got[0].Name != "secretmanager.secretAccessor" ||
		!slices.Equal(got[0].List(model.ListMembers), []string{"serviceAccount:app@p.iam.gserviceaccount.com"}) {
		t.Fatalf("binding[0] = %+v", got[0])
	}
	if got[1].Name != "secretmanager.viewer · office-hours" {
		t.Fatalf("binding[1] name = %q", got[1].Name)
	}

	boom := errors.New("boom")
	if _, _, err := New(fakeAPI{err: boom}).ListRelated(context.Background(), secrets[0], model.FacetPermissions); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}
