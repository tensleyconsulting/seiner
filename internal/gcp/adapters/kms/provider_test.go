package kms

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	locations  []string
	rings      map[string][]KeyRing // keyed by location
	keys       map[string][]Key     // keyed by ring name
	versions   map[string][]KeyVersion
	versErr    map[string]error // ListVersions fails for these keys
	jobs       map[string][]ImportJob
	policies   map[string][]Binding   // keyed by ring, or ring/key
	attest     map[string]Attestation // keyed by key/version
	onVersions func()                 // called by ListVersions, before it returns
	locErr     map[string]error       // ListKeyRings fails for these locations
	err        error
	mu         *sync.Mutex
	calls      *int
}

func (f fakeAPI) called() {
	if f.calls != nil {
		f.mu.Lock()
		*f.calls++
		f.mu.Unlock()
	}
}

func (f fakeAPI) ListLocations(context.Context, string) ([]string, error) {
	f.called()
	if f.err != nil {
		return nil, f.err
	}
	return f.locations, nil
}

func (f fakeAPI) ListKeyRings(_ context.Context, _, location string) ([]KeyRing, error) {
	f.called()
	if err := f.locErr[location]; err != nil {
		return nil, err
	}
	return f.rings[location], nil
}

func (f fakeAPI) ListKeys(_ context.Context, _, _, ring string, limit int) ([]Key, bool, error) {
	f.called()
	if f.err != nil {
		return nil, false, f.err
	}
	ks := f.keys[ring]
	if len(ks) > limit {
		return ks[:limit], true, nil
	}
	return ks, false, nil
}

func (f fakeAPI) ListVersions(_ context.Context, _, _, _, key string, limit int) ([]KeyVersion, bool, error) {
	f.called()
	if f.onVersions != nil {
		f.onVersions()
	}
	if f.err != nil {
		return nil, false, f.err
	}
	if err := f.versErr[key]; err != nil {
		return nil, false, err
	}
	vs := slices.Clone(f.versions[key])
	if len(vs) > limit {
		return vs[:limit], true, nil
	}
	return vs, false, nil
}

func (f fakeAPI) ListImportJobs(_ context.Context, _, _, ring string, limit int) ([]ImportJob, bool, error) {
	f.called()
	if f.err != nil {
		return nil, false, f.err
	}
	js := f.jobs[ring]
	if len(js) > limit {
		return js[:limit], true, nil
	}
	return js, false, nil
}

func (f fakeAPI) GetAttestation(_ context.Context, _, _, _, key, version string) (Attestation, bool, error) {
	f.called()
	if f.err != nil {
		return Attestation{}, false, f.err
	}
	a, ok := f.attest[key+"/"+version]
	return a, ok, nil
}

func (f fakeAPI) GetIAMPolicy(_ context.Context, _, _, ring, key string) ([]Binding, error) {
	f.called()
	if f.err != nil {
		return nil, f.err
	}
	if key != "" {
		ring += "/" + key
	}
	return f.policies[ring], nil
}

func (f fakeAPI) GetImportJobIAMPolicy(_ context.Context, _, _, ring, job string) ([]Binding, error) {
	f.called()
	if f.err != nil {
		return nil, f.err
	}
	return f.policies[ring+"/importJobs/"+job], nil
}

var created = time.Date(2024, 4, 1, 9, 0, 0, 0, time.UTC)

func sample() fakeAPI {
	return fakeAPI{
		locations: []string{"global", "us-central1", "us-east1"},
		rings: map[string][]KeyRing{
			"us-central1": {{Name: "app-keys", Location: "us-central1", Created: created}},
			"us-east1":    {{Name: "audit-keys", Location: "us-east1"}},
		},
		keys: map[string][]Key{
			"app-keys": {
				{
					Name: "secrets", Purpose: "ENCRYPT_DECRYPT", Algorithm: "GOOGLE_SYMMETRIC_ENCRYPTION",
					ProtectionLevel: "SOFTWARE", Primary: "4", PrimaryState: "ENABLED", RotationPeriod: "7776000s",
					NextRotation: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), DestroyDelay: "2592000s",
					Created: created, Labels: map[string]string{"app": "web"},
				},
				{Name: "signing", Purpose: "ASYMMETRIC_SIGN", Algorithm: "EC_SIGN_P256_SHA256", ProtectionLevel: "HSM",
					TemplateAlgorithm: "EC_SIGN_P384_SHA384", TemplateProtection: "HSM",
					AccessReasons: []string{"CUSTOMER_INITIATED_ACCESS", "GOOGLE_INITIATED_SERVICE"}},
				{Name: "old", Purpose: "ENCRYPT_DECRYPT", Primary: "2", PrimaryState: "DISABLED"},
			},
		},
		versions: map[string][]KeyVersion{
			"secrets": {
				{Name: "3", State: "DESTROYED", Destroyed: created},
				{Name: "10", State: "ENABLED", Algorithm: "GOOGLE_SYMMETRIC_ENCRYPTION", ProtectionLevel: "SOFTWARE", Created: created},
				{Name: "4", State: "ENABLED", Generated: created},
				{Name: "9", State: "DESTROY_SCHEDULED", DestroyScheduled: created.Add(24 * time.Hour)},
			},
			"signing": {
				{Name: "1", State: "DESTROY_SCHEDULED"},
				{Name: "2", State: "ENABLED"},
			},
		},
		jobs: map[string][]ImportJob{
			"app-keys": {{Name: "migrate", State: "ACTIVE", Method: "RSA_OAEP_3072_SHA256", ProtectionLevel: "HSM", Expires: created.Add(72 * time.Hour)}},
		},
		policies: map[string][]Binding{
			"app-keys":         {{Role: "roles/cloudkms.admin", Members: []string{"group:sec@example.com"}}},
			"app-keys/secrets": {{Role: "roles/cloudkms.cryptoKeyEncrypterDecrypter", Members: []string{"serviceAccount:app@p.iam.gserviceaccount.com"}, Condition: "business hours"}},
		},
		mu: &sync.Mutex{},
	}
}

func TestListScopes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		scope     string
		wantCalls int
		wantN     int
	}{
		{"projects/p", 4, 2}, // locations + one call per location
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

func TestRingMapping(t *testing.T) {
	t.Parallel()
	got, err := New(sample()).List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	r := got[0]
	if r.ID != "projects/p/locations/us-central1/keyRings/app-keys" || r.Name != "app-keys" || r.Service != model.ServiceKMS ||
		r.ScopeID != "projects/p" || r.Location != "us-central1" || r.Status != "" || !r.Created.Equal(created) {
		t.Fatalf("identity: %+v", r)
	}
	var facets []string
	for _, f := range r.Facets {
		facets = append(facets, f.ID)
	}
	if want := []string{model.FacetKeys, model.FacetImportJobs, model.FacetPermissions}; !slices.Equal(facets, want) {
		t.Fatalf("facets = %v, want %v", facets, want)
	}
}

func TestListLocationFailures(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	cases := []struct {
		name      string
		locErr    map[string]error
		wantRings []string
		wantErr   bool
	}{
		{name: "all loaded", wantRings: []string{"app-keys", "audit-keys"}},
		{
			name:      "one failed",
			locErr:    map[string]error{"us-east1": boom},
			wantRings: []string{"app-keys"},
		},
		{
			name:      "two failed",
			locErr:    map[string]error{"global": boom, "us-east1": errors.New("other")},
			wantRings: []string{"app-keys"},
		},
		{name: "every location failed", locErr: map[string]error{"global": boom, "us-central1": boom, "us-east1": boom}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := sample()
			api.locErr = tc.locErr
			got, err := New(api).List(context.Background(), "projects/p")
			var names []string
			for _, r := range got {
				names = append(names, r.Name)
			}
			if !slices.Equal(names, tc.wantRings) {
				t.Fatalf("rings = %v, want %v", names, tc.wantRings)
			}
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
			} else if err == nil || model.IsPartial(err) || !errors.Is(err, boom) {
				t.Fatalf("err = %v, want a hard error wrapping boom", err)
			}
		})
	}
}

func TestKeys(t *testing.T) {
	t.Parallel()
	p := New(sample())
	rings, err := p.List(context.Background(), "projects/p")
	if err != nil {
		t.Fatal(err)
	}
	parent := rings[0]
	got, truncated, err := p.ListRelated(context.Background(), parent, model.FacetKeys)
	if err != nil || truncated || len(got) != 3 {
		t.Fatalf("got=%+v truncated=%v err=%v", got, truncated, err)
	}
	k := got[0]
	if k.ID != "projects/p/locations/us-central1/keyRings/app-keys/cryptoKeys/secrets" || k.Name != "secrets" ||
		k.Service != model.ServiceKMSKey || k.ParentID != parent.ID || k.FacetID != model.FacetKeys ||
		k.ScopeID != "projects/p" || k.Location != "us-central1" || k.State != "ENABLED" || k.Badge != "SYM" ||
		!k.Created.Equal(created) || k.Labels["app"] != "web" {
		t.Fatalf("key: %+v", k)
	}
	var keys []string
	for _, f := range k.Fields {
		keys = append(keys, f.Key)
	}
	want := []string{"purpose", "algorithm", "protectionLevel", "primaryVersion", "rotation", "nextRotation", "destroyDelay"}
	if !slices.Equal(keys, want) {
		t.Fatalf("fields = %v, want %v", keys, want)
	}
	if k.Metadata["rotation"] != "90 days" || k.Metadata["destroyDelay"] != "30 days" {
		t.Fatalf("metadata: %+v", k.Metadata)
	}
	if s := got[1]; s.Status != model.StatusHealthy || s.State != "" || s.Badge != "SIGN" || s.Metadata["primaryVersion"] != "" ||
		s.Metadata["versions"] != "1 enabled, 1 destroy scheduled" {
		t.Fatalf("asymmetric key: %+v", s)
	}
	if len(k.Facets) != 2 || k.Facets[0].ID != model.FacetVersions || k.Facets[1].ID != model.FacetPermissions {
		t.Fatalf("key facets: %+v", k.Facets)
	}
	if d := got[2]; d.Status != model.StatusWarning {
		t.Fatalf("disabled key status = %v", d.Status)
	}
}

func TestKeyTemplateAndAccessReasons(t *testing.T) {
	t.Parallel()
	ring := ringResource("p", sample().rings["us-central1"][0])
	got, _, err := New(sample()).ListRelated(context.Background(), ring, model.FacetKeys)
	if err != nil {
		t.Fatal(err)
	}
	s := got[1]
	if s.Metadata["template"] != "EC_SIGN_P384_SHA384 HSM" || s.Metadata["accessReasons"] != "2" ||
		!slices.Equal(s.List("accessReasons"), []string{"CUSTOMER_INITIATED_ACCESS", "GOOGLE_INITIATED_SERVICE"}) {
		t.Fatalf("signing key: %+v", s)
	}
	if _, ok := got[0].Metadata["template"]; ok {
		t.Fatalf("a template matching the primary is not shown: %+v", got[0].Metadata)
	}
}

func TestKeyVersionSummary(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		versions []KeyVersion
		want     string
		status   model.ResourceStatus
	}{
		{"none", nil, "none", model.StatusUnknown},
		{"enabled wins", []KeyVersion{{State: "GENERATION_FAILED"}, {State: "ENABLED"}}, "1 enabled, 1 generation failed", model.StatusHealthy},
		{"worst otherwise", []KeyVersion{{State: "DESTROYED"}, {State: "DISABLED"}, {State: "IMPORT_FAILED"}}, "1 disabled, 1 import failed, 1 destroyed", model.StatusError},
		{"destroyed only", []KeyVersion{{State: "DESTROYED"}, {State: "DESTROYED"}}, "2 destroyed", model.StatusUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := keyResource("global", "projects/p/locations/global/keyRings/r", Key{Name: "k", Purpose: "MAC"})
			withVersions(&r, tc.versions, false)
			if r.Metadata["versions"] != tc.want || r.Status != tc.status || r.Fields[len(r.Fields)-1].Key != "versions" {
				t.Fatalf("versions=%q status=%v fields=%v, want %q %v", r.Metadata["versions"], r.Status, r.Fields, tc.want, tc.status)
			}
		})
	}
}

func TestKeyVersionSummaryTruncated(t *testing.T) {
	t.Parallel()
	r := keyResource("global", "projects/p/locations/global/keyRings/r", Key{Name: "k", Purpose: "MAC"})
	withVersions(&r, []KeyVersion{{State: "DESTROYED"}, {State: "IMPORT_FAILED"}}, true)
	if r.Metadata["versions"] != "1 import failed, 1 destroyed (first 2 versions)" || r.Status != model.StatusUnknown {
		t.Fatalf("truncated summary: %q %v", r.Metadata["versions"], r.Status)
	}
	withVersions(&r, []KeyVersion{{State: "ENABLED"}}, true)
	if r.Status != model.StatusHealthy {
		t.Fatalf("an enabled version among those read is still healthy: %v", r.Status)
	}
}

func TestKeyVersionSummaryCapped(t *testing.T) {
	t.Parallel()
	api := sample()
	calls := 0
	api.calls = &calls
	many := make([]Key, maxSummarized+1)
	for i := range many {
		many[i] = Key{Name: "sign-" + strconv.Itoa(i), Purpose: "ASYMMETRIC_SIGN"}
	}
	api.keys["app-keys"] = many
	ring := ringResource("p", api.rings["us-central1"][0])
	got, _, err := New(api).ListRelated(context.Background(), ring, model.FacetKeys)
	if err != nil || len(got) != len(many) || calls != 1 {
		t.Fatalf("len=%d calls=%d err=%v, want one ListKeys call and no summaries", len(got), calls, err)
	}
	if got[0].Metadata["versions"] != "" {
		t.Fatalf("summarized past the cap: %+v", got[0].Metadata)
	}
}

func TestKeyVersionSummaryContextEnded(t *testing.T) {
	t.Parallel()
	api := sample()
	api.versErr = map[string]error{"signing": context.DeadlineExceeded}
	ctx, cancel := context.WithCancel(context.Background())
	api.onVersions = cancel
	ring := ringResource("p", api.rings["us-central1"][0])
	if got, _, err := New(api).ListRelated(ctx, ring, model.FacetKeys); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatalf("got=%v err=%v, want the ended context's error and no partial list", got, err)
	}
}

func TestKeyVersionSummaryFailureKeepsKey(t *testing.T) {
	t.Parallel()
	api := sample()
	api.versErr = map[string]error{"signing": errors.New("denied")}
	ring := ringResource("p", api.rings["us-central1"][0])
	got, _, err := New(api).ListRelated(context.Background(), ring, model.FacetKeys)
	if err != nil || len(got) != 3 {
		t.Fatalf("got=%d err=%v", len(got), err)
	}
	if s := got[1]; s.Metadata["versions"] != "" || s.Status != model.StatusUnknown {
		t.Fatalf("a key whose versions failed keeps no summary: %+v", s)
	}
}

func TestVersions(t *testing.T) {
	t.Parallel()
	api := sample()
	ring := ringResource("p", api.rings["us-central1"][0])
	keys, _, err := New(api).ListRelated(context.Background(), ring, model.FacetKeys)
	if err != nil {
		t.Fatal(err)
	}
	key := keys[0]
	key.Metadata["primaryVersion"] = "10"
	got, truncated, err := New(api).ListRelated(context.Background(), key, model.FacetVersions)
	if err != nil || truncated {
		t.Fatalf("truncated=%v err=%v", truncated, err)
	}
	var names []string
	for _, v := range got {
		names = append(names, v.Name)
	}
	if want := []string{"10", "9", "4", "3"}; !slices.Equal(names, want) {
		t.Fatalf("order = %v, want newest first %v", names, want)
	}
	v := got[0]
	if v.ID != key.ID+"/cryptoKeyVersions/10" || v.Service != model.ServiceKMSKeyVersion || v.ParentID != key.ID ||
		v.FacetID != model.FacetVersions || v.ScopeID != "projects/p" || v.Location != "us-central1" ||
		v.Status != model.StatusHealthy || v.State != "ENABLED" || v.Badge != "PRIMARY" || v.Metadata["primary"] != "yes" ||
		!v.Created.Equal(created) {
		t.Fatalf("primary version: %+v", v)
	}
	if v.Fields[0].Key != "primary" || v.Fields[1].Key != "algorithm" {
		t.Fatalf("fields = %v", v.Fields)
	}
	if s := got[1]; s.Badge != "DESTROY_SCHEDULED" || s.Status != model.StatusWarning || s.Metadata["destroyScheduled"] != "2024-04-02 09:00 UTC" {
		t.Fatalf("scheduled version: %+v", s)
	}
	if d := got[3]; d.Status != model.StatusUnknown || d.Metadata["destroyed"] == "" {
		t.Fatalf("destroyed version: %+v", d)
	}
}

func TestVersionsTruncate(t *testing.T) {
	t.Parallel()
	api := sample()
	many := make([]KeyVersion, maxVersions+1)
	for i := range many {
		many[i] = KeyVersion{Name: strconv.Itoa(i + 1)}
	}
	api.versions["secrets"] = many
	key := keyResource("us-central1", "projects/p/locations/us-central1/keyRings/app-keys", api.keys["app-keys"][0])
	got, truncated, err := New(api).ListRelated(context.Background(), key, model.FacetVersions)
	if err != nil || !truncated || len(got) != maxVersions {
		t.Fatalf("len=%d truncated=%v err=%v", len(got), truncated, err)
	}
}

func TestImportJobs(t *testing.T) {
	t.Parallel()
	ring := ringResource("p", sample().rings["us-central1"][0])
	got, truncated, err := New(sample()).ListRelated(context.Background(), ring, model.FacetImportJobs)
	if err != nil || truncated || len(got) != 1 {
		t.Fatalf("got=%+v truncated=%v err=%v", got, truncated, err)
	}
	j := got[0]
	if j.ID != ring.ID+"/importJobs/migrate" || j.Service != model.ServiceKMSImportJob || j.ParentID != ring.ID ||
		j.FacetID != model.FacetImportJobs || j.ScopeID != "projects/p" || j.Status != model.StatusHealthy ||
		j.Badge != "ACTIVE" || j.Metadata["method"] != "RSA_OAEP_3072_SHA256" || j.Metadata["expires"] != "2024-04-04 09:00 UTC" {
		t.Fatalf("import job: %+v", j)
	}
	for state, want := range map[string]model.ResourceStatus{"ACTIVE": model.StatusHealthy, "PENDING_GENERATION": model.StatusWarning, "EXPIRED": model.StatusUnknown} {
		if got := importJobStatus(state); got != want {
			t.Errorf("importJobStatus(%q) = %v, want %v", state, got, want)
		}
	}
}

func TestPermissions(t *testing.T) {
	t.Parallel()
	api := sample()
	ring := ringResource("p", api.rings["us-central1"][0])
	key := keyResource("us-central1", ring.ID, api.keys["app-keys"][0])
	key.ScopeID = "projects/p"
	cases := []struct {
		parent    model.Resource
		role      string
		condition string
		member    string
	}{
		{ring, "roles/cloudkms.admin", "", "group:sec@example.com"},
		{key, "roles/cloudkms.cryptoKeyEncrypterDecrypter", "business hours", "serviceAccount:app@p.iam.gserviceaccount.com"},
	}
	for _, tc := range cases {
		t.Run(tc.parent.Name, func(t *testing.T) {
			t.Parallel()
			got, truncated, err := New(api).ListRelated(context.Background(), tc.parent, model.FacetPermissions)
			if err != nil || truncated || len(got) != 1 {
				t.Fatalf("got=%+v truncated=%v err=%v", got, truncated, err)
			}
			b := got[0]
			if b.ID != model.BindingID(tc.parent.ID, tc.role, tc.condition) || b.Service != model.ServiceIAMBinding ||
				b.ParentID != tc.parent.ID || b.FacetID != model.FacetPermissions || b.ScopeID != "projects/p" ||
				!slices.Equal(b.List(model.ListMembers), []string{tc.member}) || !b.IsBinding() {
				t.Fatalf("binding: %+v", b)
			}
		})
	}
}

func TestKeyStatus(t *testing.T) {
	t.Parallel()
	cases := map[string]model.ResourceStatus{
		"":                  model.StatusUnknown,
		"ENABLED":           model.StatusHealthy,
		"DISABLED":          model.StatusWarning,
		"DESTROY_SCHEDULED": model.StatusWarning,
		"GENERATION_FAILED": model.StatusError,
		"DESTROYED":         model.StatusUnknown,
	}
	for s, want := range cases {
		if got := keyStatus(s); got != want {
			t.Errorf("keyStatus(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestKeysTruncate(t *testing.T) {
	t.Parallel()
	api := sample()
	many := make([]Key, maxKeys+1)
	for i := range many {
		many[i] = Key{Name: "k"}
	}
	api.keys["app-keys"] = many
	parent := ringResource("p", api.rings["us-central1"][0])
	got, truncated, err := New(api).ListRelated(context.Background(), parent, model.FacetKeys)
	if err != nil || !truncated || len(got) != maxKeys {
		t.Fatalf("len=%d truncated=%v err=%v", len(got), truncated, err)
	}
}

func TestListRelatedIgnoresOthers(t *testing.T) {
	t.Parallel()
	ring := ringResource("p", sample().rings["us-central1"][0])
	cases := []struct {
		name   string
		parent model.Resource
		facet  string
	}{
		{"other service", model.Resource{ID: ring.ID, Service: model.ServiceSecretManager}, model.FacetKeys},
		{"other facet", ring, model.FacetVersions},
		{"key facet on a ring", ring, model.FacetObjects},
		{"ring facet on a key", model.Resource{ID: ring.ID + "/cryptoKeys/k", Service: model.ServiceKMSKey}, model.FacetImportJobs},
		{"key id on a ring", model.Resource{ID: ring.ID + "/cryptoKeys/k", Service: model.ServiceKMS}, model.FacetKeys},
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

func TestErrorsPropagate(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	p := New(fakeAPI{err: boom, mu: &sync.Mutex{}})
	if _, err := p.List(context.Background(), "projects/p"); !errors.Is(err, boom) {
		t.Fatalf("List err = %v", err)
	}
	ring := ringResource("p", KeyRing{Name: "r", Location: "global"})
	for _, f := range []string{model.FacetKeys, model.FacetImportJobs, model.FacetPermissions} {
		if _, _, err := p.ListRelated(context.Background(), ring, f); !errors.Is(err, boom) {
			t.Fatalf("ListRelated(%s) err = %v", f, err)
		}
	}
	key := keyResource("global", ring.ID, Key{Name: "k"})
	for _, f := range []string{model.FacetVersions, model.FacetPermissions} {
		if _, _, err := p.ListRelated(context.Background(), key, f); !errors.Is(err, boom) {
			t.Fatalf("key ListRelated(%s) err = %v", f, err)
		}
	}
}
