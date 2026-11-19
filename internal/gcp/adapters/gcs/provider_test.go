package gcs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	buckets  []Bucket
	objects  map[string][]Object
	policies map[string][]PolicyBinding
	err      error
}

func (f fakeAPI) ListBuckets(context.Context, string) ([]Bucket, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.buckets, nil
}

func (f fakeAPI) ListObjects(_ context.Context, bucket string, limit int) ([]Object, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	objs := f.objects[bucket]
	if limit > 0 && len(objs) > limit {
		return objs[:limit], true, nil
	}
	return objs, false, nil
}

func (f fakeAPI) GetBucketPolicy(_ context.Context, bucket string) ([]PolicyBinding, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.policies[bucket], nil
}

func sample() []Bucket {
	return []Bucket{
		{Name: "my-app-bucket", Location: "US-CENTRAL1", StorageClass: "STANDARD", Versioning: true,
			Created: time.Date(2024, 2, 11, 9, 30, 0, 0, time.UTC), Labels: map[string]string{"env": "prod"}},
		{Name: "audit-logs", Location: "US-EAST1", StorageClass: "NEARLINE"},
	}
}

func TestListMapsBuckets(t *testing.T) {
	t.Parallel()
	p := New(fakeAPI{buckets: sample()}, "demo-project")
	got, err := p.List(context.Background(), "projects/demo-project")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len %d", len(got))
	}
	r := got[0]
	if r.ID != "projects/demo-project/buckets/my-app-bucket" {
		t.Fatalf("id %s", r.ID)
	}
	if r.Service != model.ServiceCloudStorage || r.ScopeID != "projects/demo-project" {
		t.Fatalf("%+v", r)
	}
	if r.Metadata["storageClass"] != "STANDARD" || r.Metadata["versioning"] != "enabled" {
		t.Fatalf("metadata %+v", r.Metadata)
	}
	if !r.Created.Equal(sample()[0].Created) || r.Labels["env"] != "prod" {
		t.Fatalf("created=%v labels=%v", r.Created, r.Labels)
	}
	if r.ParentID != "" {
		t.Fatal("buckets are top-level")
	}
	if len(r.Facets) != 2 {
		t.Fatalf("facets %v", r.Facets)
	}
}

func TestListRelated(t *testing.T) {
	t.Parallel()
	p := New(fakeAPI{
		buckets: sample(),
		objects: map[string][]Object{
			"my-app-bucket": {{Name: "logos/logo.png", Size: 12, ContentType: "image/png"}},
		},
		policies: map[string][]PolicyBinding{
			"my-app-bucket": {{Role: "roles/storage.admin", Members: []string{"user:a@x"}}},
		},
	}, "demo-project")
	parent := ResourceFrom("demo-project", sample()[0])
	objs, truncated, err := p.ListRelated(context.Background(), parent, model.FacetObjects)
	if err != nil || len(objs) != 1 || objs[0].Name != "logos/logo.png" {
		t.Fatalf("objects %v err=%v", objs, err)
	}
	if truncated {
		t.Fatal("one object should not be truncated")
	}
	if objs[0].ParentID != parent.ID || objs[0].FacetID != model.FacetObjects {
		t.Fatalf("%+v", objs[0])
	}
	perms, _, err := p.ListRelated(context.Background(), parent, model.FacetPermissions)
	if err != nil || len(perms) != 1 || perms[0].Name != "storage.admin" {
		t.Fatalf("perms %v err=%v", perms, err)
	}
	if perms[0].Service != model.ServiceIAMBinding {
		t.Fatalf("service %s", perms[0].Service)
	}
}

func TestListRelatedTruncatedObjects(t *testing.T) {
	t.Parallel()
	objs := make([]Object, maxObjects+5)
	for i := range objs {
		objs[i] = Object{Name: fmt.Sprintf("o-%03d", i)}
	}
	p := New(fakeAPI{buckets: sample(), objects: map[string][]Object{"my-app-bucket": objs}}, "demo-project")
	parent := ResourceFrom("demo-project", sample()[0])
	got, truncated, err := p.ListRelated(context.Background(), parent, model.FacetObjects)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(got) != maxObjects {
		t.Fatalf("truncated=%v len=%d", truncated, len(got))
	}
	for _, r := range got {
		if _, ok := r.Metadata["truncated"]; ok {
			t.Fatalf("%s carries a truncated metadata key", r.Name)
		}
	}
}

func TestListOrgScopeEmpty(t *testing.T) {
	t.Parallel()
	p := New(fakeAPI{buckets: sample()}, "demo-project")
	got, err := p.List(context.Background(), "organizations/9")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestListUsesScopeProject(t *testing.T) {
	t.Parallel()
	p := New(fakeAPI{buckets: sample()}, "")
	got, err := p.List(context.Background(), "projects/other-project")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len %d", len(got))
	}
	if got[0].ScopeID != "projects/other-project" {
		t.Fatalf("scope %s", got[0].ScopeID)
	}
}

func TestListError(t *testing.T) {
	t.Parallel()
	boom := errors.New("permission denied")
	p := New(fakeAPI{err: boom}, "demo-project")
	_, err := p.List(context.Background(), "projects/demo-project")
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}

func TestParseBucketID(t *testing.T) {
	t.Parallel()
	project, name := parseBucketID("projects/p/buckets/b", "fallback")
	if project != "p" || name != "b" {
		t.Fatalf("%s %s", project, name)
	}
	project, name = parseBucketID("plain-bucket", "fallback")
	if project != "fallback" || name != "plain-bucket" {
		t.Fatalf("%s %s", project, name)
	}
	for _, id := range []string{
		"projects/p/buckets/b/objects/a%2Fb.txt",
		"projects/p/buckets/b/iam/bindings/roles%2Fowner",
		"projects/p/global/networks/default",
	} {
		if _, name := parseBucketID(id, "fallback"); name != "" {
			t.Errorf("parseBucketID(%q) name=%q want none", id, name)
		}
	}
}

func TestBucketDetailFields(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		b        Bucket
		wantKeys []string
		want     map[string]string
	}{
		{
			name:     "minimal",
			b:        Bucket{Name: "b"},
			wantKeys: []string{"access", "versioning"},
			want:     map[string]string{"access": "fine-grained", "versioning": "disabled"},
		},
		{
			name: "full",
			b: Bucket{Name: "b", StorageClass: "STANDARD", LocationType: "multi-region", UniformAccess: true,
				PublicAccess: "enforced", Versioning: true, SoftDelete: 7 * 24 * time.Hour,
				Retention: 24 * time.Hour, RetentionLock: true, LifecycleRules: 2, Autoclass: true,
				RequesterPays: true, KMSKey: "k"},
			wantKeys: []string{"storageClass", "locationType", "access", "publicAccessPrevention", "versioning",
				"softDelete", "retention", "lifecycleRules", "autoclass", "requesterPays", "kmsKey"},
			want: map[string]string{"softDelete": "7 days", "retention": "1 day (locked)", "access": "uniform",
				"requesterPays": "enabled; objects not listed"},
		},
		{
			name: "dual-region with logging and holds",
			b: Bucket{Name: "b", LocationType: "dual-region", DataLocations: []string{"US-EAST1", "US-WEST1"}, TurboReplication: true,
				HNS: true, ObjectRetention: "Enabled", EventBasedHold: true, LogBucket: "access-logs"},
			wantKeys: []string{"locationType", "dataLocations", "turboReplication", "access", "versioning", "objectRetention",
				"defaultEventBasedHold", "hierarchicalNamespace", "accessLogs"},
			want: map[string]string{"dataLocations": "US-EAST1, US-WEST1", "turboReplication": "enabled", "accessLogs": "access-logs"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := ResourceFrom("p", tc.b)
			var keys []string
			for _, f := range r.Fields {
				keys = append(keys, f.Key)
			}
			if fmt.Sprint(keys) != fmt.Sprint(tc.wantKeys) {
				t.Errorf("keys %v, want %v", keys, tc.wantKeys)
			}
			for k, v := range tc.want {
				if r.Metadata[k] != v {
					t.Errorf("%s = %q, want %q", k, r.Metadata[k], v)
				}
			}
		})
	}
}

func TestHumanSize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"}, {1023, "1023 B"}, {2048, "2.0 KiB"}, {151552, "148.0 KiB"}, {5 << 30, "5.0 GiB"},
	}
	for _, tc := range cases {
		if got := humanSize(tc.n); got != tc.want {
			t.Errorf("humanSize(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestBindingListsAllPrincipals(t *testing.T) {
	t.Parallel()
	members := []string{"user:a", "user:b", "user:c", "user:d"}
	r := bindingResource("p", "b", "projects/p/buckets/b", PolicyBinding{Role: "roles/storage.admin", Members: members})
	if got := r.List(model.ListMembers); !slices.Equal(got, members) {
		t.Errorf("members %v", got)
	}
	if r.ParentID != "projects/p/buckets/b" || r.FacetID != model.FacetPermissions || r.ID != "projects/p/buckets/b/iam/bindings/roles%2Fstorage.admin" {
		t.Errorf("%+v", r)
	}
}

func TestConditionalBindingKeepsCondition(t *testing.T) {
	t.Parallel()
	plain := bindingResource("p", "b", "projects/p/buckets/b", PolicyBinding{Role: "roles/storage.objectViewer", Members: []string{"allUsers"}})
	cond := bindingResource("p", "b", "projects/p/buckets/b", PolicyBinding{Role: "roles/storage.objectViewer", Members: []string{"user:a"}, Condition: "prefix"})
	if plain.ID == cond.ID {
		t.Errorf("same role with and without a condition share id %q", cond.ID)
	}
	if got := cond.Metadata["condition"]; got != "prefix" {
		t.Errorf("condition %q", got)
	}
}

// A requester-pays bucket has no Objects facet; reading them would bill a
// project.
func TestRequesterPaysBucketHasNoObjects(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		pays bool
		want []string
	}{
		{false, []string{model.FacetPermissions, model.FacetObjects}},
		{true, []string{model.FacetPermissions}},
	} {
		var got []string
		for _, f := range ResourceFrom("p", Bucket{Name: "b", RequesterPays: tc.pays}).Facets {
			got = append(got, f.ID)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("requesterPays=%v facets %v, want %v", tc.pays, got, tc.want)
		}
	}
}
