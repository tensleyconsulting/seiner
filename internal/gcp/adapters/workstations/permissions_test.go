package workstations

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func TestPermissions(t *testing.T) {
	t.Parallel()
	api := fakeAPI{
		locs:     []string{"us-central1"},
		clusters: map[string][]Cluster{"us-central1": {{Name: "dev-cluster", Location: "us-central1"}}},
		configs:  map[string][]Config{"dev-cluster": {{Name: "go-dev", Cluster: "dev-cluster", Location: "us-central1"}}},
		ws:       map[string][]Workstation{"dev-cluster/go-dev": {{Name: "alice-ws", Config: "go-dev", Cluster: "dev-cluster", Location: "us-central1", State: "STATE_RUNNING"}}},
		policies: map[string][]Binding{
			"dev-cluster/go-dev": {
				{Role: "roles/workstations.user", Members: []string{"group:devs@example.com"}},
			},
			"dev-cluster/go-dev/alice-ws": {
				{Role: "roles/workstations.user", Members: []string{"user:alice@example.com"}, Condition: "weekdays"},
			},
		},
	}
	p := New(api)
	clusters, err := p.List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	configs, _, err := p.ListRelated(context.Background(), clusters[0], model.FacetConfigs)
	if err != nil || len(configs) == 0 {
		t.Fatalf("configs=%+v err=%v", configs, err)
	}
	cfg := configs[0]
	if len(cfg.Facets) != 2 || cfg.Facets[0].ID != model.FacetWorkstations || cfg.Facets[1].ID != model.FacetPermissions {
		t.Fatalf("config facets = %+v", cfg.Facets)
	}
	cfgPerms, truncated, err := p.ListRelated(context.Background(), cfg, model.FacetPermissions)
	if err != nil || truncated || len(cfgPerms) != 1 {
		t.Fatalf("cfgPerms=%+v truncated=%v err=%v", cfgPerms, truncated, err)
	}
	if cfgPerms[0].ParentID != cfg.ID || cfgPerms[0].FacetID != model.FacetPermissions ||
		!slices.Equal(cfgPerms[0].List(model.ListMembers), []string{"group:devs@example.com"}) {
		t.Fatalf("cfgPerms[0] = %+v", cfgPerms[0])
	}

	ws, _, err := p.ListRelated(context.Background(), cfg, model.FacetWorkstations)
	if err != nil || len(ws) == 0 {
		t.Fatalf("ws=%+v err=%v", ws, err)
	}
	w := ws[0]
	if len(w.Facets) != 1 || w.Facets[0].ID != model.FacetPermissions {
		t.Fatalf("workstation facets = %+v", w.Facets)
	}
	wsPerms, truncated, err := p.ListRelated(context.Background(), w, model.FacetPermissions)
	if err != nil || truncated || len(wsPerms) != 1 {
		t.Fatalf("wsPerms=%+v truncated=%v err=%v", wsPerms, truncated, err)
	}
	if wsPerms[0].ParentID != w.ID || wsPerms[0].FacetID != model.FacetPermissions ||
		wsPerms[0].Name != "workstations.user · weekdays" {
		t.Fatalf("wsPerms[0] = %+v", wsPerms[0])
	}

	boom := errors.New("boom")
	if _, _, err := New(fakeAPI{err: boom}).ListRelated(context.Background(), cfg, model.FacetPermissions); !errors.Is(err, boom) {
		t.Fatalf("cfg err = %v, want boom", err)
	}
	if _, _, err := New(fakeAPI{err: boom}).ListRelated(context.Background(), w, model.FacetPermissions); !errors.Is(err, boom) {
		t.Fatalf("ws err = %v, want boom", err)
	}
}
