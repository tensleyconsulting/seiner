package privateca

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	pools        []Pool
	authorities  map[string][]Authority         // keyed by location/pool
	certificates map[string][]IssuedCertificate // keyed by location/pool
	crls         map[string][]CRL               // keyed by location/pool/ca
	templates    []Template
	poolIAM      map[string][]Binding // keyed by location/pool
	templateIAM  map[string][]Binding // keyed by location/template
	err          error
	calls        *atomic.Int32
}

func (f fakeAPI) record() error {
	if f.calls != nil {
		f.calls.Add(1)
	}
	return f.err
}

func (f fakeAPI) ListPools(context.Context, string) ([]Pool, error) {
	if err := f.record(); err != nil {
		return nil, err
	}
	return f.pools, nil
}

func (f fakeAPI) ListAuthorities(_ context.Context, _, location, pool string) ([]Authority, error) {
	if err := f.record(); err != nil {
		return nil, err
	}
	return slices.Clone(f.authorities[location+"/"+pool]), nil
}

func (f fakeAPI) ListCertificates(_ context.Context, _, location, pool string, limit int) ([]IssuedCertificate, bool, error) {
	if err := f.record(); err != nil {
		return nil, false, err
	}
	all := f.certificates[location+"/"+pool]
	if len(all) > limit {
		return slices.Clone(all[:limit]), true, nil
	}
	return slices.Clone(all), false, nil
}

func (f fakeAPI) ListCRLs(_ context.Context, _, location, pool, ca string, limit int) ([]CRL, bool, error) {
	if err := f.record(); err != nil {
		return nil, false, err
	}
	all := f.crls[location+"/"+pool+"/"+ca]
	if len(all) > limit {
		return slices.Clone(all[:limit]), true, nil
	}
	return slices.Clone(all), false, nil
}

func (f fakeAPI) ListTemplates(context.Context, string) ([]Template, error) {
	if err := f.record(); err != nil {
		return nil, err
	}
	return f.templates, nil
}

func (f fakeAPI) GetPoolIAMPolicy(_ context.Context, _, location, pool string) ([]Binding, error) {
	if err := f.record(); err != nil {
		return nil, err
	}
	return f.poolIAM[location+"/"+pool], nil
}

func (f fakeAPI) GetTemplateIAMPolicy(_ context.Context, _, location, template string) ([]Binding, error) {
	if err := f.record(); err != nil {
		return nil, err
	}
	return f.templateIAM[location+"/"+template], nil
}

func TestListScopesAndPermissions(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	api := fakeAPI{
		pools:     []Pool{{Name: "p1", Location: "us-central1", Tier: "ENTERPRISE"}},
		templates: []Template{{Name: "t1", Location: "us-central1"}},
		calls:     &calls,
	}
	providers := []struct {
		p     model.ResourceProvider
		perms []string
	}{
		{NewPools(api), []string{"privateca.caPools.list"}},
		{NewTemplates(api), []string{"privateca.certificateTemplates.list"}},
	}
	for _, tc := range providers {
		d := tc.p.(model.PermissionDeclarer)
		if got := d.ListPermissions("projects/p"); !slices.Equal(got, tc.perms) {
			t.Errorf("%s permissions: %v want %v", tc.p.Service(), got, tc.perms)
		}
		for _, scope := range []string{"organizations/1", "folders/2", ""} {
			if got := d.ListPermissions(scope); got != nil {
				t.Errorf("%s permissions on %q: %v", tc.p.Service(), scope, got)
			}
			res, err := tc.p.List(context.Background(), scope)
			if err != nil || res != nil {
				t.Errorf("%s List(%q) = %v, %v", tc.p.Service(), scope, res, err)
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("non-project scopes called API %d times", calls.Load())
	}
}

func TestPoolsMappingAndTierFacets(t *testing.T) {
	t.Parallel()
	labels := map[string]string{"env": "prod"}
	api := fakeAPI{
		pools: []Pool{
			{
				Name:                  "internal-pool",
				Location:              "us-central1",
				Tier:                  "ENTERPRISE",
				PublishCACert:         true,
				PublishCRL:            true,
				EncodingFormat:        "PEM",
				AllowedKeyTypes:       []string{"ECDSA_P256", "RSA 2048–4096"},
				MaxLifetime:           "31536000s",
				BackdateDuration:      "300s",
				IssuanceModes:         "CSR, config",
				IdentityPassthrough:   "subject, SANs",
				IdentityCEL:           "subject_alt_names.all(san, san.value.endsWith('.internal'))",
				PassthroughExtensions: []string{"BASE_KEY_USAGE", "EXTENDED_KEY_USAGE"},
				KMSKey:                "projects/p/locations/us-central1/keyRings/cas/cryptoKeys/pool",
				Labels:                labels,
			},
			{
				Name:               "workload-pool",
				Location:           "us-east1",
				Tier:               "DEVOPS",
				PublishCACert:      true,
				RequesterNotBefore: true,
			},
		},
	}
	got, err := NewPools(api).List(context.Background(), "projects/p")
	if err != nil || len(got) != 2 {
		t.Fatalf("got %d pools, err %v", len(got), err)
	}

	ent := got[0]
	labels["env"] = "mutated"
	if ent.ID != "projects/p/locations/us-central1/caPools/internal-pool" ||
		ent.Name != "internal-pool" || ent.Service != model.ServiceCAPool ||
		ent.ScopeID != "projects/p" || ent.Location != "us-central1" ||
		ent.Badge != "ENTERPRISE" || ent.Labels["env"] != "prod" {
		t.Fatalf("enterprise pool: %+v", ent)
	}
	if ent.Metadata["maxLifetime"] != "365 days" || ent.Metadata["backdate"] != "5m0s" || ent.Metadata["publishCrl"] != "on" {
		t.Fatalf("enterprise metadata: %+v", ent.Metadata)
	}
	var wantKeys []string
	for _, f := range ent.Fields {
		wantKeys = append(wantKeys, f.Key)
	}
	if !slices.Equal(wantKeys, poolFields) {
		t.Errorf("fields order = %v, want %v", wantKeys, poolFields)
	}
	entFacets := []string{ent.Facets[0].ID, ent.Facets[1].ID, ent.Facets[2].ID}
	if !slices.Equal(entFacets, []string{model.FacetCertificateAuthorities, model.FacetCertificates, model.FacetPermissions}) {
		t.Errorf("enterprise facets = %v", entFacets)
	}

	dev := got[1]
	if _, hasCRL := dev.Metadata["publishCrl"]; hasCRL {
		t.Errorf("devops pool should not set publishCrl: %+v", dev.Metadata)
	}
	if dev.Metadata["backdate"] != "requester-specified" {
		t.Errorf("devops backdate = %q", dev.Metadata["backdate"])
	}
	devFacets := []string{dev.Facets[0].ID, dev.Facets[1].ID}
	if len(dev.Facets) != 2 || !slices.Equal(devFacets, []string{model.FacetCertificateAuthorities, model.FacetPermissions}) {
		t.Errorf("devops facets = %+v", dev.Facets)
	}
}

func TestAuthorityMappingStatusAndCopies(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	created := time.Date(2024, 1, 10, 8, 0, 0, 0, time.UTC)
	api := fakeAPI{
		authorities: map[string][]Authority{
			"us-central1/internal-pool": {
				{
					Name:         "sub-ca-staged",
					Type:         "SUBORDINATE",
					State:        "STAGED",
					Tier:         "ENTERPRISE",
					Subject:      "CN=Example Issuing CA, O=Example",
					IssuerCA:     "projects/p/locations/us-central1/caPools/root-pool/certificateAuthorities/root-ca-1",
					NotBefore:    created,
					NotAfter:     now.Add(365 * 24 * time.Hour),
					Lifetime:     "94608000s",
					KeyAlgorithm: "EC_P256_SHA256",
					Serial:       "0a1b",
					SHA256:       "deadbeef",
					GCSBucket:    "example-ca-bucket",
					CACertURL:    "http://privateca-content.storage.googleapis.com/ca.crt",
					CRLURLs:      []string{"http://privateca-content.storage.googleapis.com/crl.crl"},
					PEMCerts:     []string{"-----BEGIN CERTIFICATE-----\nLEAF\n-----END CERTIFICATE-----", "-----BEGIN CERTIFICATE-----\nROOT\n-----END CERTIFICATE-----\n"},
					Created:      created,
					Labels:       map[string]string{"role": "issuing"},
				},
				{
					Name:      "root-ca-1",
					Type:      "SELF_SIGNED",
					State:     "ENABLED",
					Tier:      "ENTERPRISE",
					Subject:   "CN=Example Root CA, O=Example",
					SANs:      []string{"ca.example.internal"},
					NotBefore: created,
					NotAfter:  now.Add(10 * 365 * 24 * time.Hour),
					PEMCerts:  []string{"-----BEGIN CERTIFICATE-----\nROOT\n-----END CERTIFICATE-----"},
					Created:   created,
				},
				{
					Name:    "pending-sub",
					Type:    "SUBORDINATE",
					State:   "AWAITING_USER_ACTIVATION",
					Tier:    "ENTERPRISE",
					Created: created,
				},
			},
		},
	}
	p := NewPools(api)
	p.now = func() time.Time { return now }
	parent := model.Resource{
		ID:       "projects/p/locations/us-central1/caPools/internal-pool",
		Service:  model.ServiceCAPool,
		ScopeID:  "projects/p",
		Location: "us-central1",
		Metadata: map[string]string{"tier": "ENTERPRISE", "publishCrl": "on"},
	}

	cas, truncated, err := p.ListRelated(context.Background(), parent, model.FacetCertificateAuthorities)
	if err != nil || truncated || len(cas) != 3 {
		t.Fatalf("cas = %d, truncated=%v, err=%v", len(cas), truncated, err)
	}
	// ENABLED sorts first, then STAGED, then AWAITING_USER_ACTIVATION.
	if cas[0].Name != "root-ca-1" || cas[1].Name != "sub-ca-staged" || cas[2].Name != "pending-sub" {
		t.Fatalf("order = %s, %s, %s", cas[0].Name, cas[1].Name, cas[2].Name)
	}

	root := cas[0]
	if root.Status != model.StatusHealthy || root.Badge != "ROOT" || root.ParentID != parent.ID || root.FacetID != model.FacetCertificateAuthorities {
		t.Errorf("root = %+v", root)
	}
	if f, ok := root.InlineFacet(); !ok || f.ID != model.FacetCRLs {
		t.Errorf("root CRL inline facet = %+v, %v", f, ok)
	}
	if len(root.Copies) != 1 || root.Copies[0].Title != "CA certificate (PEM)" {
		t.Errorf("root copies = %+v", root.Copies)
	}
	if !slices.Equal(root.List("sans"), []string{"ca.example.internal"}) {
		t.Errorf("root sans = %v", root.List("sans"))
	}

	sub := cas[1]
	if sub.Status != model.StatusWarning || sub.Badge != "STAGED" || sub.Metadata["issuer"] != "root-pool/root-ca-1" {
		t.Errorf("sub = %+v", sub)
	}
	if len(sub.Copies) != 2 || sub.Copies[1].Title != "CA chain (PEM)" {
		t.Errorf("sub copies = %+v", sub.Copies)
	}

	pending := cas[2]
	if len(pending.Facets) != 0 {
		t.Errorf("pending CA should have no CRL facet: %+v", pending.Facets)
	}
}

func TestAuthorityStatusMatrix(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	far := now.Add(180 * 24 * time.Hour)
	cases := []struct {
		name       string
		ca         Authority
		wantStatus model.ResourceStatus
		wantBadge  string
	}{
		{"enabled root", Authority{State: "ENABLED", Type: "SELF_SIGNED", NotAfter: far}, model.StatusHealthy, "ROOT"},
		{"enabled sub", Authority{State: "ENABLED", Type: "SUBORDINATE", NotAfter: far}, model.StatusHealthy, "SUB"},
		{"staged", Authority{State: "STAGED", Type: "SUBORDINATE", NotAfter: far}, model.StatusWarning, "STAGED"},
		{"disabled", Authority{State: "DISABLED", Type: "SELF_SIGNED", NotAfter: far}, model.StatusWarning, "DISABLED"},
		{"awaiting activation", Authority{State: "AWAITING_USER_ACTIVATION", Type: "SUBORDINATE"}, model.StatusWarning, "AWAITING_USER_ACTIVATION"},
		{"deleted", Authority{State: "DELETED", Type: "SELF_SIGNED", NotAfter: now.Add(-time.Hour)}, model.StatusWarning, "DELETED"},
		{"expiring soon", Authority{State: "ENABLED", Type: "SELF_SIGNED", NotAfter: now.Add(10 * 24 * time.Hour)}, model.StatusWarning, "10d"},
		{"expired", Authority{State: "ENABLED", Type: "SELF_SIGNED", NotAfter: now.Add(-time.Minute)}, model.StatusError, "EXPIRED"},
		{"unknown", Authority{State: "MYSTERY"}, model.StatusUnknown, "MYSTERY"},
		{"empty", Authority{}, model.StatusUnknown, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, badge := authorityStatus(tc.ca, now)
			if st != tc.wantStatus || badge != tc.wantBadge {
				t.Errorf("got (%s, %q), want (%s, %q)", st, badge, tc.wantStatus, tc.wantBadge)
			}
		})
	}
}

func TestCertificatesTruncationSortingAndRevocation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	certs := make([]IssuedCertificate, maxCertificates+5)
	for i := range certs {
		certs[i] = IssuedCertificate{
			Name:     "cert-" + strconv.Itoa(i),
			IssuerCA: "projects/p/locations/us-central1/caPools/pool/certificateAuthorities/root-ca",
			Template: "projects/p/locations/us-central1/certificateTemplates/mtls-leaf",
			NotAfter: now.Add(90 * 24 * time.Hour),
			PEMCert:  "-----BEGIN CERTIFICATE-----\nLEAF\n-----END CERTIFICATE-----",
			PEMChain: []string{"-----BEGIN CERTIFICATE-----\nISSUER\n-----END CERTIFICATE-----"},
			Created:  now.Add(time.Duration(-i) * time.Minute),
			SANs:     []string{"svc.internal"},
		}
	}
	certs[1].Revoked = true
	certs[1].RevocationReason = "KEY_COMPROMISE"
	certs[2].NotAfter = now.Add(5 * 24 * time.Hour)
	certs[3].NotAfter = now.Add(-time.Hour)

	p := NewPools(fakeAPI{certificates: map[string][]IssuedCertificate{"us-central1/pool": certs}})
	p.now = func() time.Time { return now }
	pool := model.Resource{
		ID:      "projects/p/locations/us-central1/caPools/pool",
		Service: model.ServiceCAPool,
		ScopeID: "projects/p",
	}

	got, truncated, err := p.ListRelated(context.Background(), pool, model.FacetCertificates)
	if err != nil || !truncated || len(got) != maxCertificates {
		t.Fatalf("certs = %d, truncated=%v, err=%v", len(got), truncated, err)
	}
	if got[0].Status != model.StatusHealthy || got[0].Metadata["template"] != "mtls-leaf" || got[0].Metadata["issuerCa"] != "root-ca" || len(got[0].Copies) != 2 {
		t.Errorf("active cert: %+v", got[0])
	}
	if got[1].Status != model.StatusError || got[1].State != "REVOKED" || got[1].Badge != "REVOKED" || got[1].Metadata["revocationReason"] != "KEY_COMPROMISE" {
		t.Errorf("revoked cert: %+v", got[1])
	}
	if got[2].Status != model.StatusWarning || got[2].Badge != "5d" {
		t.Errorf("expiring cert: %+v", got[2])
	}
	if got[3].Status != model.StatusError || got[3].Badge != "EXPIRED" {
		t.Errorf("expired cert: %+v", got[3])
	}

	// DEVOPS pool skips certificate listing without calling the API.
	var calls atomic.Int32
	devopsPool := pool
	devopsPool.Metadata = map[string]string{"tier": "DEVOPS"}
	res, trunc, err := NewPools(fakeAPI{calls: &calls}).ListRelated(context.Background(), devopsPool, model.FacetCertificates)
	if err != nil || trunc || res != nil || calls.Load() != 0 {
		t.Errorf("devops cert list = %v, %v, %v (calls=%d)", res, trunc, err, calls.Load())
	}
}

func TestCRLsSortingAndRevokedList(t *testing.T) {
	t.Parallel()
	api := fakeAPI{
		crls: map[string][]CRL{
			"us-central1/pool/root-ca": {
				{Name: "00000001", State: "SUPERSEDED", SequenceNumber: 1, RevisionID: "a1b2c3d4"},
				{
					Name:           "00000002",
					State:          "ACTIVE",
					SequenceNumber: 2,
					RevisionID:     "e5f60718",
					AccessURL:      "http://privateca-content.storage.googleapis.com/crl",
					PEMCRL:         "-----BEGIN X509 CRL-----\nCRL\n-----END X509 CRL-----",
					Revoked: []RevokedCert{
						{Certificate: "projects/p/locations/us-central1/caPools/pool/certificates/web-1", Serial: "1a2b", Reason: "KEY_COMPROMISE"},
					},
				},
			},
		},
	}
	ca := model.Resource{
		ID:       "projects/p/locations/us-central1/caPools/pool/certificateAuthorities/root-ca",
		Service:  model.ServiceCertificateAuthority,
		ScopeID:  "projects/p",
		Location: "us-central1",
		Metadata: map[string]string{"tier": "ENTERPRISE"},
	}
	got, truncated, err := NewPools(api).ListRelated(context.Background(), ca, model.FacetCRLs)
	if err != nil || truncated || len(got) != 2 {
		t.Fatalf("crls = %d, truncated=%v, err=%v", len(got), truncated, err)
	}
	if got[0].Name != "00000002" || got[0].Status != model.StatusHealthy || got[0].Badge != "ACTIVE" || len(got[0].Copies) != 1 {
		t.Errorf("active crl: %+v", got[0])
	}
	if !slices.Equal(got[0].List("revoked"), []string{"1a2b  KEY_COMPROMISE  (web-1)"}) {
		t.Errorf("revoked list = %v", got[0].List("revoked"))
	}
	if got[1].Name != "00000001" || got[1].Status != model.StatusUnknown || got[1].Badge != "SUPERSEDED" {
		t.Errorf("superseded crl: %+v", got[1])
	}
}

func TestTemplatesAndIAMBindings(t *testing.T) {
	t.Parallel()
	api := fakeAPI{
		templates: []Template{{
			Name:                "mtls-leaf",
			Location:            "us-central1",
			Description:         "Workload mTLS leaf template",
			MaxLifetime:         "86400s",
			IsCA:                true,
			MaxIssuerPathLength: 1,
			KeyUsages:           []string{"digitalSignature", "keyEncipherment"},
			ExtendedKeyUsages:   []string{"serverAuth", "clientAuth"},
			IdentityPassthrough: "SANs",
			Created:             time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
		}},
		poolIAM: map[string][]Binding{
			"us-central1/pool": {{Role: "roles/privateca.certificateManager", Members: []string{"serviceAccount:workload@p.iam.gserviceaccount.com"}}},
		},
		templateIAM: map[string][]Binding{
			"us-central1/mtls-leaf": {{Role: "roles/privateca.templateUser", Members: []string{"group:platform@example.com"}, Condition: "Prod only"}},
		},
	}

	tmpls, err := NewTemplates(api).List(context.Background(), "projects/p")
	if err != nil || len(tmpls) != 1 {
		t.Fatalf("templates = %d, err=%v", len(tmpls), err)
	}
	tmpl := tmpls[0]
	if tmpl.ID != "projects/p/locations/us-central1/certificateTemplates/mtls-leaf" ||
		tmpl.Metadata["maxLifetime"] != "1 day" || tmpl.Metadata["isCa"] != "yes (max path 1)" ||
		len(tmpl.Facets) != 1 || tmpl.Facets[0].ID != model.FacetPermissions {
		t.Fatalf("template = %+v", tmpl)
	}

	tPerms, truncated, err := NewTemplates(api).ListRelated(context.Background(), tmpl, model.FacetPermissions)
	if err != nil || truncated || len(tPerms) != 1 || !tPerms[0].IsBinding() || tPerms[0].ParentID != tmpl.ID {
		t.Fatalf("template perms = %+v, err=%v", tPerms, err)
	}

	pool := model.Resource{ID: "projects/p/locations/us-central1/caPools/pool", Service: model.ServiceCAPool, ScopeID: "projects/p"}
	pPerms, truncated, err := NewPools(api).ListRelated(context.Background(), pool, model.FacetPermissions)
	if err != nil || truncated || len(pPerms) != 1 || !pPerms[0].IsBinding() || pPerms[0].ParentID != pool.ID {
		t.Fatalf("pool perms = %+v, err=%v", pPerms, err)
	}
}

func TestListRelatedIgnoresUnrelatedAndMalformed(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	pools := NewPools(fakeAPI{calls: &calls})
	tmpls := NewTemplates(fakeAPI{calls: &calls})

	cases := []struct {
		r      model.RelatedLister
		parent model.Resource
		facet  string
	}{
		{pools, model.Resource{ID: "projects/p/buckets/b", Service: model.ServiceCloudStorage}, model.FacetPermissions},
		{pools, model.Resource{ID: "projects/p/locations/us-central1/caPools/pool", Service: model.ServiceCAPool}, model.FacetObjects},
		{pools, model.Resource{ID: "projects/p/locations/us-central1/caPools/pool/iam/bindings/r", Service: model.ServiceCAPool}, model.FacetPermissions},
		{pools, model.Resource{ID: "projects/p/locations/us-central1/caPools/pool", Service: model.ServiceCertificateAuthority}, model.FacetCRLs},
		{pools, model.Resource{ID: "not-a-resource", Service: model.ServiceCAPool}, model.FacetPermissions},
		{tmpls, model.Resource{ID: "projects/p/locations/us-central1/caPools/pool", Service: model.ServiceCAPool}, model.FacetPermissions},
		{tmpls, model.Resource{ID: "projects/p/locations/us-central1/certificateTemplates/t", Service: model.ServiceCertificateTemplate}, model.FacetKeys},
		{tmpls, model.Resource{ID: "projects/p/locations/us-central1/certificateTemplates/t/iam/bindings/r", Service: model.ServiceCertificateTemplate}, model.FacetPermissions},
	}
	for _, tc := range cases {
		res, trunc, err := tc.r.ListRelated(context.Background(), tc.parent, tc.facet)
		if err != nil || trunc || res != nil {
			t.Errorf("(%s, %q, %q) = %v, %v, %v", tc.parent.Service, tc.parent.ID, tc.facet, res, trunc, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("API called %d times on ignored inputs", calls.Load())
	}
}

func TestErrorPropagation(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	api := fakeAPI{err: boom}
	pools := NewPools(api)
	tmpls := NewTemplates(api)
	ctx := context.Background()

	if _, err := pools.List(ctx, "projects/p"); !errors.Is(err, boom) {
		t.Errorf("Pools.List err = %v", err)
	}
	if _, err := tmpls.List(ctx, "projects/p"); !errors.Is(err, boom) {
		t.Errorf("Templates.List err = %v", err)
	}
	pool := model.Resource{ID: "projects/p/locations/us-central1/caPools/pool", Service: model.ServiceCAPool, ScopeID: "projects/p"}
	for _, facet := range []string{model.FacetCertificateAuthorities, model.FacetCertificates, model.FacetPermissions} {
		if _, _, err := pools.ListRelated(ctx, pool, facet); !errors.Is(err, boom) {
			t.Errorf("Pools.ListRelated(%s) err = %v", facet, err)
		}
	}
	ca := model.Resource{ID: pool.ID + "/certificateAuthorities/ca", Service: model.ServiceCertificateAuthority, ScopeID: "projects/p"}
	if _, _, err := pools.ListRelated(ctx, ca, model.FacetCRLs); !errors.Is(err, boom) {
		t.Errorf("Pools.ListRelated(crls) err = %v", err)
	}
	tmpl := model.Resource{ID: "projects/p/locations/us-central1/certificateTemplates/t", Service: model.ServiceCertificateTemplate, ScopeID: "projects/p"}
	if _, _, err := tmpls.ListRelated(ctx, tmpl, model.FacetPermissions); !errors.Is(err, boom) {
		t.Errorf("Templates.ListRelated err = %v", err)
	}
}
