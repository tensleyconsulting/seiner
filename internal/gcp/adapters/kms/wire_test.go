package kms

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"

	cloudkms "google.golang.org/api/cloudkms/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/pagetest"
)

func liveFor(t *testing.T, url string) liveAPI {
	t.Helper()
	svc, err := cloudkms.NewService(context.Background(), option.WithEndpoint(url), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	return liveAPI{svc: svc}
}

const ring = "/projects/p/locations/us/keyRings/r"

func named(kind string, names ...string) []string {
	var out []string
	for _, n := range names {
		out = append(out, fmt.Sprintf(`{"name":"projects/p/locations/us/keyRings/r/%s/%s"}`, kind, n))
	}
	return out
}

// Keys, versions, and import jobs stop paging at the limit and report
// truncation only when an item was left.
func TestLimitedListsStopAtLimit(t *testing.T) {
	t.Parallel()
	type lister func(a liveAPI, limit int) (names []string, truncated bool, err error)
	keys := func(a liveAPI, limit int) ([]string, bool, error) {
		ks, tr, err := a.ListKeys(context.Background(), "p", "us", "r", limit)
		var out []string
		for _, k := range ks {
			out = append(out, k.Name)
		}
		return out, tr, err
	}
	versions := func(a liveAPI, limit int) ([]string, bool, error) {
		vs, tr, err := a.ListVersions(context.Background(), "p", "us", "r", "k", limit)
		var out []string
		for _, v := range vs {
			out = append(out, v.Name)
		}
		return out, tr, err
	}
	jobs := func(a liveAPI, limit int) ([]string, bool, error) {
		js, tr, err := a.ListImportJobs(context.Background(), "p", "us", "r", limit)
		var out []string
		for _, j := range js {
			out = append(out, j.Name)
		}
		return out, tr, err
	}
	tests := []struct {
		name          string
		suffix, field string
		kind          string
		list          lister
	}{
		{"keys", ring + "/cryptoKeys", "cryptoKeys", "cryptoKeys", keys},
		{"versions", ring + "/cryptoKeys/k/cryptoKeyVersions", "cryptoKeyVersions", "cryptoKeys/k/cryptoKeyVersions", versions},
		{"import jobs", ring + "/importJobs", "importJobs", "importJobs", jobs},
	}
	for _, tt := range tests {
		for _, lim := range []struct {
			limit      int
			want       []string
			truncated  bool
			wantTokens []string
		}{
			{2, []string{"a", "b"}, true, []string{"", "p1"}},
			{3, []string{"a", "b", "c"}, false, []string{"", "p1"}},
			{9, []string{"a", "b", "c"}, false, []string{"", "p1"}},
		} {
			t.Run(fmt.Sprintf("%s limit %d", tt.name, lim.limit), func(t *testing.T) {
				t.Parallel()
				srv := pagetest.New(t)
				srv.Pages(tt.suffix, tt.field, named(tt.kind, "a", "b"), named(tt.kind, "c"))
				got, truncated, err := tt.list(liveFor(t, srv.URL), lim.limit)
				if err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(got, lim.want) || truncated != lim.truncated {
					t.Errorf("got %v truncated=%v, want %v truncated=%v", got, truncated, lim.want, lim.truncated)
				}
				if toks := srv.Tokens(tt.suffix); !slices.Equal(toks, lim.wantTokens) {
					t.Errorf("page tokens %q, want %q", toks, lim.wantTokens)
				}
			})
		}
	}
}

func TestListLocationsAndRingsPage(t *testing.T) {
	t.Parallel()
	srv := pagetest.New(t)
	srv.Pages("/projects/p/locations", "locations", []string{`{"locationId":"us"}`}, []string{`{"locationId":"europe-west1"}`})
	srv.Pages("/projects/p/locations/us/keyRings", "keyRings",
		[]string{`{"name":"projects/p/locations/us/keyRings/a","createTime":"2025-01-01T00:00:00Z"}`},
		[]string{`{"name":"projects/p/locations/us/keyRings/b"}`})
	a := liveFor(t, srv.URL)
	locs, err := a.ListLocations(context.Background(), "p")
	if err != nil || !slices.Equal(locs, []string{"us", "europe-west1"}) {
		t.Fatalf("locations %v, %v", locs, err)
	}
	rings, err := a.ListKeyRings(context.Background(), "p", "us")
	if err != nil || len(rings) != 2 || rings[0].Name != "a" || rings[0].Location != "us" || rings[0].Created.IsZero() || rings[1].Name != "b" {
		t.Fatalf("rings %+v, %v", rings, err)
	}
}

// IAM policies are read as version 3, so conditional bindings come back
// with their conditions.
func TestGetIAMPolicyAsksForVersion3(t *testing.T) {
	t.Parallel()
	policy := `{"version":3,"bindings":[{"role":"roles/cloudkms.viewer","members":["user:a@x"],
		"condition":{"title":"business hours","expression":"true"}}]}`
	for _, tt := range []struct{ name, key, suffix string }{
		{"key ring", "", ring + ":getIamPolicy"},
		{"key", "k", ring + "/cryptoKeys/k:getIamPolicy"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := pagetest.New(t)
			srv.JSON(tt.suffix, policy)
			got, err := liveFor(t, srv.URL).GetIAMPolicy(context.Background(), "p", "us", "r", tt.key)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Condition != "business hours" {
				t.Fatalf("bindings %+v", got)
			}
			asked := srv.Asked(tt.suffix)
			if len(asked) != 1 || asked[0].Get("options.requestedPolicyVersion") != "3" {
				t.Fatalf("asked %v, want options.requestedPolicyVersion=3", asked)
			}
		})
	}
}

func TestListErrorsSurface(t *testing.T) {
	t.Parallel()
	srv := pagetest.New(t)
	srv.Error(ring+"/cryptoKeys", http.StatusForbidden, "forbidden")
	_, _, err := liveFor(t, srv.URL).ListKeys(context.Background(), "p", "us", "r", 10)
	var ge *googleapi.Error
	if !errors.As(err, &ge) || ge.Code != http.StatusForbidden {
		t.Fatalf("err = %v, want a 403 googleapi.Error", err)
	}
}
