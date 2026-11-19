package gcp

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/crm"
	"github.com/tensleyconsulting/seiner/internal/model"
)

func TestOpenDemo(t *testing.T) {
	t.Parallel()
	sess := Session{
		Identity: model.Identity{Account: "sa@x", ADCProject: "real-prod", Source: model.IdentityADC},
		creds:    &google.Credentials{},
	}
	res := open(context.Background(), OpenOptions{Demo: true, Project: "demo-project"}, sess, func(context.Context, Session, string) (Catalog, error) {
		t.Fatal("live must not run")
		return Catalog{}, nil
	})
	if res.Source != SourceDemo {
		t.Fatalf("source %q", res.Source)
	}
	if res.Project != "demo-project" {
		t.Fatalf("project %q", res.Project)
	}
	root, err := res.Catalog.Hierarchy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := root.Find("projects/demo-project"); !ok {
		t.Fatal("fixture tree")
	}
}

func TestOpenLiveUsesADCProject(t *testing.T) {
	t.Parallel()
	sess := Session{
		Identity: model.Identity{Account: "sa@x", ADCProject: "real-prod", Source: model.IdentityADC},
		creds:    &google.Credentials{},
	}
	res := open(context.Background(), OpenOptions{}, sess, func(_ context.Context, _ Session, project string) (Catalog, error) {
		if project != "real-prod" {
			t.Fatalf("project %q", project)
		}
		return NewCatalog().WithHierarchy(model.HierarchyNode{ID: project, Name: project, Kind: model.KindProject}), nil
	})
	if res.Source != SourceLive {
		t.Fatalf("source %q", res.Source)
	}
	if res.Project != "real-prod" {
		t.Fatalf("project %q", res.Project)
	}
}

func TestOpenLiveRespectsExplicitProject(t *testing.T) {
	t.Parallel()
	sess := Session{
		Identity: model.Identity{Account: "sa@x", ADCProject: "real-prod", Source: model.IdentityADC},
		creds:    &google.Credentials{},
	}
	res := open(context.Background(), OpenOptions{Project: "other-proj", ProjectSet: true}, sess, func(_ context.Context, _ Session, project string) (Catalog, error) {
		if project != "other-proj" {
			t.Fatalf("project %q", project)
		}
		return NewCatalog().WithHierarchy(model.HierarchyNode{ID: project, Name: project, Kind: model.KindProject}), nil
	})
	if res.Project != "other-proj" || res.Source != SourceLive {
		t.Fatalf("%s %s", res.Project, res.Source)
	}
}

// Without --demo, nothing falls back to the demo catalog.
func TestOpenNeverFallsBackToDemo(t *testing.T) {
	t.Parallel()
	ready := Session{
		Identity: model.Identity{Account: "user (ADC)", Source: model.IdentityADC},
		creds:    &google.Credentials{},
	}
	noADC := Session{Identity: model.Identity{Err: errors.New("no adc")}}
	mustNotRun := func(context.Context, Session, string) (Catalog, error) {
		t.Error("live must not run")
		return Catalog{}, nil
	}
	fails := func(context.Context, Session, string) (Catalog, error) {
		return Catalog{}, errors.New("cannot dial")
	}
	tests := []struct {
		name    string
		opts    OpenOptions
		sess    Session
		live    liveFunc
		wantErr bool
		notice  string
	}{
		{"no credentials", OpenOptions{ProjectSet: true, Project: "p"}, noADC, mustNotRun, true, ""},
		{"no project", OpenOptions{}, ready, mustNotRun, false, model.NoticeNoProject},
		{"live project fails", OpenOptions{ProjectSet: true, Project: "real-prod"}, ready, fails, true, ""},
		{"live organization fails", OpenOptions{InitialResource: "organizations/123"}, ready, fails, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := open(context.Background(), tt.opts, tt.sess, tt.live)
			if res.Source != SourceNone {
				t.Fatalf("source %q, want %q", res.Source, SourceNone)
			}
			if _, err := res.Catalog.Hierarchy(context.Background()); err == nil {
				t.Fatal("a failed open must not carry a catalog")
			}
			if (res.Identity.Err != nil) != tt.wantErr {
				t.Fatalf("err %v, want err %v", res.Identity.Err, tt.wantErr)
			}
			if res.Identity.Notice != tt.notice {
				t.Fatalf("notice %q, want %q", res.Identity.Notice, tt.notice)
			}
		})
	}
}

func TestOpenNoticeOnlyWithoutProject(t *testing.T) {
	t.Parallel()
	ready := Session{
		Identity: model.Identity{Account: "sa@x", ADCProject: "real-prod", Source: model.IdentityADC},
		creds:    &google.Credentials{},
	}
	live := func(context.Context, Session, string) (Catalog, error) { return Catalog{}, nil }
	tests := []struct {
		name string
		opts OpenOptions
		sess Session
	}{
		{"live with adc project", OpenOptions{}, ready},
		{"live with --project", OpenOptions{ProjectSet: true, Project: "p"}, ready},
		{"--demo", OpenOptions{Demo: true, Project: "demo-project"}, ready},
		{"no credentials", OpenOptions{}, Session{Identity: model.Identity{Err: errors.New("no adc")}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if n := open(context.Background(), tt.opts, tt.sess, live).Identity.Notice; n != "" {
				t.Fatalf("notice %q", n)
			}
		})
	}
}

// TestLiveProvidersLazyConstructionFailureSurfacesAsProviderError builds the
// real live providers (as liveCatalog does) with a ClientOption that makes
// every underlying Google client fail to construct, offline and without a
// network call. Because construction is lazy, Open itself would have
// succeeded; the failure must instead show up as a per-service ProviderError
// from List, not as a silently dropped provider.
func TestLiveProvidersLazyConstructionFailureSurfacesAsProviderError(t *testing.T) {
	t.Parallel()
	badOpts := []option.ClientOption{option.WithCredentialsJSON([]byte("not-json"))}
	providers, _ := liveProviders(nil, crm.NewLazyClients(nil, badOpts...), badOpts)
	cat := NewCatalog(providers...)

	res := cat.List(context.Background(), "projects/demo-project")
	if len(res.Resources) != 0 {
		t.Fatalf("resources = %+v, want none", res.Resources)
	}
	var gotStorage bool
	for _, e := range res.Errors {
		if e.Service == model.ServiceCloudStorage {
			gotStorage = true
			if e.Message == "" && e.Err == nil {
				t.Fatalf("storage error has no detail: %+v", e)
			}
		}
	}
	if !gotStorage {
		t.Fatalf("errors = %+v, want a Cloud Storage provider error", res.Errors)
	}
}

func TestSessionReady(t *testing.T) {
	t.Parallel()
	if (Session{}).Ready() {
		t.Fatal("empty")
	}
	if (Session{Identity: model.Identity{Err: errors.New("x")}, creds: &google.Credentials{}}).Ready() {
		t.Fatal("err")
	}
	if !(Session{creds: &google.Credentials{}}).Ready() {
		t.Fatal("want ready")
	}
}

func TestOpenLiveFolderOrOrganization(t *testing.T) {
	t.Parallel()
	// No ADC project and no project set: the folder or organization is
	// enough.
	sess := Session{Identity: model.Identity{Account: "sa@x", Source: model.IdentityADC}, creds: &google.Credentials{}}
	for _, start := range []string{"organizations/123", "folders/456"} {
		res := open(context.Background(), OpenOptions{InitialResource: start}, sess, func(_ context.Context, _ Session, scope string) (Catalog, error) {
			if scope != start {
				t.Fatalf("scope %q, want %q", scope, start)
			}
			return NewCatalog().WithHierarchy(model.ScopeNode(scope)), nil
		})
		if res.Source != SourceLive || res.Identity.Notice != "" {
			t.Fatalf("%s: source %q notice %q", start, res.Source, res.Identity.Notice)
		}
	}

	res := open(context.Background(), OpenOptions{InitialResource: "organizations/123", Demo: true}, sess, func(context.Context, Session, string) (Catalog, error) {
		t.Fatal("live must not run with --demo")
		return Catalog{}, nil
	})
	if res.Source != SourceDemo {
		t.Fatalf("demo: source %q", res.Source)
	}
}
