package artifactregistry

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"math/big"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/gcp/fanout"
	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	locations []string
	repos     map[string][]Repository // keyed by location
	pkgs      map[string][]Package    // keyed by repo name
	policies  map[string][]Binding    // keyed by repo name
	versions  map[string][]Version    // keyed by repo/pkg
	sigs      map[string][]Signature  // keyed by repo/pkg@digest
	artifacts map[string][]Version    // keyed by repo/pkg@digest
	failLocs  map[string]error        // per-location ListRepositories error
	err       error                   // returned from every method when set
	calls     *atomic.Int32
}

func (f fakeAPI) called() {
	if f.calls != nil {
		f.calls.Add(1)
	}
}

func (f fakeAPI) ListLocations(context.Context, string) ([]string, error) {
	f.called()
	if f.err != nil {
		return nil, f.err
	}
	return f.locations, nil
}

func (f fakeAPI) ListRepositories(_ context.Context, _, location string) ([]Repository, error) {
	f.called()
	if f.err != nil {
		return nil, f.err
	}
	if err := f.failLocs[location]; err != nil {
		return nil, err
	}
	return f.repos[location], nil
}

func (f fakeAPI) GetRepositoryPolicy(_ context.Context, _, _, name string) ([]Binding, error) {
	f.called()
	if f.err != nil {
		return nil, f.err
	}
	return f.policies[name], nil
}

func (f fakeAPI) ListPackages(_ context.Context, _, _, repo string, limit int) ([]Package, bool, error) {
	f.called()
	if f.err != nil {
		return nil, false, f.err
	}
	return capped(f.pkgs[repo], limit)
}

func (f fakeAPI) ListVersions(_ context.Context, _, _, repo, pkg string, limit int) ([]Version, bool, error) {
	f.called()
	if f.err != nil {
		return nil, false, f.err
	}
	return capped(f.versions[repo+"/"+pkg], limit)
}

func (f fakeAPI) ListSignatures(_ context.Context, _, _, repo, pkg, digest string, limit int) ([]Signature, bool, error) {
	f.called()
	if f.err != nil {
		return nil, false, f.err
	}
	return capped(f.sigs[repo+"/"+pkg+"@"+digest], limit)
}

func (f fakeAPI) ListArtifacts(_ context.Context, _, _, repo, pkg, digest string, limit int) ([]Version, bool, error) {
	f.called()
	if f.err != nil {
		return nil, false, f.err
	}
	return capped(f.artifacts[repo+"/"+pkg+"@"+digest], limit)
}

type fakeScan struct {
	vulns map[string][]Vulnerability // keyed by resource URL
	err   error
	urls  *[]string
}

func (f fakeScan) ListVulnerabilities(_ context.Context, _, resourceURL string, limit int) ([]Vulnerability, bool, error) {
	if f.urls != nil {
		*f.urls = append(*f.urls, resourceURL)
	}
	if f.err != nil {
		return nil, false, f.err
	}
	return capped(f.vulns[resourceURL], limit)
}

func capped[T any](items []T, limit int) ([]T, bool, error) {
	if len(items) > limit {
		return items[:limit], true, nil
	}
	return items, false, nil
}

var created = time.Date(2024, 5, 20, 8, 0, 0, 0, time.UTC)

const (
	digest = "sha256:3f9a1c2b7d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8"
	image  = "us-central1-docker.pkg.dev/p/app-images/web/frontend@" + digest
)

func sample() fakeAPI {
	return fakeAPI{
		locations: []string{"us", "us-central1"},
		repos: map[string][]Repository{
			"us-central1": {{
				Name: "app-images", Location: "us-central1", Description: "Images", Format: "DOCKER",
				Mode: "STANDARD_REPOSITORY", RegistryURI: "us-central1-docker.pkg.dev/p/app-images",
				SizeBytes: 1395864371, ImmutableTags: true, CleanupPolicies: 2, CleanupDryRun: true,
				ScanState: "SCANNING_ACTIVE", Created: created, Labels: map[string]string{"team": "platform"},
			}},
			"us": {{
				Name: "pypi-cache", Location: "us", Format: "PYTHON", Mode: "REMOTE_REPOSITORY",
				Upstream: "PyPI", ScanState: "SCANNING_UNSUPPORTED", ScanReason: "format not supported",
			}},
		},
		policies: map[string][]Binding{
			"app-images": {
				{Role: "roles/artifactregistry.reader", Members: []string{"serviceAccount:nodes@p.iam.gserviceaccount.com"}},
				{Role: "roles/artifactregistry.writer", Members: []string{"group:ci@example.com"}, Condition: "Weekdays"},
			},
		},
		pkgs: map[string][]Package{
			"app-images": {{Name: "web/frontend", Created: created}, {Name: "api", DisplayName: "API server"}},
			"pypi-cache": {{Name: "requests"}},
		},
		versions: map[string][]Version{
			"app-images/web/frontend": {
				{Name: digest, Tags: []string{"v1.4.2", "latest"}, SizeBytes: 50540134, MediaType: "application/vnd.oci.image.manifest.v1+json", Created: created},
				{Name: "sha256:9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e"},
			},
			"pypi-cache/requests": {{Name: "2.32.3", Description: "HTTP for Humans"}},
		},
	}
}

func TestListScopes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		scope     string
		wantCalls int32
		wantN     int
	}{
		{"projects/p", 3, 2}, // locations + one call per location
		{"folders/1", 0, 0},
		{"organizations/1", 0, 0},
		{"", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.scope, func(t *testing.T) {
			t.Parallel()
			api := sample()
			api.calls = new(atomic.Int32)
			got, err := New(api).List(context.Background(), tc.scope)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.wantN || api.calls.Load() != tc.wantCalls {
				t.Fatalf("len=%d calls=%d, want %d and %d", len(got), api.calls.Load(), tc.wantN, tc.wantCalls)
			}
		})
	}
}

func TestListLocationFailures(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	cases := []struct {
		name      string
		failLocs  map[string]error
		wantRepos []string
		wantErr   bool
	}{
		{name: "all loaded", wantRepos: []string{"pypi-cache", "app-images"}},
		{name: "one failed", failLocs: map[string]error{"us": boom}, wantRepos: []string{"app-images"}},
		{name: "every location failed", failLocs: map[string]error{"us": boom, "us-central1": errors.New("other")}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := sample()
			api.failLocs = tc.failLocs
			got, err := New(api).List(context.Background(), "projects/p")
			var names []string
			for _, r := range got {
				names = append(names, r.Name)
			}
			if !slices.Equal(names, tc.wantRepos) {
				t.Fatalf("repositories = %v, want %v", names, tc.wantRepos)
			}
			var le fanout.LocationError
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
			} else if model.IsPartial(err) || !errors.Is(err, boom) || !errors.As(err, &le) {
				t.Fatalf("all locations failed: err = %v, want the first LocationError wrapping boom", err)
			}
		})
	}
}

func TestRepositoryMapping(t *testing.T) {
	t.Parallel()
	got, err := New(sample()).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	// fanout keeps location order: us, then us-central1.
	remote, docker := got[0], got[1]
	if docker.ID != "projects/p/locations/us-central1/repositories/app-images" || docker.Name != "app-images" ||
		docker.Service != model.ServiceArtifactRegistry || docker.ScopeID != "projects/p" || docker.Location != "us-central1" {
		t.Fatalf("identity: %+v", docker)
	}
	if docker.Status != "" || !docker.Created.Equal(created) || docker.Labels["team"] != "platform" ||
		docker.Description != "Images" {
		t.Fatalf("status/created/labels: %+v", docker)
	}
	if err := docker.Validate(); err != nil {
		t.Fatal(err)
	}
	want := []string{"format", "mode", "registryUri", "size", "vulnerabilityScanning", "immutableTags", "cleanupPolicies"}
	if keys := fieldKeys(docker); !slices.Equal(keys, want) {
		t.Errorf("fields = %v\nwant %v", keys, want)
	}
	for k, v := range map[string]string{
		"format": "DOCKER", "mode": "Standard", "size": "1.3 GiB", "vulnerabilityScanning": "active",
		"immutableTags": "on", "cleanupPolicies": "2 (dry run)",
	} {
		if docker.Metadata[k] != v {
			t.Errorf("docker %s = %q, want %q", k, docker.Metadata[k], v)
		}
	}
	wantFacets := []model.Facet{{ID: model.FacetPermissions, Label: "Permissions", Optional: true}, {ID: model.FacetPackages, Label: "Images"}}
	if !slices.Equal(docker.Facets, wantFacets) {
		t.Errorf("docker facets = %+v, want %+v", docker.Facets, wantFacets)
	}

	for k, v := range map[string]string{
		"mode": "Remote", "upstream": "PyPI", "vulnerabilityScanning": "unsupported (format not supported)",
	} {
		if remote.Metadata[k] != v {
			t.Errorf("remote %s = %q, want %q", k, remote.Metadata[k], v)
		}
	}
	if _, ok := remote.Metadata["immutableTags"]; ok {
		t.Error("immutableTags is Docker-only")
	}
	if remote.Facets[1].Label != "Packages" {
		t.Errorf("remote facet label = %q", remote.Facets[1].Label)
	}
}

func TestListRelatedPermissions(t *testing.T) {
	t.Parallel()
	p := New(sample())
	repo := model.Resource{ID: "projects/p/locations/us-central1/repositories/app-images", Service: model.ServiceArtifactRegistry, ScopeID: "projects/p"}
	got, truncated, err := p.ListRelated(context.Background(), repo, model.FacetPermissions)
	if err != nil || truncated {
		t.Fatalf("ListRelated: %v %v", truncated, err)
	}
	wantIDs := []string{
		repo.ID + "/iam/bindings/roles%2Fartifactregistry.reader",
		repo.ID + "/iam/bindings/roles%2Fartifactregistry.writer/conditions/Weekdays",
	}
	if len(got) != len(wantIDs) {
		t.Fatalf("bindings = %+v", got)
	}
	for i, b := range got {
		if b.ID != wantIDs[i] || b.Service != model.ServiceIAMBinding || b.ScopeID != "projects/p" ||
			b.ParentID != repo.ID || b.FacetID != model.FacetPermissions || !b.IsBinding() {
			t.Errorf("binding %d = %+v", i, b)
		}
	}
	if m := got[0].List(model.ListMembers); !slices.Equal(m, []string{"serviceAccount:nodes@p.iam.gserviceaccount.com"}) {
		t.Errorf("members = %v", m)
	}
	if got[1].Name != "artifactregistry.writer · Weekdays" {
		t.Errorf("conditional name = %q", got[1].Name)
	}
}

func TestListRelatedPackagesAndVersions(t *testing.T) {
	t.Parallel()
	p := New(sample())
	ctx := context.Background()
	repos, _ := p.List(ctx, "projects/p")
	docker := repos[1]

	pkgs, truncated, err := p.ListRelated(ctx, docker, model.FacetPackages)
	if err != nil || truncated || len(pkgs) != 2 {
		t.Fatalf("packages: %d truncated=%v err=%v", len(pkgs), truncated, err)
	}
	web := pkgs[1]
	if web.ID != docker.ID+"/packages/web%2Ffrontend" || web.Name != "web/frontend" || web.Service != model.ServiceArtifactPackage ||
		web.ParentID != docker.ID || web.FacetID != model.FacetPackages || web.ScopeID != "projects/p" || web.Location != "us-central1" {
		t.Fatalf("package: %+v", web)
	}
	if err := web.Validate(); err != nil {
		t.Fatal(err)
	}
	wantPkgFacets := []model.Facet{
		{ID: model.FacetVersions, Label: "Digests"},
		{ID: model.FacetSignatureTags, Label: "Signature Tags", Optional: true},
	}
	if !slices.Equal(web.Facets, wantPkgFacets) {
		t.Errorf("package facets = %+v, want %+v", web.Facets, wantPkgFacets)
	}
	if pkgs[0].Metadata["displayName"] != "API server" {
		t.Errorf("displayName = %q", pkgs[0].Metadata["displayName"])
	}

	versions, truncated, err := p.ListRelated(ctx, web, model.FacetVersions)
	if err != nil || truncated || len(versions) != 2 {
		t.Fatalf("versions: %d truncated=%v err=%v", len(versions), truncated, err)
	}
	tagged, untagged := versions[0], versions[1]
	if tagged.ID != web.ID+"/versions/"+digest || tagged.Name != "v1.4.2, latest" || tagged.Service != model.ServiceArtifactVersion ||
		tagged.ParentID != web.ID || tagged.FacetID != model.FacetVersions || !tagged.Created.Equal(created) {
		t.Fatalf("version: %+v", tagged)
	}
	if err := tagged.Validate(); err != nil {
		t.Fatal(err)
	}
	want := []string{"format", "tags", "digest", "uri", "size", "mediaType"}
	if keys := fieldKeys(tagged); !slices.Equal(keys, want) {
		t.Errorf("fields = %v\nwant %v", keys, want)
	}
	if tagged.Metadata["uri"] != image || tagged.Metadata["size"] != "48.2 MiB" {
		t.Errorf("uri/size = %q %q", tagged.Metadata["uri"], tagged.Metadata["size"])
	}
	wantVerFacets := []model.Facet{
		{ID: model.FacetSignatures, Label: "Signatures", Optional: true},
		{ID: model.FacetVulnerabilities, Label: "Vulnerabilities", Optional: true},
		{ID: model.FacetArtifacts, Label: "Artifacts", Optional: true},
	}
	if !slices.Equal(tagged.Facets, wantVerFacets) {
		t.Errorf("docker version facets = %+v, want %+v", tagged.Facets, wantVerFacets)
	}
	if untagged.Name != "sha256:9b8c7d6e5f4a" {
		t.Errorf("untagged name = %q", untagged.Name)
	}

	// Non-Docker: package facet says Versions (inline), versions have no facets.
	pyPkgs, _, _ := p.ListRelated(ctx, repos[0], model.FacetPackages)
	if len(pyPkgs[0].Facets) != 1 || pyPkgs[0].Facets[0].Label != "Versions" || !pyPkgs[0].Facets[0].Inline {
		t.Errorf("python package facet = %+v", pyPkgs[0].Facets)
	}
	pyVers, _, err := p.ListRelated(ctx, pyPkgs[0], model.FacetVersions)
	if err != nil || len(pyVers) != 1 {
		t.Fatalf("python versions: %v %v", pyVers, err)
	}
	if v := pyVers[0]; v.Name != "2.32.3" || v.Metadata["version"] != "2.32.3" || v.Facets != nil || v.Description != "HTTP for Humans" {
		t.Errorf("python version = %+v", v)
	}
}

func TestListRelatedOrder(t *testing.T) {
	t.Parallel()
	day := func(d int) time.Time { return time.Date(2026, 1, d, 0, 0, 0, 0, time.UTC) }
	tests := []struct {
		name     string
		repo     int // index into the sample's repositories
		pkgs     []Package
		versions []Version
		want     []string // package names, then version names
	}{
		{
			name: "docker",
			repo: 1,
			pkgs: []Package{{Name: "web/frontend"}, {Name: "worker-10"}, {Name: "API"}, {Name: "worker-9"}},
			versions: []Version{
				{Name: "sha256:aaaaaaaaaaaaaaaa", Created: day(1)},
				{Name: "sha256:1", Tags: []string{"v1.10.0"}},
				{Name: "sha256:bbbbbbbbbbbbbbbb", Created: day(3)},
				{Name: "sha256:2", Tags: []string{"v1.9.2"}},
				{Name: "sha256:3", Tags: []string{"v1.9.10", "stable"}},
			},
			want: []string{"API", "web/frontend", "worker-9", "worker-10",
				"v1.9.2", "v1.9.10, stable", "v1.10.0", "sha256:bbbbbbbbbbbb", "sha256:aaaaaaaaaaaa"},
		},
		{
			name:     "python",
			repo:     0,
			pkgs:     []Package{{Name: "requests"}},
			versions: []Version{{Name: "2.32.10"}, {Name: "2.4.0"}, {Name: "2.32.3"}},
			want:     []string{"requests", "2.4.0", "2.32.3", "2.32.10"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			api := sample()
			ctx := context.Background()
			repos, _ := New(api).List(ctx, "projects/p")
			repo := repos[tt.repo]
			api.pkgs[repo.Name] = tt.pkgs
			first := tt.pkgs[0].Name
			api.versions[repo.Name+"/"+first] = tt.versions
			p := New(api)

			pkgs, _, err := p.ListRelated(ctx, repo, model.FacetPackages)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			var parent model.Resource
			for _, r := range pkgs {
				got = append(got, r.Name)
				if r.Name == first {
					parent = r
				}
			}
			versions, _, err := p.ListRelated(ctx, parent, model.FacetVersions)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range versions {
				got = append(got, r.Name)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestListRelatedTruncates(t *testing.T) {
	t.Parallel()
	api := sample()
	many := make([]Package, maxPackages+5)
	for i := range many {
		many[i] = Package{Name: strings.Repeat("x", i+1)}
	}
	api.pkgs["app-images"] = many
	repo := repositoryResource("p", api.repos["us-central1"][0])
	got, truncated, err := New(api).ListRelated(context.Background(), repo, model.FacetPackages)
	if err != nil || !truncated || len(got) != maxPackages {
		t.Fatalf("got %d truncated=%v err=%v", len(got), truncated, err)
	}
}

func TestListRelatedIgnoresOthers(t *testing.T) {
	t.Parallel()
	api := sample()
	api.calls = new(atomic.Int32)
	var urls []string
	p, v := New(api), NewVulnerabilities(fakeScan{urls: &urls})
	repo := model.Resource{ID: "projects/p/locations/us/repositories/r", Service: model.ServiceArtifactRegistry}
	pkg := model.Resource{ID: repo.ID + "/packages/x", Service: model.ServiceArtifactPackage, Metadata: map[string]string{model.MetaArtifactFormat: "DOCKER"}}
	pyPkg := model.Resource{ID: repo.ID + "/packages/x", Service: model.ServiceArtifactPackage, Metadata: map[string]string{model.MetaArtifactFormat: "PYTHON"}}
	version := model.Resource{ID: repo.ID + "/packages/x/versions/sha256:a", Service: model.ServiceArtifactVersion}
	cases := []struct {
		name   string
		lister model.RelatedLister
		parent model.Resource
		facet  string
	}{
		{"other service", p, model.Resource{ID: "projects/p/global/networks/default", Service: model.ServiceVPC}, model.FacetPackages},
		{"undeclared facet", p, repo, model.FacetVersions},
		{"permissions of a package", p, pkg, model.FacetPermissions},
		{"permissions under a package id", p, model.Resource{ID: repo.ID + "/packages/x", Service: model.ServiceArtifactRegistry}, model.FacetPermissions},
		{"malformed repo id", p, model.Resource{ID: "nope", Service: model.ServiceArtifactRegistry}, model.FacetPackages},
		{"package id on a repo", p, model.Resource{ID: repo.ID + "/packages/x", Service: model.ServiceArtifactRegistry}, model.FacetPackages},
		{"signature tags of python package", p, pyPkg, model.FacetSignatureTags},
		{"versions of a version", p, version, model.FacetVersions},
		{"vulnerabilities on provider", p, version, model.FacetVulnerabilities},
		{"signatures of a repo", p, repo, model.FacetSignatures},
		{"signatures malformed id", p, model.Resource{ID: pkg.ID, Service: model.ServiceArtifactVersion}, model.FacetSignatures},
		{"vulns of a repo", v, repo, model.FacetVulnerabilities},
		{"vulns undeclared facet", v, version, model.FacetVersions},
		{"vulns malformed id", v, model.Resource{ID: "projects/p/locations/us/repositories/r", Service: model.ServiceArtifactVersion}, model.FacetVulnerabilities},
	}
	for _, tc := range cases {
		got, truncated, err := tc.lister.ListRelated(context.Background(), tc.parent, tc.facet)
		if got != nil || truncated || err != nil {
			t.Errorf("%s: got %v %v %v", tc.name, got, truncated, err)
		}
	}
	if api.calls.Load() != 0 || len(urls) != 0 {
		t.Errorf("API called: %d AR, %v scan", api.calls.Load(), urls)
	}
}

func TestVulnerabilities(t *testing.T) {
	t.Parallel()
	published := time.Date(2023, 10, 10, 14, 15, 0, 0, time.UTC)
	scan := fakeScan{vulns: map[string][]Vulnerability{"https://" + image: {
		{
			ID: "projects/p/occurrences/o1", CVE: "CVE-2023-44487", Severity: "HIGH", Effective: "HIGH",
			CVSS: 7.5, CVSSVersion: "CVSS_VERSION_3", FixAvailable: true,
			Summary: "HTTP/2 rapid reset", Details: "Long text.", Published: published,
			Packages: []AffectedPackage{
				{Name: "golang.org/x/net", Type: "GO", Installed: "v0.15.0", Fixed: "v0.17.0", Path: "/app/server"},
				{Name: "golang.org/x/net", Type: "GO", Installed: "v0.15.0", Fixed: "v0.17.0", Path: "/app/worker"},
			},
			URLs: []string{"https://nvd.nist.gov/vuln/detail/CVE-2023-44487"},
		},
		{ID: "projects/p/occurrences/o2", CVE: "CVE-2011-3374", Severity: "MEDIUM", Effective: "LOW",
			Details: "only long", Packages: []AffectedPackage{{Name: "apt", Type: "OS", Installed: "2.6.1"}}},
	}}}
	ctx := context.Background()
	p := New(sample())
	repos, _ := p.List(ctx, "projects/p")
	pkgs, _, _ := p.ListRelated(ctx, repos[1], model.FacetPackages)
	versions, _, _ := p.ListRelated(ctx, pkgs[1], model.FacetVersions)

	got, truncated, err := NewVulnerabilities(scan).ListRelated(ctx, versions[0], model.FacetVulnerabilities)
	if err != nil || truncated || len(got) != 2 {
		t.Fatalf("got %d truncated=%v err=%v", len(got), truncated, err)
	}
	high := got[0]
	if high.ID != "projects/p/occurrences/o1" || high.Name != "CVE-2023-44487" || high.Service != model.ServiceVulnerability ||
		high.ScopeID != "projects/p" || high.Location != "us-central1" || high.ParentID != versions[0].ID ||
		high.FacetID != model.FacetVulnerabilities || high.State != "HIGH" || high.Status != model.StatusError ||
		high.Description != "HTTP/2 rapid reset" {
		t.Fatalf("vulnerability: %+v", high)
	}
	if err := high.Validate(); err != nil {
		t.Fatal(err)
	}
	want := []string{"cvss", "fixAvailable", "package", "packageType", "installed", "fixedIn", "path", "image", "published"}
	if keys := fieldKeys(high); !slices.Equal(keys, want) {
		t.Errorf("fields = %v\nwant %v", keys, want)
	}
	if high.Metadata["cvss"] != "7.5 (v3)" || high.Metadata["image"] != image || high.Metadata["fixAvailable"] != "yes" {
		t.Errorf("metadata = %v", high.Metadata)
	}
	if d := high.List("details"); !slices.Equal(d, []string{"Long text."}) {
		t.Errorf("details = %v", d)
	}
	if pk := high.List("packages"); len(pk) != 2 || pk[0] != "golang.org/x/net v0.15.0 → v0.17.0 [GO]" {
		t.Errorf("packages = %v", pk)
	}
	if refs := high.List("references"); len(refs) != 1 {
		t.Errorf("references = %v", refs)
	}

	low := got[1]
	if low.Status != model.StatusWarning || low.Description != "only long" || low.List("details") != nil ||
		low.Metadata["noteSeverity"] != "MEDIUM" || low.Metadata["fixAvailable"] != "no" || low.List("packages") != nil {
		t.Errorf("low = %+v", low)
	}
	if _, ok := low.Metadata["fixedIn"]; ok {
		t.Error("fixedIn set without a fix")
	}
	if line := affectedLine(AffectedPackage{Name: "apt", Installed: "2.6.1", Type: "OS"}); line != "apt 2.6.1 (no fix) [OS]" {
		t.Errorf("affectedLine = %q", line)
	}
}

func TestVulnerabilitiesSortedBySeverityWithBadges(t *testing.T) {
	t.Parallel()
	url := "https://" + image
	scan := fakeScan{vulns: map[string][]Vulnerability{url: {
		{ID: "projects/p/occurrences/1", CVE: "CVE-1", Effective: "LOW", CVSS: 3.1},
		{ID: "projects/p/occurrences/2", CVE: "CVE-2", Effective: "SEVERITY_UNSPECIFIED"},
		{ID: "projects/p/occurrences/3", CVE: "CVE-3", Effective: "HIGH", CVSS: 7.5},
		{ID: "projects/p/occurrences/4", CVE: "CVE-4", Effective: "CRITICAL", CVSS: 9.1},
		{ID: "projects/p/occurrences/5", CVE: "CVE-5", Effective: "HIGH", CVSS: 8.8},
		{ID: "projects/p/occurrences/6", CVE: "CVE-6", Effective: "MEDIUM", CVSS: 5.3},
		{ID: "projects/p/occurrences/7", CVE: "CVE-0", Effective: "HIGH", CVSS: 7.5},
		{ID: "projects/p/occurrences/8", CVE: "CVE-8", Effective: "MINIMAL"},
	}}}
	version := model.Resource{ID: "projects/p/locations/us-central1/repositories/app-images/packages/web%2Ffrontend/versions/" + digest, Service: model.ServiceArtifactVersion}
	got, _, err := NewVulnerabilities(scan).ListRelated(context.Background(), version, model.FacetVulnerabilities)
	if err != nil {
		t.Fatal(err)
	}
	var names, badges []string
	for _, r := range got {
		names = append(names, r.Name)
		badges = append(badges, r.Badge)
	}
	wantNames := []string{"CVE-4", "CVE-5", "CVE-0", "CVE-3", "CVE-6", "CVE-1", "CVE-8", "CVE-2"}
	if !slices.Equal(names, wantNames) {
		t.Errorf("order = %v\nwant %v", names, wantNames)
	}
	wantBadges := []string{"CRITICAL", "HIGH", "HIGH", "HIGH", "MEDIUM", "LOW", "MINIMAL", ""}
	if !slices.Equal(badges, wantBadges) {
		t.Errorf("badges = %v\nwant %v", badges, wantBadges)
	}
	if first := scan.vulns[url][0].CVE; first != "CVE-1" {
		t.Errorf("sorting must not reorder the API's slice: first = %s", first)
	}
}

func TestVulnerabilitiesListIsEmpty(t *testing.T) {
	t.Parallel()
	got, err := NewVulnerabilities(fakeScan{err: errors.New("unused")}).List(context.Background(), "projects/p")
	if got != nil || err != nil {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestDomainScopedProjectImage(t *testing.T) {
	t.Parallel()
	if got, want := imageRef("example.com:p", "us", "r", "a/b", "sha256:x"), "us-docker.pkg.dev/example.com/p/r/a/b@sha256:x"; got != want {
		t.Errorf("imageRef = %q, want %q", got, want)
	}
}

func TestErrorsPropagate(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	p := New(fakeAPI{err: boom})
	ctx := context.Background()
	if _, err := p.List(ctx, "projects/p"); !errors.Is(err, boom) {
		t.Errorf("List err = %v", err)
	}
	repo := model.Resource{ID: "projects/p/locations/us/repositories/r", Service: model.ServiceArtifactRegistry}
	if _, _, err := p.ListRelated(ctx, repo, model.FacetPackages); !errors.Is(err, boom) {
		t.Errorf("ListRelated packages err = %v", err)
	}
	if _, _, err := p.ListRelated(ctx, repo, model.FacetPermissions); !errors.Is(err, boom) {
		t.Errorf("ListRelated permissions err = %v", err)
	}
	pkg := model.Resource{ID: repo.ID + "/packages/x", Service: model.ServiceArtifactPackage}
	if _, _, err := p.ListRelated(ctx, pkg, model.FacetVersions); !errors.Is(err, boom) {
		t.Errorf("ListRelated versions err = %v", err)
	}
	version := model.Resource{ID: pkg.ID + "/versions/sha256:a", Service: model.ServiceArtifactVersion}
	if _, _, err := p.ListRelated(ctx, version, model.FacetSignatures); !errors.Is(err, boom) {
		t.Errorf("ListRelated signatures err = %v", err)
	}
	if _, _, err := NewVulnerabilities(fakeScan{err: boom}).ListRelated(ctx, version, model.FacetVulnerabilities); !errors.Is(err, boom) {
		t.Errorf("vulnerabilities err = %v", err)
	}
}

func TestNilAPI(t *testing.T) {
	t.Parallel()
	if _, err := New(nil).List(context.Background(), "projects/p"); err == nil {
		t.Error("List with nil API should fail")
	}
	version := model.Resource{ID: "projects/p/locations/us/repositories/r/packages/x/versions/sha256:a", Service: model.ServiceArtifactVersion}
	if _, _, err := NewVulnerabilities(nil).ListRelated(context.Background(), version, model.FacetVulnerabilities); err == nil {
		t.Error("ListRelated with nil scan API should fail")
	}
}

func fieldKeys(r model.Resource) []string {
	out := make([]string, 0, len(r.Fields))
	for _, f := range r.Fields {
		out = append(out, f.Key)
	}
	return out
}

func TestRepositoryPolicyFields(t *testing.T) {
	t.Parallel()
	r := repositoryResource("p", Repository{
		Name: "hub", Location: "us", Format: "DOCKER", Mode: "REMOTE_REPOSITORY", RemoteSource: "DOCKER_HUB",
		UpstreamUser: "ci", UpstreamSecret: "projects/p/secrets/hub/versions/2", NoUpstreamValidate: true,
		CleanupPolicies: 1, CleanupRules: []string{"keep-recent  KEEP  5 most recent"},
	})
	if r.Metadata["remoteSource"] != "DOCKER_HUB" || r.Metadata["upstreamCredentials"] != "ci (password in projects/p/secrets/hub/versions/2)" ||
		r.Metadata["upstreamValidation"] != "off" || !slices.Equal(r.List("cleanupPolicies"), []string{"keep-recent  KEEP  5 most recent"}) {
		t.Fatalf("remote repo = %+v %+v", r.Metadata, r.Lists)
	}
	v := repositoryResource("p", Repository{Name: "all", Location: "us", Format: "MAVEN", Mode: "VIRTUAL_REPOSITORY", VirtualUpstreams: 1,
		UpstreamRules: []string{"priority 10  projects/p/locations/us/repositories/internal"}, MavenPolicy: "SNAPSHOT", MavenOverwrites: true})
	if v.Metadata["versionPolicy"] != "SNAPSHOT" || v.Metadata["snapshotOverwrites"] != "on" || len(v.List("upstreams")) != 1 {
		t.Fatalf("virtual repo = %+v %+v", v.Metadata, v.Lists)
	}
}

func TestSplitDigestsAndSignatureTags(t *testing.T) {
	t.Parallel()
	api := sample()
	api.versions["app-images/web/frontend"] = []Version{
		{Name: digest, Tags: []string{"v1.4.2", "latest"}, ArtifactType: "application/vnd.oci.image.manifest.v1+json", Created: created},
		{Name: "sha256:sig1111111111111", Tags: []string{"sha256-3f9a1c2b7d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8.sig"}},
		{Name: "sha256:sig2222222222222", Tags: []string{"prod-sha256-abcdef123456.sig"}},
		{Name: "sha256:mixed0000000000", Tags: []string{"v1.0.0", "weird.sig"}},
		{Name: "sha256:9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e", Created: created},
	}
	pkg := model.Resource{
		ID:       "projects/p/locations/us-central1/repositories/app-images/packages/web%2Ffrontend",
		Service:  model.ServiceArtifactPackage,
		ScopeID:  "projects/p",
		Location: "us-central1",
		Metadata: map[string]string{model.MetaArtifactFormat: "DOCKER"},
	}
	p := New(api)
	ctx := context.Background()

	digests, trunc, err := p.ListRelated(ctx, pkg, model.FacetVersions)
	if err != nil || trunc {
		t.Fatalf("digests: trunc=%v err=%v", trunc, err)
	}
	if len(digests) != 3 {
		t.Fatalf("digests len = %d, want 3", len(digests))
	}
	if digests[0].Name != "v1.0.0, weird.sig" || digests[1].Name != "v1.4.2, latest" || digests[2].Name != "sha256:9b8c7d6e5f4a" {
		t.Fatalf("unexpected digest order: %q %q %q", digests[0].Name, digests[1].Name, digests[2].Name)
	}
	if digests[1].Metadata["artifactType"] != "application/vnd.oci.image.manifest.v1+json" {
		t.Errorf("artifactType = %q", digests[1].Metadata["artifactType"])
	}
	for _, d := range digests {
		if d.FacetID != model.FacetVersions || len(d.Facets) != 3 {
			t.Errorf("digest %s: facet=%q facets=%+v", d.Name, d.FacetID, d.Facets)
		}
	}

	sigTags, trunc, err := p.ListRelated(ctx, pkg, model.FacetSignatureTags)
	if err != nil || trunc {
		t.Fatalf("sigTags: trunc=%v err=%v", trunc, err)
	}
	if len(sigTags) != 2 {
		t.Fatalf("sigTags len = %d, want 2", len(sigTags))
	}
	if sigTags[0].FacetID != model.FacetSignatureTags || sigTags[0].Facets != nil {
		t.Errorf("sigTag facet/facets = %q %+v", sigTags[0].FacetID, sigTags[0].Facets)
	}
	if got, want := sigTags[0].Metadata["signedDigest"], "sha256:abcdef123456"; got != want {
		t.Errorf("prefixed signedDigest = %q, want %q", got, want)
	}
	if got, want := sigTags[1].Metadata["signedDigest"], digest; got != want {
		t.Errorf("signedDigest = %q, want %q", got, want)
	}

	// Truncation: empty filtered list never reports truncated=true; >maxVersions caps and sets truncated=true.
	many := make([]Version, maxVersions+25)
	for i := range many {
		many[i] = Version{Name: "sha256:" + strconv.Itoa(i)}
	}
	api.versions["app-images/web/frontend"] = many
	gotMany, trunc, err := New(api).ListRelated(ctx, pkg, model.FacetVersions)
	if err != nil || !trunc || len(gotMany) != maxVersions {
		t.Fatalf("capped digests: len=%d trunc=%v err=%v", len(gotMany), trunc, err)
	}
	gotEmptySig, trunc, err := New(api).ListRelated(ctx, pkg, model.FacetSignatureTags)
	if err != nil || trunc || len(gotEmptySig) != 0 {
		t.Fatalf("empty filtered sigTags must not truncate: len=%d trunc=%v err=%v", len(gotEmptySig), trunc, err)
	}
}

func TestSignaturesFacet(t *testing.T) {
	t.Parallel()
	t1 := time.Date(2025, 10, 1, 12, 41, 0, 0, time.UTC)
	t2 := time.Date(2025, 10, 1, 12, 42, 0, 0, time.UTC)

	v2Cert := testCertPEM(t, []string{"alice@example.com"}, nil, oidFulcioIssuerV2, "https://accounts.google.com", true)
	v1Cert := testCertPEM(t, nil, []string{"https://github.com/example/web/.github/workflows/release.yml@refs/heads/main"}, oidFulcioIssuerV1, "https://token.actions.githubusercontent.com", false)
	plainCert := testCertPEM(t, []string{"ops@example.com"}, nil, nil, "", false)

	api := sample()
	key := "app-images/web/frontend@" + digest
	api.sigs = map[string][]Signature{
		key: {
			{
				ID:             "sha256:key0",
				Source:         "tag",
				Tag:            "sha256-abc.sig",
				ManifestDigest: "sha256:key0",
				ArtifactType:   "application/vnd.dev.cosign.simplesigning.v1+json",
				Signature:      "a2V5c2ln",
				KeyHint:        "gcpkms://key-1",
				HasRekor:       true,
				RekorLogIndex:  0,
			},
			{
				ID:             "sha256:v2cert",
				Source:         "referrer",
				ManifestDigest: "sha256:v2cert",
				ArtifactType:   "application/vnd.dev.sigstore.bundle.v0.3+json",
				PredicateType:  "https://sigstore.dev/cosign/sign/v1",
				PayloadDigest:  "sha256:payloadv2",
				Signature:      "djJzaWc=",
				CertificatePEM: v2Cert,
				BundleJSON:     `{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json"}`,
				HasRekor:       true,
				RekorLogIndex:  142,
				RekorLogID:     "logid-v2",
				IntegratedTime: t1,
			},
			{
				ID:             "sha256:v1cert",
				Source:         "tag",
				Tag:            "sha256-abc.sig",
				ManifestDigest: "sha256:v1cert",
				ArtifactType:   "application/vnd.dev.cosign.simplesigning.v1+json",
				Signature:      "djFzaWc=",
				CertificatePEM: v1Cert,
				ChainPEM:       []string{plainCert},
				IntegratedTime: t2,
			},
			{
				ID:             "sha256:plaincert",
				Source:         "referrer",
				ManifestDigest: "sha256:plaincert",
				ArtifactType:   "application/vnd.dev.cosign.artifact.sig.v1+json",
				Signature:      "cGxhaW4=",
				CertificatePEM: plainCert,
				ChainPEM:       []string{plainCert, "not-pem"},
			},
		},
	}

	version := model.Resource{
		ID:       "projects/p/locations/us-central1/repositories/app-images/packages/web%2Ffrontend/versions/" + digest,
		Service:  model.ServiceArtifactVersion,
		ScopeID:  "projects/p",
		Location: "us-central1",
	}
	got, trunc, err := New(api).ListRelated(context.Background(), version, model.FacetSignatures)
	if err != nil || trunc || len(got) != 4 {
		t.Fatalf("sigs: len=%d trunc=%v err=%v", len(got), trunc, err)
	}
	// Sorted newest IntegratedTime first (t2, t1), then zero IntegratedTime sorted naturally by name.
	wantOrder := []string{
		"https://github.com/example/web/.github/workflows/release.yml@refs/heads/main",
		"alice@example.com",
		"gcpkms://key-1",
		"ops@example.com",
	}
	var gotNames []string
	for _, r := range got {
		if err := r.Validate(); err != nil {
			t.Fatalf("validate %s: %v", r.ID, err)
		}
		if r.Status != model.StatusUnknown || r.State != "" || r.Service != model.ServiceArtifactSignature ||
			r.ParentID != version.ID || r.FacetID != model.FacetSignatures {
			t.Errorf("unexpected signature resource: %+v", r)
		}
		gotNames = append(gotNames, r.Name)
	}
	if !slices.Equal(gotNames, wantOrder) {
		t.Errorf("order = %q\nwant %q", gotNames, wantOrder)
	}

	// Verify input slice was not mutated.
	if api.sigs[key][0].ID != "sha256:key0" {
		t.Errorf("input slice mutated: first = %s", api.sigs[key][0].ID)
	}

	// Check Fulcio v2 keyless details & copies (index 1).
	v2 := got[1]
	if v2.Badge != "KEYLESS" || v2.Metadata["signing"] != "keyless" ||
		v2.Metadata["issuer"] != "https://accounts.google.com" ||
		v2.Metadata["source"] != "OCI referrer" ||
		v2.Metadata["rekorLogIndex"] != "142" ||
		v2.Metadata["rekorLogId"] != "logid-v2" ||
		v2.Metadata["integratedTime"] != "2025-10-01 12:41 UTC" ||
		v2.Metadata["verified"] != "no (seiner does not verify signatures)" ||
		v2.Metadata[model.MetaArtifactImage] != image {
		t.Errorf("v2 metadata = %+v", v2.Metadata)
	}
	if len(v2.Copies) != 3 || v2.Copies[0].Title != "Signature" || v2.Copies[1].Title != "Certificate (PEM)" || v2.Copies[2].Title != "Sigstore bundle (JSON)" {
		t.Errorf("v2 copies = %+v", v2.Copies)
	}
	wantFields := []string{"signing", "identity", "issuer", "source", "artifactType", "predicateType",
		"manifestDigest", "payloadDigest", "rekorLogIndex", "rekorLogId", "integratedTime",
		"certificateValid", "serial", "image", "verified"}
	if keys := fieldKeys(v2); !slices.Equal(keys, wantFields) {
		t.Errorf("v2 fields = %v\nwant %v", keys, wantFields)
	}

	// Check key-only signature with RekorLogIndex == 0 (index 2).
	keyRes := got[2]
	if keyRes.Badge != "KEY" || keyRes.Metadata["signing"] != "key" || keyRes.Metadata["rekorLogIndex"] != "0" ||
		keyRes.Metadata["source"] != "tag (sha256-abc.sig)" {
		t.Errorf("keyRes = %+v", keyRes.Metadata)
	}

	// Check non-Fulcio certificate signature with chain (index 3).
	certRes := got[3]
	if certRes.Badge != "CERT" || certRes.Metadata["signing"] != "certificate" {
		t.Errorf("certRes = %s %+v", certRes.Badge, certRes.Metadata)
	}
	if chain := certRes.List("chain"); len(chain) != 2 || !strings.Contains(chain[1], "not parsed") {
		t.Errorf("chain = %v", chain)
	}
	if len(certRes.Copies) != 3 || certRes.Copies[2].Title != "Certificate chain (PEM)" {
		t.Errorf("certRes copies = %+v", certRes.Copies)
	}

	// Truncation at maxSignatures.
	manySigs := make([]Signature, maxSignatures+3)
	for i := range manySigs {
		manySigs[i] = Signature{ID: "sha256:" + strconv.Itoa(i), ManifestDigest: "sha256:" + strconv.Itoa(i)}
	}
	api.sigs[key] = manySigs
	gotMany, trunc, err := New(api).ListRelated(context.Background(), version, model.FacetSignatures)
	if err != nil || !trunc || len(gotMany) != maxSignatures {
		t.Fatalf("sigs truncation: len=%d trunc=%v err=%v", len(gotMany), trunc, err)
	}
}

func testCertPEM(t *testing.T, emails, uris []string, extOID asn1.ObjectIdentifier, issuerVal string, derString bool) string {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:   big.NewInt(42),
		Subject:        pkix.Name{CommonName: "test-signer"},
		Issuer:         pkix.Name{CommonName: "Test CA"},
		NotBefore:      time.Date(2025, 10, 1, 12, 40, 0, 0, time.UTC),
		NotAfter:       time.Date(2025, 10, 1, 12, 50, 0, 0, time.UTC),
		EmailAddresses: emails,
	}
	for _, rawURI := range uris {
		u, err := url.Parse(rawURI)
		if err != nil {
			t.Fatal(err)
		}
		tmpl.URIs = append(tmpl.URIs, u)
	}
	if extOID != nil {
		val := []byte(issuerVal)
		if derString {
			val, err = asn1.Marshal(issuerVal)
			if err != nil {
				t.Fatal(err)
			}
		}
		tmpl.ExtraExtensions = []pkix.Extension{{Id: extOID, Value: val}}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestAttachedArtifactsFilteredFromDigestsAndListedUnderParent(t *testing.T) {
	t.Parallel()
	const (
		parentDigest     = "sha256:1111111111111111"
		referrerDigest   = "sha256:2222222222222222"
		childImgDigest   = "sha256:3333333333333333"
		standaloneDigest = "sha256:4444444444444444"
	)
	api := sample()
	api.versions["app-images/web/frontend"] = []Version{
		{Name: parentDigest, Tags: []string{"v1.0.0"}, MediaType: "application/vnd.oci.image.index.v1+json", Created: created},
		{Name: referrerDigest, ArtifactType: "application/spdx+json", SubjectDigest: parentDigest, Created: created},
		{Name: childImgDigest, ParentDigest: parentDigest, Platform: "linux/amd64", Created: created},
		{Name: standaloneDigest, Created: created},
	}
	api.artifacts = map[string][]Version{
		"app-images/web/frontend@" + parentDigest: {
			{Name: referrerDigest, ArtifactType: "application/spdx+json", SubjectDigest: parentDigest},
			{Name: childImgDigest, ParentDigest: parentDigest, Platform: "linux/amd64"},
		},
	}
	p := New(api)
	ctx := context.Background()

	pkg := model.Resource{
		ID:       "projects/p/locations/us-central1/repositories/app-images/packages/web%2Ffrontend",
		Service:  model.ServiceArtifactPackage,
		ScopeID:  "projects/p",
		Location: "us-central1",
		Metadata: map[string]string{model.MetaArtifactFormat: "DOCKER"},
	}
	digests, _, err := p.ListRelated(ctx, pkg, model.FacetVersions)
	if err != nil {
		t.Fatalf("ListRelated digests: %v", err)
	}
	if len(digests) != 2 || digests[0].Name != "v1.0.0" || digests[1].Name != "sha256:444444444444" {
		t.Fatalf("digests = %+v, want only v1.0.0 and standalone", digests)
	}

	artifacts, _, err := p.ListRelated(ctx, digests[0], model.FacetArtifacts)
	if err != nil {
		t.Fatalf("ListRelated artifacts: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("artifacts len = %d, want 2", len(artifacts))
	}
	if artifacts[0].ParentID != digests[0].ID || artifacts[0].FacetID != model.FacetArtifacts || len(artifacts[0].Facets) != 0 {
		t.Errorf("referrer artifact = %+v, want no facets and parent=%s", artifacts[0], digests[0].ID)
	}
	if artifacts[1].Name != "linux/amd64 (sha256:333333333333)" || len(artifacts[1].Facets) != 3 {
		t.Errorf("platform child = %+v", artifacts[1])
	}
}
