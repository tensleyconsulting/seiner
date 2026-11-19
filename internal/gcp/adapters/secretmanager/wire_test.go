package secretmanager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	sm "google.golang.org/api/secretmanager/v1"

	"github.com/tensleyconsulting/seiner/internal/gcp/pagetest"
)

func liveFor(t *testing.T, url string) liveAPI {
	t.Helper()
	svc, err := sm.NewService(context.Background(), option.WithEndpoint(url), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	return liveAPI{svc: svc}
}

const versionsPath = "/projects/p/secrets/s/versions"

func versions(names ...string) []string {
	var out []string
	for _, n := range names {
		out = append(out, fmt.Sprintf(`{"name":"projects/123/secrets/s/versions/%s","state":"ENABLED"}`, n))
	}
	return out
}

// ListVersions asks for pages no larger than the limit (at most 100),
// stops at the limit, and reports truncation only when a version was left.
func TestListVersionsStopsAtLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		limit      int
		want       []string
		truncated  bool
		wantTokens []string
		pageSize   string
	}{
		{1, []string{"3"}, true, []string{""}, "1"},
		{2, []string{"3", "2"}, true, []string{"", "p1"}, "2"},
		{3, []string{"3", "2", "1"}, false, []string{"", "p1"}, "3"},
		{500, []string{"3", "2", "1"}, false, []string{"", "p1"}, "100"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("limit %d", tt.limit), func(t *testing.T) {
			t.Parallel()
			srv := pagetest.New(t)
			srv.Pages(versionsPath, "versions", versions("3", "2"), versions("1"))
			got, truncated, err := liveFor(t, srv.URL).ListVersions(context.Background(), "p", "s", tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, v := range got {
				names = append(names, v.Name)
			}
			if !slices.Equal(names, tt.want) || truncated != tt.truncated {
				t.Errorf("got %v truncated=%v, want %v truncated=%v", names, truncated, tt.want, tt.truncated)
			}
			if toks := srv.Tokens(versionsPath); !slices.Equal(toks, tt.wantTokens) {
				t.Errorf("page tokens %q, want %q", toks, tt.wantTokens)
			}
			for _, q := range srv.Asked(versionsPath) {
				if q.Get("pageSize") != tt.pageSize {
					t.Errorf("pageSize %q, want %q", q.Get("pageSize"), tt.pageSize)
				}
			}
		})
	}
}

func TestListSecretsPagesAndMaps(t *testing.T) {
	t.Parallel()
	srv := pagetest.New(t)
	srv.Pages("/projects/p/secrets", "secrets",
		[]string{`{"name":"projects/123/secrets/api-key","replication":{"automatic":{}},"createTime":"2025-02-03T04:05:06Z"}`},
		[]string{`{"name":"projects/123/secrets/db-pass","replication":{"userManaged":{"replicas":[{"location":"us-east1"}]}}}`})
	got, err := liveFor(t, srv.URL).ListSecrets(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "api-key" || got[0].Replication != "automatic" || got[0].Created.IsZero() ||
		got[1].Name != "db-pass" || !slices.Equal(got[1].Locations, []string{"us-east1"}) {
		t.Fatalf("secrets %+v", got)
	}
	if toks := srv.Tokens("/projects/p/secrets"); !slices.Equal(toks, []string{"", "p1"}) {
		t.Errorf("page tokens %q", toks)
	}
}

func TestListSecretsError(t *testing.T) {
	t.Parallel()
	srv := pagetest.New(t)
	srv.Error("/projects/p/secrets", http.StatusForbidden, "forbidden")
	_, err := liveFor(t, srv.URL).ListSecrets(context.Background(), "p")
	var ge *googleapi.Error
	if !errors.As(err, &ge) || ge.Code != http.StatusForbidden {
		t.Fatalf("err = %v, want a 403 googleapi.Error", err)
	}
}
