package bigquery

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func TestTableAndRoutinePermissions(t *testing.T) {
	t.Parallel()
	api := fakeDatasets{
		listed:   []Dataset{{ID: "analytics", Location: "US"}},
		full:     map[string]Dataset{"analytics": {ID: "analytics", Location: "US"}},
		tables:   []Table{{ID: "events", Type: "TABLE"}},
		routines: []Routine{{ID: "clean_ip", Type: "SCALAR_FUNCTION"}},
		tableIAM: map[string][]Binding{
			"analytics/events": {{Role: "roles/bigquery.dataViewer", Members: []string{"group:analysts@example.com"}}},
		},
		routIAM: map[string][]Binding{
			"analytics/clean_ip": {{Role: "roles/bigquery.metadataViewer", Members: []string{"user:alice@example.com"}}},
		},
	}
	p := NewDatasets(api)
	ds, err := p.List(context.Background(), "projects/p")
	if err != nil || len(ds) != 1 {
		t.Fatalf("ds=%+v err=%v", ds, err)
	}
	tables, _, err := p.ListRelated(context.Background(), ds[0], model.FacetTables)
	if err != nil || len(tables) != 1 {
		t.Fatalf("tables=%+v err=%v", tables, err)
	}
	if len(tables[0].Facets) != 1 || tables[0].Facets[0].ID != model.FacetPermissions {
		t.Fatalf("table facets = %+v", tables[0].Facets)
	}
	tPerms, truncated, err := p.ListRelated(context.Background(), tables[0], model.FacetPermissions)
	if err != nil || truncated || len(tPerms) != 1 || tPerms[0].ParentID != tables[0].ID ||
		!slices.Equal(tPerms[0].List(model.ListMembers), []string{"group:analysts@example.com"}) {
		t.Fatalf("table perms = %+v %v %v", tPerms, truncated, err)
	}

	routines, _, err := p.ListRelated(context.Background(), ds[0], model.FacetRoutines)
	if err != nil || len(routines) != 1 {
		t.Fatalf("routines=%+v err=%v", routines, err)
	}
	if len(routines[0].Facets) != 1 || routines[0].Facets[0].ID != model.FacetPermissions {
		t.Fatalf("routine facets = %+v", routines[0].Facets)
	}
	rPerms, truncated, err := p.ListRelated(context.Background(), routines[0], model.FacetPermissions)
	if err != nil || truncated || len(rPerms) != 1 || rPerms[0].ParentID != routines[0].ID ||
		!slices.Equal(rPerms[0].List(model.ListMembers), []string{"user:alice@example.com"}) {
		t.Fatalf("routine perms = %+v %v %v", rPerms, truncated, err)
	}

	boom := errors.New("boom")
	if _, _, err := NewDatasets(fakeDatasets{err: boom}).ListRelated(context.Background(), tables[0], model.FacetPermissions); !errors.Is(err, boom) {
		t.Fatalf("table err = %v, want boom", err)
	}
	if _, _, err := NewDatasets(fakeDatasets{err: boom}).ListRelated(context.Background(), routines[0], model.FacetPermissions); !errors.Is(err, boom) {
		t.Fatalf("routine err = %v, want boom", err)
	}
}

func TestReservationAssignmentAndConnectionPermissions(t *testing.T) {
	t.Parallel()
	api := fakeRegional{
		locations:    []string{"US"},
		reservations: map[string][]Reservation{"US": {{Name: "prod-etl", Location: "US", Slots: 100}}},
		assignments:  map[string][]Assignment{"prod-etl": {{Name: "a1", Assignee: "projects/p", JobType: "QUERY", State: "ACTIVE"}}},
		connections:  map[string][]Connection{"US": {{ID: "cloud-sql", Location: "US", Kind: "Cloud SQL"}}},
		resIAM: map[string][]Binding{
			"US/prod-etl": {{Role: "roles/bigquery.resourceViewer", Members: []string{"group:data@example.com"}}},
		},
		assignIAM: map[string][]Binding{
			"US/prod-etl/a1": {{Role: "roles/bigquery.resourceEditor", Members: []string{"user:bob@example.com"}}},
		},
		connIAM: map[string][]Binding{
			"US/cloud-sql": {{Role: "roles/bigquery.connectionUser", Members: []string{"serviceAccount:etl@p.iam.gserviceaccount.com"}}},
		},
	}
	rp := NewReservations(api)
	res, err := rp.List(context.Background(), "projects/p")
	if err != nil || len(res) != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	resPerms, _, err := rp.ListRelated(context.Background(), res[0], model.FacetPermissions)
	if err != nil || len(resPerms) != 1 || resPerms[0].ParentID != res[0].ID {
		t.Fatalf("resPerms=%+v err=%v", resPerms, err)
	}
	as, _, err := rp.ListRelated(context.Background(), res[0], model.FacetAssignments)
	if err != nil || len(as) != 1 {
		t.Fatalf("as=%+v err=%v", as, err)
	}
	if len(as[0].Facets) != 1 || as[0].Facets[0].ID != model.FacetPermissions {
		t.Fatalf("assignment facets = %+v", as[0].Facets)
	}
	asPerms, _, err := rp.ListRelated(context.Background(), as[0], model.FacetPermissions)
	if err != nil || len(asPerms) != 1 || asPerms[0].ParentID != as[0].ID {
		t.Fatalf("asPerms=%+v err=%v", asPerms, err)
	}

	cp := NewConnections(api)
	conns, err := cp.List(context.Background(), "projects/p")
	if err != nil || len(conns) != 1 {
		t.Fatalf("conns=%+v err=%v", conns, err)
	}
	if len(conns[0].Facets) != 1 || conns[0].Facets[0].ID != model.FacetPermissions {
		t.Fatalf("connection facets = %+v", conns[0].Facets)
	}
	connPerms, _, err := cp.ListRelated(context.Background(), conns[0], model.FacetPermissions)
	if err != nil || len(connPerms) != 1 || connPerms[0].ParentID != conns[0].ID ||
		!slices.Equal(connPerms[0].List(model.ListMembers), []string{"serviceAccount:etl@p.iam.gserviceaccount.com"}) {
		t.Fatalf("connPerms=%+v err=%v", connPerms, err)
	}
}
