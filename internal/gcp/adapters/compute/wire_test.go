package compute

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"

	gce "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/pagetest"
)

func computeFor(t *testing.T, url string) liveAPI {
	t.Helper()
	svc, err := gce.NewService(context.Background(), option.WithEndpoint(url), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	return liveAPI{svc: svc}
}

const link = "https://www.googleapis.com/compute/v1/projects/p"

// Aggregated lists read every scope of every page.
func TestListInstancesAggregatedPages(t *testing.T) {
	t.Parallel()
	srv := pagetest.New(t)
	srv.RawPages("/projects/p/aggregated/instances",
		`{"items":{
			"zones/us-central1-a":{"instances":[{"name":"web-1","zone":"`+link+`/zones/us-central1-a","status":"RUNNING"}]},
			"zones/us-east1-b":{"warning":{"code":"NO_RESULTS_ON_PAGE"}}}}`,
		`{"items":{"zones/europe-west1-c":{"instances":[{"name":"batch","zone":"`+link+`/zones/europe-west1-c","status":"TERMINATED"}]}}}`)
	got, err := computeFor(t, srv.URL).ListInstances(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, in := range got {
		names = append(names, in.Name+"@"+in.Zone)
	}
	if want := []string{"web-1@us-central1-a", "batch@europe-west1-c"}; !slices.Equal(names, want) {
		t.Fatalf("instances %v, want %v", names, want)
	}
	if toks := srv.Tokens("/projects/p/aggregated/instances"); !slices.Equal(toks, []string{"", "p1"}) {
		t.Errorf("page tokens %q", toks)
	}
}

// A group with an autoscaler gets its limits from the autoscaler list; a
// failed autoscaler list keeps the groups without them.
func TestListInstanceGroupsAutoscalers(t *testing.T) {
	t.Parallel()
	group := link + "/zones/us-central1-a/instanceGroupManagers/web"
	groups := `{"items":{"zones/us-central1-a":{"instanceGroupManagers":[
		{"name":"web","zone":"` + link + `/zones/us-central1-a","selfLink":"` + group + `","targetSize":3,
		 "status":{"autoscaler":"` + link + `/zones/us-central1-a/autoscalers/web-as"}},
		{"name":"batch","zone":"` + link + `/zones/us-central1-a","selfLink":"` + link + `/zones/us-central1-a/instanceGroupManagers/batch"}]}}}`
	tests := []struct {
		name    string
		failAS  bool
		wantMin int64
	}{
		{"with autoscalers", false, 2},
		{"autoscaler list denied", true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := pagetest.New(t)
			srv.RawPages("/projects/p/aggregated/instanceGroupManagers", groups)
			if tt.failAS {
				srv.Error("/projects/p/aggregated/autoscalers", http.StatusForbidden, "forbidden")
			} else {
				srv.RawPages("/projects/p/aggregated/autoscalers", `{"items":{"zones/us-central1-a":{"autoscalers":[
					{"name":"web-as","target":"`+group+`","status":"ACTIVE",
					 "autoscalingPolicy":{"minNumReplicas":2,"maxNumReplicas":10,"cpuUtilization":{"utilizationTarget":0.6}}}]}}}`)
			}
			got, err := computeFor(t, srv.URL).ListInstanceGroups(context.Background(), "p")
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 || got[0].Name != "web" || got[0].AutoscaleMin != tt.wantMin || got[1].AutoscaleMin != 0 {
				t.Fatalf("groups %+v", got)
			}
			if !tt.failAS && (got[0].AutoscaleMax != 10 || got[0].AutoscaleMode != "ON") {
				t.Errorf("autoscaler not applied: %+v", got[0])
			}
		})
	}
}

// The autoscaler list is skipped when no group names an autoscaler.
func TestListInstanceGroupsSkipsAutoscalers(t *testing.T) {
	t.Parallel()
	srv := pagetest.New(t)
	srv.RawPages("/projects/p/aggregated/instanceGroupManagers", `{"items":{"zones/us-central1-a":{"instanceGroupManagers":[
		{"name":"batch","zone":"`+link+`/zones/us-central1-a"}]}}}`)
	srv.RawPages("/projects/p/aggregated/autoscalers", `{"items":{}}`)
	if _, err := computeFor(t, srv.URL).ListInstanceGroups(context.Background(), "p"); err != nil {
		t.Fatal(err)
	}
	if n := len(srv.Asked("/projects/p/aggregated/autoscalers")); n != 0 {
		t.Fatalf("autoscaler list called %d times, want 0", n)
	}
}

// Managed instances come from the zonal or regional group, stop at the
// limit, and report truncation only when one was left.
func TestListManagedInstancesStopsAtLimit(t *testing.T) {
	t.Parallel()
	inst := func(names ...string) []string {
		var out []string
		for _, n := range names {
			out = append(out, fmt.Sprintf(`{"instance":"%s/zones/us-central1-a/instances/%s","instanceStatus":"RUNNING"}`, link, n))
		}
		return out
	}
	tests := []struct {
		name         string
		zone, region string
		suffix       string
		limit        int
		want         []string
		truncated    bool
	}{
		{"zonal within limit", "us-central1-a", "", "/projects/p/zones/us-central1-a/instanceGroupManagers/g/listManagedInstances", 9, []string{"a", "b", "c"}, false},
		{"zonal at limit", "us-central1-a", "", "/projects/p/zones/us-central1-a/instanceGroupManagers/g/listManagedInstances", 2, []string{"a", "b"}, true},
		{"regional", "", "us-central1", "/projects/p/regions/us-central1/instanceGroupManagers/g/listManagedInstances", 9, []string{"a", "b", "c"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := pagetest.New(t)
			srv.Pages(tt.suffix, "managedInstances", inst("a", "b"), inst("c"))
			got, truncated, err := computeFor(t, srv.URL).ListManagedInstances(context.Background(), "p", tt.zone, tt.region, "g", tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, m := range got {
				names = append(names, m.Name)
			}
			if !slices.Equal(names, tt.want) || truncated != tt.truncated {
				t.Errorf("got %v truncated=%v, want %v truncated=%v", names, truncated, tt.want, tt.truncated)
			}
		})
	}
}

func TestGlobalListPagesAndErrors(t *testing.T) {
	t.Parallel()
	srv := pagetest.New(t)
	srv.Pages("/projects/p/global/networks", "items",
		[]string{`{"name":"default","autoCreateSubnetworks":true}`},
		[]string{`{"name":"vpc-a","routingConfig":{"routingMode":"GLOBAL"}}`})
	srv.Error("/projects/p/global/firewalls", http.StatusForbidden, "forbidden")
	a := computeFor(t, srv.URL)
	nets, err := a.ListNetworks(context.Background(), "p")
	if err != nil || len(nets) != 2 || nets[1].Name != "vpc-a" {
		t.Fatalf("networks %+v, %v", nets, err)
	}
	_, err = a.ListFirewalls(context.Background(), "p")
	var ge *googleapi.Error
	if !errors.As(err, &ge) || ge.Code != http.StatusForbidden {
		t.Fatalf("err = %v, want a 403 googleapi.Error", err)
	}
}
