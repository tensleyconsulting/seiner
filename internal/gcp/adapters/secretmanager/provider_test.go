package secretmanager

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	secrets  []Secret
	versions map[string][]Version // keyed by secret name
	err      error
	calls    *int
}

func (f fakeAPI) called() {
	if f.calls != nil {
		*f.calls++
	}
}

func (f fakeAPI) ListSecrets(context.Context, string) ([]Secret, error) {
	f.called()
	if f.err != nil {
		return nil, f.err
	}
	return f.secrets, nil
}

func (f fakeAPI) ListVersions(_ context.Context, _, secret string, limit int) ([]Version, bool, error) {
	f.called()
	if f.err != nil {
		return nil, false, f.err
	}
	vs := f.versions[secret]
	if len(vs) > limit {
		return vs[:limit], true, nil
	}
	return vs, false, nil
}

var created = time.Date(2024, 5, 2, 8, 10, 0, 0, time.UTC)

func sample() fakeAPI {
	return fakeAPI{
		secrets: []Secret{
			{
				Name: "db-password", Replication: "automatic", RotationPeriod: "2592000s",
				NextRotation: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Topics: []string{"secret-events"},
				Aliases: map[string]string{"current": "3", "previous": "2"}, Created: created,
				Labels: map[string]string{"env": "prod"},
			},
			{
				Name: "api-key", Replication: "user-managed", Locations: []string{"us-central1", "us-east1"},
				KMSKey: "projects/p/locations/us-central1/keyRings/r/cryptoKeys/k", DestroyTTL: "604800s",
				Expires: time.Now().Add(-time.Hour),
			},
		},
		versions: map[string][]Version{
			"db-password": {
				{Name: "3", State: "ENABLED", Created: created},
				{Name: "2", State: "DISABLED"},
				{Name: "1", State: "DESTROYED", Destroyed: time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)},
			},
		},
	}
}

func TestListScopes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		scope     string
		wantCalls int
		wantN     int
	}{
		{"projects/p", 1, 2},
		{"folders/1", 0, 0},
		{"organizations/1", 0, 0},
		{"", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.scope, func(t *testing.T) {
			t.Parallel()
			api := sample()
			calls := 0
			api.calls = &calls
			got, err := New(api).List(context.Background(), tc.scope)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.wantN || calls != tc.wantCalls {
				t.Fatalf("len=%d calls=%d, want %d and %d", len(got), calls, tc.wantN, tc.wantCalls)
			}
		})
	}
}

func TestSecretMapping(t *testing.T) {
	t.Parallel()
	got, err := New(sample()).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	r := got[0]
	if r.ID != "projects/p/secrets/db-password" || r.Name != "db-password" || r.Service != model.ServiceSecretManager ||
		r.ScopeID != "projects/p" || r.Location != "global" || r.Status != model.StatusUnknown ||
		!r.Created.Equal(created) || r.Labels["env"] != "prod" {
		t.Fatalf("identity: %+v", r)
	}
	var keys []string
	for _, f := range r.Fields {
		keys = append(keys, f.Key)
	}
	if want := []string{"replication", "rotation", "nextRotation", "topics"}; !slices.Equal(keys, want) {
		t.Fatalf("fields = %v, want %v", keys, want)
	}
	if r.Metadata["rotation"] != "30 days" || r.Metadata["nextRotation"] != "2026-11-01 00:00 UTC" {
		t.Fatalf("metadata: %+v", r.Metadata)
	}
	if aliases := r.List("aliases"); !slices.Equal(aliases, []string{"current → 3", "previous → 2"}) {
		t.Fatalf("aliases = %v", aliases)
	}
	if len(r.Facets) != 1 || r.Facets[0].ID != model.FacetVersions || !r.Facets[0].Inline {
		t.Fatalf("facets: %+v", r.Facets)
	}

	um := got[1]
	if um.Metadata["replication"] != "user-managed (us-central1, us-east1)" || um.Metadata["destroyDelay"] != "7 days" ||
		um.Metadata["kmsKey"] == "" || um.Status != model.StatusWarning {
		t.Fatalf("user-managed, expired secret: %+v", um)
	}
}

func TestVersions(t *testing.T) {
	t.Parallel()
	p := New(sample())
	secrets, err := p.List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	parent := secrets[0]
	got, truncated, err := p.ListRelated(context.Background(), parent, model.FacetVersions)
	if err != nil || truncated || len(got) != 3 {
		t.Fatalf("got=%+v truncated=%v err=%v", got, truncated, err)
	}
	v := got[0]
	if v.ID != "projects/p/secrets/db-password/versions/3" || v.Name != "3" || v.Service != model.ServiceSecretVersion ||
		v.ParentID != parent.ID || v.FacetID != model.FacetVersions || v.ScopeID != "projects/p" ||
		v.State != "ENABLED" || v.Badge != "ENABLED" || !v.Created.Equal(created) {
		t.Fatalf("version: %+v", v)
	}
	statuses := []model.ResourceStatus{got[0].Status, got[1].Status, got[2].Status}
	if want := []model.ResourceStatus{model.StatusHealthy, model.StatusWarning, model.StatusUnknown}; !slices.Equal(statuses, want) {
		t.Fatalf("statuses = %v, want %v", statuses, want)
	}
	if got[2].Metadata["destroyed"] != "2025-01-10 12:00 UTC" {
		t.Fatalf("destroyed version: %+v", got[2].Metadata)
	}
}

func TestVersionsTruncate(t *testing.T) {
	t.Parallel()
	api := sample()
	many := make([]Version, maxVersions+1)
	for i := range many {
		many[i] = Version{Name: "v", State: "ENABLED"}
	}
	api.versions["db-password"] = many
	parent := secretResource("p", api.secrets[0])
	got, truncated, err := New(api).ListRelated(context.Background(), parent, model.FacetVersions)
	if err != nil || !truncated || len(got) != maxVersions {
		t.Fatalf("len=%d truncated=%v err=%v", len(got), truncated, err)
	}
}

func TestListRelatedIgnoresOthers(t *testing.T) {
	t.Parallel()
	secret := secretResource("p", sample().secrets[0])
	cases := []struct {
		name   string
		parent model.Resource
		facet  string
	}{
		{"other service", model.Resource{ID: "projects/p/locations/l/repositories/r/packages/x", Service: model.ServiceArtifactPackage}, model.FacetVersions},
		{"other facet", secret, model.FacetKeys},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			api := sample()
			api.calls = &calls
			got, truncated, err := New(api).ListRelated(context.Background(), tc.parent, tc.facet)
			if got != nil || truncated || err != nil || calls != 0 {
				t.Fatalf("got=%v truncated=%v err=%v calls=%d", got, truncated, err, calls)
			}
		})
	}
}

func TestDuration(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"": "", "86400s": "1 day", "7776000s": "90 days", "5400s": "1h30m0s", "weird": "weird"} {
		if got := duration(in); got != want {
			t.Errorf("duration(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestErrorsPropagate(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	p := New(fakeAPI{err: boom})
	if _, err := p.List(context.Background(), "projects/p"); !errors.Is(err, boom) {
		t.Fatalf("List err = %v", err)
	}
	if _, _, err := p.ListRelated(context.Background(), secretResource("p", Secret{Name: "s"}), model.FacetVersions); !errors.Is(err, boom) {
		t.Fatalf("ListRelated err = %v", err)
	}
}
