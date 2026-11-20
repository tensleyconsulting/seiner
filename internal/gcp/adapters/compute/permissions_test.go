package compute

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func TestComputePermissions(t *testing.T) {
	t.Parallel()
	api := fakeAPI{
		instances: []Instance{{Name: "web-1", Zone: "us-central1-a", Status: "RUNNING"}},
		networks:  []Network{{Name: "default"}},
		subnets:   []Subnet{{Name: "app-sub", Region: "us-central1", Network: "default"}},
		disks: []Disk{
			{Name: "boot-disk", Zone: "us-central1-a", Status: "READY"},
			{Name: "rep-disk", Region: "us-central1", Status: "READY"},
		},
		templates: []InstanceTemplate{
			{Name: "global-tmpl"},
			{Name: "regional-tmpl", Region: "us-central1"},
		},
		urlMaps:  []URLMap{{Name: "web-lb", ServiceRefs: []string{"global/backendServices/web-be", "us-central1/backendServices/reg-be"}}},
		backends: []BackendService{{Name: "web-be"}, {Name: "reg-be", Region: "us-central1"}},
		attached: []ServiceAttachment{{Name: "psc-svc", Region: "us-central1"}},
		iam: map[string][]Binding{
			"instances/us-central1-a/web-1":          {{Role: "roles/compute.instanceAdmin.v1", Members: []string{"user:alice@example.com"}}},
			"subnetworks/us-central1/app-sub":        {{Role: "roles/compute.networkUser", Members: []string{"serviceAccount:gke@p.iam.gserviceaccount.com"}}},
			"disks/us-central1-a/boot-disk":          {{Role: "roles/compute.storageAdmin", Members: []string{"group:ops@example.com"}}},
			"disks/us-central1/rep-disk":             {{Role: "roles/compute.storageAdmin", Members: []string{"group:ops@example.com"}}},
			"instanceTemplates//global-tmpl":         {{Role: "roles/compute.viewer", Members: []string{"group:devs@example.com"}}},
			"backendServices//web-be":                {{Role: "roles/compute.loadBalancerAdmin", Members: []string{"group:netops@example.com"}}},
			"backendServices/us-central1/reg-be":     {{Role: "roles/compute.loadBalancerAdmin", Members: []string{"group:netops@example.com"}}},
			"serviceAttachments/us-central1/psc-svc": {{Role: "roles/compute.networkAdmin", Members: []string{"group:psc@example.com"}}},
		},
	}

	ctx := context.Background()
	check := func(name string, lister model.RelatedLister, r model.Resource, wantMember string) {
		t.Helper()
		if len(r.Facets) == 0 || r.Facets[len(r.Facets)-1].ID != model.FacetPermissions {
			t.Fatalf("%s: missing Permissions facet: %+v", name, r.Facets)
		}
		got, truncated, err := lister.ListRelated(ctx, r, model.FacetPermissions)
		if err != nil || truncated || len(got) != 1 {
			t.Fatalf("%s: got=%+v truncated=%v err=%v", name, got, truncated, err)
		}
		if got[0].ParentID != r.ID || got[0].FacetID != model.FacetPermissions ||
			!slices.Equal(got[0].List(model.ListMembers), []string{wantMember}) {
			t.Fatalf("%s: binding = %+v", name, got[0])
		}
	}

	insts, _ := NewInstances(api).List(ctx, "projects/p")
	check("instance", NewInstances(api), insts[0], "user:alice@example.com")

	nets, _ := NewNetworks(api).List(ctx, "projects/p")
	subs, _, _ := NewNetworks(api).ListRelated(ctx, nets[0], model.FacetSubnets)
	check("subnet", NewNetworks(api), subs[0], "serviceAccount:gke@p.iam.gserviceaccount.com")

	disks, _ := NewDisks(api).List(ctx, "projects/p")
	check("zonal disk", NewDisks(api), disks[0], "group:ops@example.com")
	check("regional disk", NewDisks(api), disks[1], "group:ops@example.com")

	tmpls, _ := NewInstanceTemplates(api).List(ctx, "projects/p")
	check("global template", NewInstanceTemplates(api), tmpls[0], "group:devs@example.com")
	// Regional instance templates do not support GetIamPolicy.
	if len(tmpls[1].Facets) != 0 {
		t.Fatalf("regional template facets = %+v, want none", tmpls[1].Facets)
	}
	if got, _, err := NewInstanceTemplates(api).ListRelated(ctx, tmpls[1], model.FacetPermissions); got != nil || err != nil {
		t.Fatalf("regional template ListRelated = %+v %v, want nil", got, err)
	}

	lbs, _ := NewLoadBalancers(api).List(ctx, "projects/p")
	bes, _, _ := NewLoadBalancers(api).ListRelated(ctx, lbs[0], model.FacetBackends)
	check("global backend", NewLoadBalancers(api), bes[0], "group:netops@example.com")
	check("regional backend", NewLoadBalancers(api), bes[1], "group:netops@example.com")

	sas, _ := NewServiceAttachments(api).List(ctx, "projects/p")
	check("service attachment", NewServiceAttachments(api), sas[0], "group:psc@example.com")

	boom := errors.New("boom")
	if _, _, err := NewInstances(fakeAPI{err: boom}).ListRelated(ctx, insts[0], model.FacetPermissions); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}
