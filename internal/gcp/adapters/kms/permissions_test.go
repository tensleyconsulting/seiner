package kms

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func TestImportJobPermissions(t *testing.T) {
	t.Parallel()
	api := fakeAPI{
		locations: []string{"us-central1"},
		rings:     map[string][]KeyRing{"us-central1": {{Name: "app-ring", Location: "us-central1"}}},
		jobs:      map[string][]ImportJob{"app-ring": {{Name: "hsm-import", State: "ACTIVE"}}},
		policies: map[string][]Binding{
			"app-ring/importJobs/hsm-import": {
				{Role: "roles/cloudkms.importer", Members: []string{"user:alice@example.com"}},
			},
		},
	}
	p := New(api)
	rings, err := p.List(context.Background(), "projects/p")
	if err != nil || len(rings) == 0 {
		t.Fatalf("rings=%+v err=%v", rings, err)
	}
	jobs, _, err := p.ListRelated(context.Background(), rings[0], model.FacetImportJobs)
	if err != nil || len(jobs) == 0 {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
	job := jobs[0]
	if len(job.Facets) != 1 || job.Facets[0].ID != model.FacetPermissions {
		t.Fatalf("import job facets = %+v", job.Facets)
	}
	got, truncated, err := p.ListRelated(context.Background(), job, model.FacetPermissions)
	if err != nil || truncated || len(got) != 1 {
		t.Fatalf("got=%+v truncated=%v err=%v", got, truncated, err)
	}
	if got[0].Service != model.ServiceIAMBinding || got[0].ParentID != job.ID ||
		got[0].FacetID != model.FacetPermissions || got[0].ScopeID != "projects/p" ||
		got[0].Name != "cloudkms.importer" ||
		!slices.Equal(got[0].List(model.ListMembers), []string{"user:alice@example.com"}) {
		t.Fatalf("binding[0] = %+v", got[0])
	}

	boom := errors.New("boom")
	if _, _, err := New(fakeAPI{err: boom}).ListRelated(context.Background(), job, model.FacetPermissions); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}
