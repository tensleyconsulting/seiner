package artifactregistry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cloud.google.com/go/artifactregistry/apiv1/artifactregistrypb"
	ca "google.golang.org/api/containeranalysis/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
)

// The live mappers are pure; these tests need no client or credentials.

func TestFromVersion(t *testing.T) {
	t.Parallel()
	md, err := structpb.NewStruct(map[string]any{
		"imageSizeBytes": "50540134",
		"mediaType":      "application/vnd.oci.image.manifest.v1+json",
		"artifactType":   "application/vnd.example+type",
		"buildTime":      "2025-10-01T12:31:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	pkg := "projects/p/locations/us/repositories/r/packages/web%2Ffrontend"
	got := fromVersion(&artifactregistrypb.Version{
		Name:        pkg + "/versions/sha256:abc",
		RelatedTags: []*artifactregistrypb.Tag{{Name: pkg + "/tags/v1.4.2"}, {Name: pkg + "/tags/latest"}},
		Metadata:    md,
	})
	if got.Name != "sha256:abc" || !slices.Equal(got.Tags, []string{"v1.4.2", "latest"}) || got.SizeBytes != 50540134 ||
		got.MediaType != "application/vnd.oci.image.manifest.v1+json" ||
		got.ArtifactType != "application/vnd.example+type" ||
		!got.BuildTime.Equal(time.Date(2025, 10, 1, 12, 31, 0, 0, time.UTC)) {
		t.Errorf("fromVersion = %+v", got)
	}
	if plain := fromVersion(&artifactregistrypb.Version{Name: pkg + "/versions/1.0"}); plain.SizeBytes != 0 || plain.MediaType != "" {
		t.Errorf("no metadata = %+v", plain)
	}
}

func TestFromRepository(t *testing.T) {
	t.Parallel()
	got := fromRepository(&artifactregistrypb.Repository{
		Name:   "projects/p/locations/us-central1/repositories/app-images",
		Format: artifactregistrypb.Repository_DOCKER,
		Mode:   artifactregistrypb.Repository_STANDARD_REPOSITORY,
	})
	if got.Name != "app-images" || got.Location != "us-central1" || got.Format != "DOCKER" || got.Mode != "STANDARD_REPOSITORY" {
		t.Errorf("fromRepository = %+v", got)
	}
	if unset := fromRepository(&artifactregistrypb.Repository{Name: "projects/p/locations/us/repositories/x"}); unset.Format != "" {
		t.Errorf("unspecified format = %q", unset.Format)
	}
}

func TestFromPackageUnescapes(t *testing.T) {
	t.Parallel()
	got := fromPackage(&artifactregistrypb.Package{Name: "projects/p/locations/us/repositories/r/packages/web%2Ffrontend"})
	if got.Name != "web/frontend" {
		t.Errorf("name = %q", got.Name)
	}
}

func TestFromOccurrence(t *testing.T) {
	t.Parallel()
	got := fromOccurrence(&ca.Occurrence{
		Name:     "projects/p/occurrences/o1",
		NoteName: "projects/goog-vulnz/notes/CVE-2023-4911",
		Vulnerability: &ca.VulnerabilityOccurrence{
			Severity: "HIGH", CvssScore: 7.8, FixAvailable: true,
			PackageIssue: []*ca.PackageIssue{{
				AffectedPackage: "glibc", PackageType: "OS",
				AffectedVersion: &ca.Version{Kind: "NORMAL", Name: "2.36", Revision: "9+deb12u1"},
				FixedVersion:    &ca.Version{Kind: "NORMAL", FullName: "2.36-9+deb12u3"},
			}, {
				AffectedPackage: "libc-bin", PackageType: "OS",
				AffectedVersion: &ca.Version{Kind: "NORMAL", Name: "2.36", Revision: "9+deb12u1", Epoch: 1},
				FixedVersion:    &ca.Version{Kind: "MAXIMUM"},
				FileLocation:    []*ca.GrafeasV1FileLocation{{FilePath: "/usr/bin/ldd"}},
			}},
			RelatedUrls: []*ca.RelatedUrl{{Url: "https://nvd.nist.gov/vuln/detail/CVE-2023-4911"}, {Label: "empty"}},
		},
	})
	if got.CVE != "CVE-2023-4911" || got.Effective != "HIGH" || len(got.URLs) != 1 {
		t.Errorf("fromOccurrence = %+v", got)
	}
	want := []AffectedPackage{
		{Name: "glibc", Type: "OS", Installed: "2.36-9+deb12u1", Fixed: "2.36-9+deb12u3"},
		{Name: "libc-bin", Type: "OS", Installed: "1:2.36-9+deb12u1", Path: "/usr/bin/ldd"},
	}
	if !slices.Equal(got.Packages, want) {
		t.Errorf("packages = %+v\nwant %+v", got.Packages, want)
	}
}

func TestPageSize(t *testing.T) {
	t.Parallel()
	if pageSize(200) != 201 || pageSize(5000) != 1000 {
		t.Errorf("pageSize = %d, %d", pageSize(200), pageSize(5000))
	}
}

func TestFromRepositoryPoliciesAndUpstreams(t *testing.T) {
	t.Parallel()
	keep := int32(5)
	remote := fromRepository(&artifactregistrypb.Repository{
		Name: "projects/p/locations/us/repositories/hub",
		ModeConfig: &artifactregistrypb.Repository_RemoteRepositoryConfig{RemoteRepositoryConfig: &artifactregistrypb.RemoteRepositoryConfig{
			RemoteSource: &artifactregistrypb.RemoteRepositoryConfig_DockerRepository_{DockerRepository: &artifactregistrypb.RemoteRepositoryConfig_DockerRepository{
				Upstream: &artifactregistrypb.RemoteRepositoryConfig_DockerRepository_PublicRepository_{
					PublicRepository: artifactregistrypb.RemoteRepositoryConfig_DockerRepository_DOCKER_HUB,
				},
			}},
			UpstreamCredentials: &artifactregistrypb.RemoteRepositoryConfig_UpstreamCredentials{
				Credentials: &artifactregistrypb.RemoteRepositoryConfig_UpstreamCredentials_UsernamePasswordCredentials_{
					UsernamePasswordCredentials: &artifactregistrypb.RemoteRepositoryConfig_UpstreamCredentials_UsernamePasswordCredentials{
						Username: "ci", PasswordSecretVersion: "projects/p/secrets/hub/versions/2",
					},
				},
			},
			DisableUpstreamValidation: true,
		}},
		CleanupPolicies: map[string]*artifactregistrypb.CleanupPolicy{
			"keep-recent": {Id: "keep-recent", Action: artifactregistrypb.CleanupPolicy_KEEP,
				ConditionType: &artifactregistrypb.CleanupPolicy_MostRecentVersions{MostRecentVersions: &artifactregistrypb.CleanupPolicyMostRecentVersions{KeepCount: &keep}}},
			"drop-untagged": {Id: "drop-untagged", Action: artifactregistrypb.CleanupPolicy_DELETE,
				ConditionType: &artifactregistrypb.CleanupPolicy_Condition{Condition: &artifactregistrypb.CleanupPolicyCondition{
					TagState: artifactregistrypb.CleanupPolicyCondition_UNTAGGED.Enum(), OlderThan: durationpb.New(30 * 24 * time.Hour),
					PackageNamePrefixes: []string{"web/"},
				}}},
		},
	})
	if remote.RemoteSource != "DOCKER_HUB" || remote.UpstreamUser != "ci" || remote.UpstreamSecret != "projects/p/secrets/hub/versions/2" || !remote.NoUpstreamValidate {
		t.Fatalf("remote = %+v", remote)
	}
	if want := []string{"drop-untagged  DELETE  untagged, older than 30d, packages web/", "keep-recent  KEEP  5 most recent"}; !slices.Equal(remote.CleanupRules, want) {
		t.Fatalf("cleanup = %q, want %q", remote.CleanupRules, want)
	}
	virtual := fromRepository(&artifactregistrypb.Repository{
		Name: "projects/p/locations/us/repositories/all",
		ModeConfig: &artifactregistrypb.Repository_VirtualRepositoryConfig{VirtualRepositoryConfig: &artifactregistrypb.VirtualRepositoryConfig{
			UpstreamPolicies: []*artifactregistrypb.UpstreamPolicy{
				{Id: "b", Repository: "projects/p/locations/us/repositories/hub", Priority: 20},
				{Id: "a", Repository: "projects/p/locations/us/repositories/internal", Priority: 10},
			},
		}},
		FormatConfig: &artifactregistrypb.Repository_MavenConfig{MavenConfig: &artifactregistrypb.Repository_MavenRepositoryConfig{
			VersionPolicy: artifactregistrypb.Repository_MavenRepositoryConfig_RELEASE,
		}},
	})
	if want := []string{"priority 10  projects/p/locations/us/repositories/internal", "priority 20  projects/p/locations/us/repositories/hub"}; !slices.Equal(virtual.UpstreamRules, want) ||
		virtual.MavenPolicy != "RELEASE" {
		t.Fatalf("virtual = %+v", virtual)
	}
}

func TestLiveOCISignaturesReferrersAndSigTag(t *testing.T) {
	t.Parallel()
	const imgDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	const (
		sigManifestV3  = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		sigManifestV2  = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		slsaManifest   = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		sigTagManifest = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
		bundleV3Digest = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
		bundleV2Digest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
		slsaBlobDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	)

	certPEM := testCertPEM(t, []string{"release@example.com"}, nil, oidFulcioIssuerV2, "https://accounts.google.com", true)
	certDER := base64.StdEncoding.EncodeToString([]byte("fake-der-bytes"))
	bundleV3 := map[string]any{
		"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json",
		"verificationMaterial": map[string]any{
			"certificate": map[string]any{"rawBytes": certDER},
			"tlogEntries": []map[string]any{{
				"logIndex":       "123456",
				"integratedTime": "1759322460",
				"logId":          map[string]any{"keyId": base64.StdEncoding.EncodeToString(make([]byte, 32))},
			}},
		},
		"messageSignature": map[string]any{"signature": "djNzaWc="},
	}
	bundleV3Bytes, _ := json.Marshal(bundleV3)

	inTotoSign := base64.StdEncoding.EncodeToString([]byte(`{"predicateType":"https://sigstore.dev/cosign/sign/v1"}`))
	bundleV2 := map[string]any{
		"mediaType": "application/vnd.dev.sigstore.bundle.v0.2+json",
		"verificationMaterial": map[string]any{
			"x509CertificateChain": map[string]any{
				"certificates": []map[string]any{
					{"rawBytes": certDER},
					{"rawBytes": base64.StdEncoding.EncodeToString([]byte("chain-der"))},
				},
			},
			"tlogEntries": []map[string]any{{
				"logIndex":       0,
				"integratedTime": 1759322500,
			}},
		},
		"dsseEnvelope": map[string]any{
			"payloadType": "application/vnd.in-toto+json",
			"payload":     inTotoSign,
			"signatures":  []map[string]any{{"keyid": "kms-key-id", "sig": "ZHNzZXNpZw=="}},
		},
	}
	bundleV2Bytes, _ := json.Marshal(bundleV2)

	var slsaBlobFetched atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/p/app-images/web/frontend/referrers/" + imgDigest:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"manifests": []map[string]any{
					{
						"mediaType":    "application/vnd.oci.image.manifest.v1+json",
						"artifactType": "application/vnd.dev.sigstore.bundle.v0.3+json",
						"digest":       sigManifestV3,
						"annotations":  map[string]string{"dev.sigstore.bundle.predicateType": "https://sigstore.dev/cosign/sign/v1"},
					},
					{
						"mediaType":    "application/vnd.oci.image.manifest.v1+json",
						"artifactType": "application/vnd.dev.sigstore.bundle.v0.2+json",
						"digest":       sigManifestV2,
					},
					{
						"mediaType":    "application/vnd.oci.image.manifest.v1+json",
						"artifactType": "application/vnd.dev.sigstore.bundle.v0.3+json",
						"digest":       slsaManifest,
						"annotations":  map[string]string{"dev.sigstore.bundle.predicateType": "https://slsa.dev/provenance/v1"},
					},
				},
			})
		case "/v2/p/app-images/web/frontend/manifests/" + sigManifestV3:
			w.Header().Set("Docker-Content-Digest", sigManifestV3)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"layers": []map[string]any{{
					"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json",
					"digest":    bundleV3Digest,
					"size":      len(bundleV3Bytes),
				}},
			})
		case "/v2/p/app-images/web/frontend/blobs/" + bundleV3Digest:
			_, _ = w.Write(bundleV3Bytes)
		case "/v2/p/app-images/web/frontend/manifests/" + sigManifestV2:
			w.Header().Set("Docker-Content-Digest", sigManifestV2)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"layers": []map[string]any{{
					"mediaType": "application/vnd.dev.sigstore.bundle.v0.2+json",
					"digest":    bundleV2Digest,
					"size":      len(bundleV2Bytes),
				}},
			})
		case "/v2/p/app-images/web/frontend/blobs/" + bundleV2Digest:
			_, _ = w.Write(bundleV2Bytes)
		case "/v2/p/app-images/web/frontend/blobs/" + slsaBlobDigest:
			slsaBlobFetched.Add(1)
			http.NotFound(w, r)
		case "/v2/p/app-images/web/frontend/manifests/" + strings.ReplaceAll(imgDigest, ":", "-") + ".sig":
			w.Header().Set("Docker-Content-Digest", sigTagManifest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"layers": []map[string]any{
					{
						"mediaType": "application/vnd.dev.cosign.simplesigning.v1+json",
						"digest":    "sha256:layer1",
						"annotations": map[string]string{
							"dev.cosignproject.cosign/signature": "bGF5ZXIx",
							"dev.sigstore.cosign/certificate":    certPEM,
							"dev.sigstore.cosign/chain":          certPEM + "\n" + certPEM,
							"dev.sigstore.cosign/bundle":         `{"Payload":{"integratedTime":1759322600,"logIndex":99,"logID":"abc"}}`,
						},
					},
					{
						"mediaType": "application/vnd.dev.cosign.simplesigning.v1+json",
						"digest":    "sha256:layer2",
						"annotations": map[string]string{
							"dev.cosignproject.cosign/signature": "bGF5ZXIy",
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oci := liveOCI{
		http:    srv.Client(),
		hostURL: func(string) string { return srv.URL },
	}
	got, trunc, err := oci.ListSignatures(context.Background(), "p", "us-central1", "app-images", "web/frontend", imgDigest, 100)
	if err != nil || trunc {
		t.Fatalf("ListSignatures: trunc=%v err=%v", trunc, err)
	}
	if slsaBlobFetched.Load() != 0 {
		t.Errorf("SLSA provenance blob should not be fetched, got %d", slsaBlobFetched.Load())
	}
	if len(got) != 4 {
		t.Fatalf("got %d signatures, want 4: %+v", len(got), got)
	}
	if got[0].Source != "referrer" || got[0].Signature != "djNzaWc=" || !got[0].HasRekor || got[0].RekorLogIndex != 123456 || len(got[0].RekorLogID) != 64 {
		t.Errorf("v3 bundle sig = %+v", got[0])
	}
	if got[1].Signature != "ZHNzZXNpZw==" || got[1].KeyHint != "kms-key-id" || !got[1].HasRekor || got[1].RekorLogIndex != 0 || len(got[1].ChainPEM) != 1 {
		t.Errorf("v2 dsse sig = %+v", got[1])
	}
	if got[2].Source != "tag" || got[2].ID != sigTagManifest+"-1" || len(got[2].ChainPEM) != 2 || got[2].RekorLogIndex != 99 {
		t.Errorf("tag layer 1 = %+v", got[2])
	}
	if got[3].Source != "tag" || got[3].ID != sigTagManifest+"-2" || got[3].Signature != "bGF5ZXIy" {
		t.Errorf("tag layer 2 = %+v", got[3])
	}
}

func TestLiveOCIFallbackDedupDomainAndErrors(t *testing.T) {
	t.Parallel()
	const imgDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	const dupManifest = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/example.com/p/repo/app/manifests/" + strings.ReplaceAll(imgDigest, ":", "-"):
			// Fallback referrer tag returns 1 referrer matching the .sig manifest digest.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"manifests": []map[string]any{{
					"mediaType":    "application/vnd.oci.image.manifest.v1+json",
					"artifactType": "application/vnd.dev.cosign.artifact.sig.v1+json",
					"digest":       dupManifest,
				}},
			})
		case "/v2/example.com/p/repo/app/manifests/" + dupManifest,
			"/v2/example.com/p/repo/app/manifests/" + strings.ReplaceAll(imgDigest, ":", "-") + ".sig":
			w.Header().Set("Docker-Content-Digest", dupManifest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"layers": []map[string]any{{
					"mediaType":   "application/vnd.dev.cosign.simplesigning.v1+json",
					"digest":      "sha256:layer",
					"annotations": map[string]string{"dev.cosignproject.cosign/signature": "c2ln"},
				}},
			})
		case "/v2/p/repo/denied/referrers/" + imgDigest:
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":[{"code":"DENIED","message":"Permission \"artifactregistry.repositories.downloadArtifacts\" denied on resource"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oci := liveOCI{http: srv.Client(), hostURL: func(string) string { return srv.URL }}

	// Domain-scoped project + fallback referrer tag + deduplication against .sig tag.
	got, _, err := oci.ListSignatures(context.Background(), "example.com:p", "us", "repo", "app", imgDigest, 100)
	if err != nil {
		t.Fatalf("fallback/dedup err = %v", err)
	}
	if len(got) != 1 || got[0].ID != dupManifest {
		t.Fatalf("expected 1 deduplicated sig, got %+v", got)
	}

	// Both 404 -> empty, nil.
	empty, trunc, err := oci.ListSignatures(context.Background(), "p", "us", "repo", "unsigned", imgDigest, 100)
	if err != nil || trunc || len(empty) != 0 {
		t.Fatalf("404 unsigned: len=%d trunc=%v err=%v", len(empty), trunc, err)
	}

	// HTTP 403 -> *googleapi.Error with permission message.
	_, _, err = oci.ListSignatures(context.Background(), "p", "us", "repo", "denied", imgDigest, 100)
	var ge *googleapi.Error
	if !errors.As(err, &ge) || ge.Code != http.StatusForbidden || !strings.Contains(ge.Message, "artifactregistry.repositories.downloadArtifacts") {
		t.Fatalf("403 err = %v", err)
	}
}

func TestLiveOCIEnrichVersionsAndListArtifacts(t *testing.T) {
	t.Parallel()
	const (
		idxDigest        = "sha256:1111111111111111"
		amd64Digest      = "sha256:2222222222222222"
		attestDigest     = "sha256:3333333333333333"
		sbomDigest       = "sha256:4444444444444444"
		sigDigest        = "sha256:5555555555555555"
		standaloneDigest = "sha256:6666666666666666"
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON := func(v any) {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(v); err != nil {
				t.Errorf("encode: %v", err)
			}
		}
		switch r.URL.Path {
		case "/v2/p/repo/app/manifests/" + idxDigest:
			writeJSON(map[string]any{
				"manifests": []map[string]any{
					{
						"mediaType": "application/vnd.oci.image.manifest.v1+json",
						"digest":    amd64Digest,
						"size":      1000,
						"platform":  map[string]string{"os": "linux", "architecture": "amd64"},
					},
					{
						"mediaType":   "application/vnd.oci.image.manifest.v1+json",
						"digest":      attestDigest,
						"size":        200,
						"platform":    map[string]string{"os": "unknown", "architecture": "unknown"},
						"annotations": map[string]string{"vnd.docker.reference.type": "attestation-manifest"},
					},
				},
			})
		case "/v2/p/repo/app/manifests/" + sbomDigest:
			writeJSON(map[string]any{
				"artifactType": "application/spdx+json",
				"subject":      map[string]string{"digest": idxDigest},
			})
		case "/v2/p/repo/app/manifests/" + sigDigest:
			writeJSON(map[string]any{
				"artifactType": sigstoreBundleMT + ".v0.3+json",
				"subject":      map[string]string{"digest": idxDigest},
			})
		case "/v2/p/repo/app/manifests/" + standaloneDigest:
			writeJSON(map[string]any{
				"config": map[string]string{"mediaType": "application/vnd.oci.image.config.v1+json"},
			})
		case "/v2/p/repo/app/referrers/" + idxDigest:
			writeJSON(map[string]any{
				"manifests": []map[string]any{
					{
						"mediaType":    "application/vnd.oci.image.manifest.v1+json",
						"artifactType": sigstoreBundleMT + ".v0.3+json",
						"digest":       sigDigest,
						"size":         300,
						"annotations":  map[string]string{"dev.sigstore.bundle.predicateType": cosignSignPredicate},
					},
					{
						"mediaType":    "application/vnd.oci.image.manifest.v1+json",
						"artifactType": "application/spdx+json",
						"digest":       sbomDigest,
						"size":         400,
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oci := liveOCI{http: srv.Client(), hostURL: func(string) string { return srv.URL }}
	versions := []Version{
		{Name: idxDigest, Tags: []string{"v1.0.0"}, MediaType: "application/vnd.oci.image.index.v1+json"},
		{Name: amd64Digest},
		{Name: attestDigest},
		{Name: sbomDigest},
		{Name: sigDigest},
		{Name: standaloneDigest},
	}
	oci.enrichVersions(context.Background(), "p", "us", "repo", "app", versions)

	if versions[1].ParentDigest != idxDigest || versions[1].Platform != "linux/amd64" || versions[1].IsAttestation {
		t.Errorf("amd64 child = %+v", versions[1])
	}
	if versions[2].ParentDigest != idxDigest || !versions[2].IsAttestation {
		t.Errorf("attestation child = %+v", versions[2])
	}
	if versions[3].SubjectDigest != idxDigest || versions[3].ArtifactType != "application/spdx+json" {
		t.Errorf("sbom referrer = %+v", versions[3])
	}
	if versions[4].SubjectDigest != idxDigest {
		t.Errorf("sig referrer = %+v", versions[4])
	}
	if versions[5].SubjectDigest != "" || versions[5].ParentDigest != "" {
		t.Errorf("standalone = %+v", versions[5])
	}

	artifacts, trunc, err := oci.ListArtifacts(context.Background(), "p", "us", "repo", "app", idxDigest, 100)
	if err != nil || trunc {
		t.Fatalf("ListArtifacts: trunc=%v err=%v", trunc, err)
	}
	if len(artifacts) != 3 {
		t.Fatalf("artifacts len = %d, want 3: %+v", len(artifacts), artifacts)
	}
	// sbomDigest (sigDigest skipped), amd64Digest, attestDigest.
	if artifacts[0].Name != sbomDigest || artifacts[1].Name != amd64Digest || artifacts[2].Name != attestDigest {
		t.Errorf("artifacts = %+v", artifacts)
	}
}
