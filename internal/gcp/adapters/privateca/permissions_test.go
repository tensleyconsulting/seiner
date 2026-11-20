package privateca

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func TestCRLPermissions(t *testing.T) {
	t.Parallel()
	api := fakeAPI{
		pools:       []Pool{{Name: "internal", Location: "us-central1", Tier: "ENTERPRISE"}},
		authorities: map[string][]Authority{"us-central1/internal": {{Name: "root-ca", Tier: "ENTERPRISE", State: "ENABLED"}}},
		crls:        map[string][]CRL{"us-central1/internal/root-ca": {{Name: "crl-1", State: "ACTIVE"}}},
		crlIAM: map[string][]Binding{
			"us-central1/internal/root-ca/crl-1": {
				{Role: "roles/privateca.auditor", Members: []string{"group:secops@example.com"}},
			},
		},
	}
	p := NewPools(api)
	pools, err := p.List(context.Background(), "projects/p")
	if err != nil || len(pools) != 1 {
		t.Fatalf("pools=%+v err=%v", pools, err)
	}
	cas, _, err := p.ListRelated(context.Background(), pools[0], model.FacetCertificateAuthorities)
	if err != nil || len(cas) != 1 {
		t.Fatalf("cas=%+v err=%v", cas, err)
	}
	crls, _, err := p.ListRelated(context.Background(), cas[0], model.FacetCRLs)
	if err != nil || len(crls) != 1 {
		t.Fatalf("crls=%+v err=%v", crls, err)
	}
	crl := crls[0]
	if len(crl.Facets) != 1 || crl.Facets[0].ID != model.FacetPermissions {
		t.Fatalf("crl facets = %+v", crl.Facets)
	}
	got, truncated, err := p.ListRelated(context.Background(), crl, model.FacetPermissions)
	if err != nil || truncated || len(got) != 1 {
		t.Fatalf("got=%+v truncated=%v err=%v", got, truncated, err)
	}
	if got[0].Service != model.ServiceIAMBinding || got[0].ParentID != crl.ID ||
		got[0].FacetID != model.FacetPermissions || got[0].ScopeID != "projects/p" ||
		got[0].Name != "privateca.auditor" ||
		!slices.Equal(got[0].List(model.ListMembers), []string{"group:secops@example.com"}) {
		t.Fatalf("binding[0] = %+v", got[0])
	}

	boom := errors.New("boom")
	if _, _, err := NewPools(fakeAPI{err: boom}).ListRelated(context.Background(), crl, model.FacetPermissions); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}
