package artifactregistry

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	ar "cloud.google.com/go/artifactregistry/apiv1"
	"cloud.google.com/go/artifactregistry/apiv1/artifactregistrypb"
	iampb "cloud.google.com/go/iam/apiv1/iampb"
	"golang.org/x/sync/errgroup"
	ca "google.golang.org/api/containeranalysis/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/api/option/internaloption"
	htransport "google.golang.org/api/transport/http"
	locationpb "google.golang.org/genproto/googleapis/cloud/location"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
	"github.com/tensleyconsulting/seiner/internal/gcp/regions"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// The Artifact Registry REST client cannot address packages whose id has a
// slash (web/frontend): the id must be sent as web%2Ffrontend, and the REST
// client escapes the % again. The gRPC client sends names unaltered.
type liveAPI struct{ client *ar.Client }

// NewLazy defers client construction to the first call, so a slow or failing
// constructor cannot block startup; the failure surfaces as this service's
// ProviderError instead. g closes the client once built.
func NewLazy(g *lazy.Group, opts ...option.ClientOption) Provider {
	l := lazy.NewIn(g, func(ctx context.Context) (*ar.Client, error) {
		return ar.NewClient(ctx, opts...)
	})
	httpClient := lazy.New(func(ctx context.Context) (*http.Client, error) {
		allOpts := append([]option.ClientOption{
			internaloption.WithDefaultScopes("https://www.googleapis.com/auth/cloud-platform"),
		}, opts...)
		c, _, err := htransport.NewClient(ctx, allOpts...)
		return c, err
	})
	locs := regions.Cached(func(ctx context.Context, project string) ([]string, error) {
		c, err := l.Get(ctx)
		if err != nil {
			return nil, err
		}
		return liveAPI{client: c}.ListLocations(ctx, project)
	})
	return New(lazyAPI{l: l, http: httpClient, locs: locs})
}

// NewLazyVulnerabilities is NewLazy for the Artifact Analysis client.
func NewLazyVulnerabilities(opts ...option.ClientOption) Vulnerabilities {
	l := lazy.New(func(ctx context.Context) (*ca.Service, error) {
		return ca.NewService(ctx, opts...)
	})
	return NewVulnerabilities(lazyScanAPI{l: l})
}

// lazyAPI builds the client on first use. Its location list is cached for
// the session, since it changes rarely and every repository list starts
// with it.
type lazyAPI struct {
	l    *lazy.Value[*ar.Client]
	http *lazy.Value[*http.Client]
	locs regions.Lister
}

func (a lazyAPI) ListLocations(ctx context.Context, project string) ([]string, error) {
	return a.locs.Regions(ctx, project)
}

func (a lazyAPI) ListRepositories(ctx context.Context, project, location string) ([]Repository, error) {
	c, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{client: c}.ListRepositories(ctx, project, location)
}

func (a lazyAPI) GetRepositoryPolicy(ctx context.Context, project, location, name string) ([]Binding, error) {
	c, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{client: c}.GetRepositoryPolicy(ctx, project, location, name)
}

func (a lazyAPI) ListPackages(ctx context.Context, project, location, repo string, limit int) ([]Package, bool, error) {
	c, err := a.l.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveAPI{client: c}.ListPackages(ctx, project, location, repo, limit)
}

func (a lazyAPI) ListVersions(ctx context.Context, project, location, repo, pkg string, limit int) ([]Version, bool, error) {
	c, err := a.l.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	versions, truncated, err := liveAPI{client: c}.ListVersions(ctx, project, location, repo, pkg, limit)
	if err != nil || !isDockerVersions(versions) {
		return versions, truncated, err
	}
	if hc, err := a.http.Get(ctx); err == nil {
		liveOCI{http: hc}.enrichVersions(ctx, project, location, repo, pkg, versions)
	}
	return versions, truncated, nil
}

func (a lazyAPI) ListSignatures(ctx context.Context, project, location, repo, pkg, digest string, limit int) ([]Signature, bool, error) {
	hc, err := a.http.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveOCI{http: hc}.ListSignatures(ctx, project, location, repo, pkg, digest, limit)
}

func (a lazyAPI) ListArtifacts(ctx context.Context, project, location, repo, pkg, digest string, limit int) ([]Version, bool, error) {
	hc, err := a.http.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveOCI{http: hc}.ListArtifacts(ctx, project, location, repo, pkg, digest, limit)
}

type lazyScanAPI struct{ l *lazy.Value[*ca.Service] }

func (a lazyScanAPI) ListVulnerabilities(ctx context.Context, project, resourceURL string, limit int) ([]Vulnerability, bool, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveScanAPI{svc: svc}.ListVulnerabilities(ctx, project, resourceURL, limit)
}

var (
	_ API     = lazyAPI{}
	_ API     = liveAPI{}
	_ ScanAPI = lazyScanAPI{}
	_ ScanAPI = liveScanAPI{}
)

func (a liveAPI) ListLocations(ctx context.Context, project string) ([]string, error) {
	it := a.client.ListLocations(ctx, &locationpb.ListLocationsRequest{Name: "projects/" + project})
	var out []string
	for {
		l, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		out = append(out, l.GetLocationId())
	}
}

func (a liveAPI) ListRepositories(ctx context.Context, project, location string) ([]Repository, error) {
	it := a.client.ListRepositories(ctx, &artifactregistrypb.ListRepositoriesRequest{
		Parent: "projects/" + project + "/locations/" + location,
	})
	var out []Repository
	for {
		r, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		out = append(out, fromRepository(r))
	}
}

func (a liveAPI) GetRepositoryPolicy(ctx context.Context, project, location, name string) ([]Binding, error) {
	// Version 3 reads conditional bindings.
	p, err := a.client.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{
		Resource: repositoryID(project, location, name),
		Options:  &iampb.GetPolicyOptions{RequestedPolicyVersion: 3},
	})
	if err != nil {
		return nil, err
	}
	out := make([]Binding, 0, len(p.GetBindings()))
	for _, b := range p.GetBindings() {
		out = append(out, Binding{
			Role:      b.GetRole(),
			Members:   append([]string(nil), b.GetMembers()...),
			Condition: b.GetCondition().GetTitle(),
		})
	}
	return out, nil
}

func (a liveAPI) ListPackages(ctx context.Context, project, location, repo string, limit int) ([]Package, bool, error) {
	it := a.client.ListPackages(ctx, &artifactregistrypb.ListPackagesRequest{
		Parent:   repositoryID(project, location, repo),
		PageSize: pageSize(limit),
	})
	var out []Package
	for {
		p, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return out, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		if len(out) == limit {
			return out, true, nil
		}
		out = append(out, fromPackage(p))
	}
}

func (a liveAPI) ListVersions(ctx context.Context, project, location, repo, pkg string, limit int) ([]Version, bool, error) {
	it := a.client.ListVersions(ctx, &artifactregistrypb.ListVersionsRequest{
		Parent:   packageID(repositoryID(project, location, repo), pkg),
		PageSize: pageSize(limit),
		View:     artifactregistrypb.VersionView_FULL, // FULL adds tags and image metadata
	})
	var out []Version
	for {
		v, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return out, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		if len(out) == limit {
			return out, true, nil
		}
		out = append(out, fromVersion(v))
	}
}

func (a liveAPI) ListSignatures(ctx context.Context, project, location, repo, pkg, digest string, limit int) ([]Signature, bool, error) {
	return nil, false, errors.New("artifactregistry: no OCI HTTP client")
}

func (a liveAPI) ListArtifacts(ctx context.Context, project, location, repo, pkg, digest string, limit int) ([]Version, bool, error) {
	return nil, false, errors.New("artifactregistry: no OCI HTTP client")
}

// pageSize asks for one more than limit, so a full page tells truncation
// apart from an exact fit. The API caps pages at 1,000.
func pageSize(limit int) int32 {
	return int32(min(limit+1, 1000))
}

func fromRepository(r *artifactregistrypb.Repository) Repository {
	out := Repository{
		Name:            unescapedLast(r.GetName()),
		Location:        locationOf(r.GetName()),
		Description:     r.GetDescription(),
		Format:          r.GetFormat().String(),
		Mode:            r.GetMode().String(),
		RegistryURI:     r.GetRegistryUri(),
		SizeBytes:       r.GetSizeBytes(),
		KMSKey:          r.GetKmsKeyName(),
		ImmutableTags:   r.GetDockerConfig().GetImmutableTags(),
		CleanupPolicies: len(r.GetCleanupPolicies()),
		CleanupDryRun:   r.GetCleanupPolicyDryRun(),
		Upstream:        r.GetRemoteRepositoryConfig().GetDescription(),
		Created:         timeOf(r.GetCreateTime()),
		Updated:         timeOf(r.GetUpdateTime()),
		Labels:          r.GetLabels(),
	}
	if out.Format == "FORMAT_UNSPECIFIED" {
		out.Format = ""
	}
	if vc := r.GetVirtualRepositoryConfig(); vc != nil {
		out.VirtualUpstreams = len(vc.GetUpstreamPolicies())
		policies := slices.Clone(vc.GetUpstreamPolicies())
		slices.SortStableFunc(policies, func(a, b *artifactregistrypb.UpstreamPolicy) int {
			return int(a.GetPriority()) - int(b.GetPriority())
		})
		for _, u := range policies {
			out.UpstreamRules = append(out.UpstreamRules, fmt.Sprintf("priority %d  %s", u.GetPriority(), u.GetRepository()))
		}
	}
	for _, id := range slices.Sorted(maps.Keys(r.GetCleanupPolicies())) {
		out.CleanupRules = append(out.CleanupRules, cleanupRule(id, r.GetCleanupPolicies()[id]))
	}
	if rc := r.GetRemoteRepositoryConfig(); rc != nil {
		out.RemoteSource = remoteSource(rc)
		if c := rc.GetUpstreamCredentials().GetUsernamePasswordCredentials(); c != nil {
			out.UpstreamUser = c.GetUsername()
			out.UpstreamSecret = c.GetPasswordSecretVersion()
		}
		out.NoUpstreamValidate = rc.GetDisableUpstreamValidation()
	}
	if mc := r.GetMavenConfig(); mc != nil {
		if p := mc.GetVersionPolicy().String(); p != "VERSION_POLICY_UNSPECIFIED" {
			out.MavenPolicy = p
		}
		out.MavenOverwrites = mc.GetAllowSnapshotOverwrites()
	}
	if sc := r.GetVulnerabilityScanningConfig(); sc != nil {
		out.ScanState = sc.GetEnablementState().String()
		out.ScanReason = sc.GetEnablementStateReason()
	}
	return out
}

// cleanupRule reads "id  DELETE  untagged, older than 30d, packages web/"
// or "id  KEEP  5 most recent".
func cleanupRule(id string, p *artifactregistrypb.CleanupPolicy) string {
	var parts []string
	if c := p.GetCondition(); c != nil {
		if ts := c.GetTagState().String(); ts != "TAG_STATE_UNSPECIFIED" && ts != "ANY" {
			parts = append(parts, strings.ToLower(ts))
		}
		if d := c.GetOlderThan(); d != nil {
			parts = append(parts, "older than "+days(d.AsDuration()))
		}
		if d := c.GetNewerThan(); d != nil {
			parts = append(parts, "newer than "+days(d.AsDuration()))
		}
		if len(c.GetTagPrefixes()) > 0 {
			parts = append(parts, "tags "+strings.Join(c.GetTagPrefixes(), ","))
		}
		if len(c.GetVersionNamePrefixes()) > 0 {
			parts = append(parts, "versions "+strings.Join(c.GetVersionNamePrefixes(), ","))
		}
		if len(c.GetPackageNamePrefixes()) > 0 {
			parts = append(parts, "packages "+strings.Join(c.GetPackageNamePrefixes(), ","))
		}
	}
	if m := p.GetMostRecentVersions(); m != nil {
		parts = append(parts, fmt.Sprintf("%d most recent", m.GetKeepCount()))
		if len(m.GetPackageNamePrefixes()) > 0 {
			parts = append(parts, "packages "+strings.Join(m.GetPackageNamePrefixes(), ","))
		}
	}
	if len(parts) == 0 {
		parts = []string{"any version"}
	}
	return id + "  " + p.GetAction().String() + "  " + strings.Join(parts, ", ")
}

// days reads a duration in whole days when it is one ("30d"), and as Go
// prints it otherwise.
func days(d time.Duration) string {
	if d > 0 && d%(24*time.Hour) == 0 {
		return strconv.Itoa(int(d/(24*time.Hour))) + "d"
	}
	return d.String()
}

// remoteSource names a remote repository's upstream: the public preset,
// or the custom URI.
func remoteSource(rc *artifactregistrypb.RemoteRepositoryConfig) string {
	public := func(s string) string {
		if strings.HasSuffix(s, "_UNSPECIFIED") {
			return ""
		}
		return s
	}
	switch {
	case rc.GetDockerRepository() != nil:
		r := rc.GetDockerRepository()
		return cmp.Or(r.GetCustomRepository().GetUri(), public(r.GetPublicRepository().String()))
	case rc.GetMavenRepository() != nil:
		r := rc.GetMavenRepository()
		return cmp.Or(r.GetCustomRepository().GetUri(), public(r.GetPublicRepository().String()))
	case rc.GetNpmRepository() != nil:
		r := rc.GetNpmRepository()
		return cmp.Or(r.GetCustomRepository().GetUri(), public(r.GetPublicRepository().String()))
	case rc.GetPythonRepository() != nil:
		r := rc.GetPythonRepository()
		return cmp.Or(r.GetCustomRepository().GetUri(), public(r.GetPublicRepository().String()))
	case rc.GetAptRepository() != nil:
		r := rc.GetAptRepository()
		if p := r.GetPublicRepository(); p != nil {
			return strings.TrimSuffix(public(p.GetRepositoryBase().String())+" "+p.GetRepositoryPath(), " ")
		}
		return r.GetCustomRepository().GetUri()
	case rc.GetYumRepository() != nil:
		r := rc.GetYumRepository()
		if p := r.GetPublicRepository(); p != nil {
			return strings.TrimSuffix(public(p.GetRepositoryBase().String())+" "+p.GetRepositoryPath(), " ")
		}
		return r.GetCustomRepository().GetUri()
	case rc.GetCommonRepository() != nil:
		return rc.GetCommonRepository().GetUri()
	default:
		return ""
	}
}

func fromPackage(p *artifactregistrypb.Package) Package {
	return Package{
		Name:        unescapedLast(p.GetName()),
		DisplayName: p.GetDisplayName(),
		Created:     timeOf(p.GetCreateTime()),
		Updated:     timeOf(p.GetUpdateTime()),
	}
}

func fromVersion(v *artifactregistrypb.Version) Version {
	out := Version{
		Name:        unescapedLast(v.GetName()),
		Description: v.GetDescription(),
		Created:     timeOf(v.GetCreateTime()),
		Updated:     timeOf(v.GetUpdateTime()),
	}
	for _, t := range v.GetRelatedTags() {
		out.Tags = append(out.Tags, unescapedLast(t.GetName()))
	}
	// Docker versions carry the DockerImage fields as a struct.
	md := v.GetMetadata().GetFields()
	out.SizeBytes = int64Of(md["imageSizeBytes"])
	out.MediaType = md["mediaType"].GetStringValue()
	out.ArtifactType = md["artifactType"].GetStringValue()
	out.BuildTime = parseTime(md["buildTime"].GetStringValue())
	return out
}

// int64Of reads an int64 that JSON-mapped protos encode as a string.
func int64Of(v *structpb.Value) int64 {
	switch k := v.GetKind().(type) {
	case *structpb.Value_StringValue:
		n, _ := strconv.ParseInt(k.StringValue, 10, 64)
		return n
	case *structpb.Value_NumberValue:
		return int64(k.NumberValue)
	}
	return 0
}

// unescapedLast is the last path segment of a resource name, unescaped
// (packages/web%2Ffrontend → web/frontend).
func unescapedLast(name string) string {
	seg := lastSeg(name)
	if u, err := url.PathUnescape(seg); err == nil {
		return u
	}
	return seg
}

func locationOf(name string) string {
	n, ok := model.ParseName(name)
	if !ok {
		return ""
	}
	return n.Location
}

func timeOf(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// liveScanAPI reads vulnerability occurrences through the Container Analysis
// REST API. Its names (projects/p) and filters need no path escaping.
type liveScanAPI struct{ svc *ca.Service }

func (a liveScanAPI) ListVulnerabilities(ctx context.Context, project, resourceURL string, limit int) ([]Vulnerability, bool, error) {
	filter := `kind="VULNERABILITY" AND resourceUrl="` + strings.ReplaceAll(resourceURL, `"`, `\"`) + `"`
	call := a.svc.Projects.Occurrences.List("projects/" + project).Filter(filter).PageSize(int64(pageSize(limit)))
	var out []Vulnerability
	token := ""
	for {
		resp, err := call.PageToken(token).Context(ctx).Do()
		if err != nil {
			return nil, false, err
		}
		for _, o := range resp.Occurrences {
			if len(out) == limit {
				return out, true, nil
			}
			if o.Vulnerability != nil {
				out = append(out, fromOccurrence(o))
			}
		}
		if resp.NextPageToken == "" {
			return out, false, nil
		}
		token = resp.NextPageToken
	}
}

func fromOccurrence(o *ca.Occurrence) Vulnerability {
	v := o.Vulnerability
	out := Vulnerability{
		ID:           o.Name,
		CVE:          lastSeg(o.NoteName),
		Severity:     v.Severity,
		Effective:    v.EffectiveSeverity,
		CVSS:         v.CvssScore,
		CVSSVersion:  v.CvssVersion,
		FixAvailable: v.FixAvailable,
		Summary:      v.ShortDescription,
		Details:      v.LongDescription,
		Published:    parseTime(o.AdvisoryPublishTime),
		Created:      parseTime(o.CreateTime),
		Updated:      parseTime(o.UpdateTime),
	}
	if out.Effective == "" || out.Effective == "SEVERITY_UNSPECIFIED" {
		out.Effective = v.Severity
	}
	for _, pi := range v.PackageIssue {
		p := AffectedPackage{
			Name:      pi.AffectedPackage,
			Type:      pi.PackageType,
			Installed: versionString(pi.AffectedVersion),
			Fixed:     versionString(pi.FixedVersion),
		}
		if len(pi.FileLocation) > 0 {
			p.Path = pi.FileLocation[0].FilePath
		}
		out.Packages = append(out.Packages, p)
	}
	for _, u := range v.RelatedUrls {
		if u.Url != "" {
			out.URLs = append(out.URLs, u.Url)
		}
	}
	return out
}

// versionString renders a Grafeas version. The MAXIMUM sentinel means no
// fixed version exists, so it renders as "".
func versionString(v *ca.Version) string {
	if v == nil || v.Kind == "MAXIMUM" || v.Kind == "MINIMUM" {
		return ""
	}
	if v.FullName != "" {
		return v.FullName
	}
	s := v.Name
	if v.Revision != "" {
		s += "-" + v.Revision
	}
	if v.Epoch > 0 {
		s = strconv.FormatInt(v.Epoch, 10) + ":" + s
	}
	return s
}

const (
	maxManifestBytes = 1 << 20
	maxBundleBytes   = 1 << 20

	ociIndexAccept    = "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json"
	ociManifestAccept = "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"

	cosignSignPredicate = "https://sigstore.dev/cosign/sign/v1"
	sigstoreBundleMT    = "application/vnd.dev.sigstore.bundle"
	cosignSimpleMT      = "application/vnd.dev.cosign.simplesigning.v1+json"
	cosignArtifactSigMT = "application/vnd.dev.cosign.artifact.sig.v1+json"
	dsseEnvelopeMT      = "application/vnd.dsse.envelope.v1+json"
	inTotoPayloadType   = "application/vnd.in-toto+json"
)

type liveOCI struct {
	http    *http.Client
	hostURL func(location string) string // nil uses "https://" + location + "-docker.pkg.dev"
}

func (c liveOCI) baseURL(location string) string {
	if c.hostURL != nil {
		return strings.TrimRight(c.hostURL(location), "/")
	}
	return "https://" + location + "-docker.pkg.dev"
}

func ociRepoPath(project, repo, pkg string) string {
	raw := strings.ReplaceAll(project, ":", "/") + "/" + repo + "/" + pkg
	segs := strings.Split(raw, "/")
	out := make([]string, 0, len(segs))
	for _, s := range segs {
		if s == "" {
			continue
		}
		out = append(out, url.PathEscape(s))
	}
	return strings.Join(out, "/")
}

func (c liveOCI) ListSignatures(ctx context.Context, project, location, repo, pkg, digest string, limit int) ([]Signature, bool, error) {
	if !strings.Contains(digest, ":") || limit <= 0 {
		return nil, false, nil
	}
	if c.http == nil {
		return nil, false, errors.New("artifactregistry: no OCI HTTP client")
	}
	base := c.baseURL(location) + "/v2/" + ociRepoPath(project, repo, pkg)
	seenManifests := map[string]bool{}
	var out []Signature

	// Pass 1: OCI 1.1 Referrers API (with OCI 1.1 fallback referrer tag).
	referrers, err := c.fetchReferrers(ctx, base, digest)
	if err != nil {
		return nil, false, err
	}
	for _, desc := range referrers {
		if !isSignatureReferrer(desc) {
			continue
		}
		manifest, manifestDigest, ok, err := c.fetchManifest(ctx, base, desc.Digest)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			continue
		}
		if manifestDigest == "" {
			manifestDigest = desc.Digest
		}
		seenManifests[manifestDigest] = true
		pred := cmp.Or(desc.Annotations["dev.sigstore.bundle.predicateType"], manifest.Annotations["dev.sigstore.bundle.predicateType"])
		manifestAT := cmp.Or(manifest.ArtifactType, desc.ArtifactType, manifest.Config.MediaType, desc.MediaType)

		var sigLayers []ociDescriptor
		for _, layer := range manifest.Layers {
			if strings.HasPrefix(layer.MediaType, sigstoreBundleMT) || isSimpleSigningLayer(layer) {
				sigLayers = append(sigLayers, layer)
			}
		}
		for i, layer := range sigLayers {
			id := layerID(manifestDigest, i, len(sigLayers))
			if strings.HasPrefix(layer.MediaType, sigstoreBundleMT) {
				if layer.Size > maxBundleBytes {
					continue
				}
				blob, ok, err := c.fetchBlob(ctx, base, layer.Digest, maxBundleBytes)
				if err != nil {
					return nil, false, err
				}
				if !ok {
					continue
				}
				sig, keep, err := parseSigstoreBundle(blob, id, manifestDigest, layer.Digest, cmp.Or(layer.MediaType, manifestAT), pred)
				if err != nil || !keep {
					continue
				}
				if len(out) == limit {
					return out, true, nil
				}
				out = append(out, sig)
				continue
			}
			if len(out) == limit {
				return out, true, nil
			}
			out = append(out, fromSimpleSigningLayer(layer, id, "referrer", "", manifestDigest, manifestAT, pred))
		}
	}

	// Pass 2: Legacy cosign sha256-<hex>.sig tag manifest.
	sigTag := strings.ReplaceAll(digest, ":", "-") + ".sig"
	manifest, manifestDigest, ok, err := c.fetchManifest(ctx, base, sigTag)
	if err != nil {
		return nil, false, err
	}
	if !ok || seenManifests[manifestDigest] {
		return out, false, nil
	}
	var sigLayers []ociDescriptor
	for _, layer := range manifest.Layers {
		if isSimpleSigningLayer(layer) {
			sigLayers = append(sigLayers, layer)
		}
	}
	for i, layer := range sigLayers {
		if len(out) == limit {
			return out, true, nil
		}
		id := layerID(manifestDigest, i, len(sigLayers))
		out = append(out, fromSimpleSigningLayer(layer, id, "tag", sigTag, manifestDigest, cmp.Or(manifest.ArtifactType, layer.MediaType), ""))
	}
	return out, false, nil
}

func layerID(manifestDigest string, idx, total int) string {
	if total <= 1 {
		return manifestDigest
	}
	return manifestDigest + "-" + strconv.Itoa(idx+1)
}

func isSignatureReferrer(desc ociDescriptor) bool {
	at := cmp.Or(desc.ArtifactType, desc.MediaType)
	pred := desc.Annotations["dev.sigstore.bundle.predicateType"]
	if strings.HasPrefix(at, sigstoreBundleMT) {
		return pred == "" || pred == cosignSignPredicate
	}
	return at == cosignArtifactSigMT || at == cosignSimpleMT
}

func isSimpleSigningLayer(layer ociDescriptor) bool {
	return layer.MediaType == cosignSimpleMT ||
		layer.MediaType == dsseEnvelopeMT ||
		layer.Annotations["dev.cosignproject.cosign/signature"] != ""
}

type ociIndex struct {
	Manifests []ociDescriptor `json:"manifests"`
}

type ociManifest struct {
	ArtifactType string            `json:"artifactType"`
	Config       ociDescriptor     `json:"config"`
	Layers       []ociDescriptor   `json:"layers"`
	Subject      *ociDescriptor    `json:"subject"`
	Annotations  map[string]string `json:"annotations"`
}

type ociPlatform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant"`
}

type ociDescriptor struct {
	MediaType    string            `json:"mediaType"`
	ArtifactType string            `json:"artifactType"`
	Digest       string            `json:"digest"`
	Size         int64             `json:"size"`
	Platform     *ociPlatform      `json:"platform"`
	Annotations  map[string]string `json:"annotations"`
}

func isDockerVersions(versions []Version) bool {
	for _, v := range versions {
		if strings.HasPrefix(v.Name, "sha256:") {
			return true
		}
	}
	return false
}

func isImageIndex(mediaType string) bool {
	return mediaType == "application/vnd.oci.image.index.v1+json" ||
		mediaType == "application/vnd.docker.distribution.manifest.list.v2+json"
}

func isImageConfigMediaType(mt string) bool {
	return mt == "" ||
		mt == "application/vnd.oci.image.config.v1+json" ||
		mt == "application/vnd.docker.container.image.v1+json" ||
		mt == "application/vnd.oci.empty.v1+json"
}

func formatPlatform(p *ociPlatform) string {
	if p == nil || (p.OS == "" && p.Architecture == "") {
		return ""
	}
	s := p.OS + "/" + p.Architecture
	if p.Variant != "" {
		s += "/" + p.Variant
	}
	return s
}

func isAttestationDescriptor(d ociDescriptor) bool {
	if d.Annotations["vnd.docker.reference.type"] == "attestation-manifest" {
		return true
	}
	return d.Platform != nil && d.Platform.OS == "unknown" && d.Platform.Architecture == "unknown"
}

// enrichVersions populates SubjectDigest (from OCI 1.1 referrer manifests)
// and ParentDigest/Platform (from OCI image indexes) on Docker versions so
// attached artifacts can be nested under their parent image instead of
// cluttering Digests. Best-effort: individual OCI fetch errors are ignored.
func (c liveOCI) enrichVersions(ctx context.Context, project, location, repo, pkg string, versions []Version) {
	if c.http == nil || len(versions) == 0 {
		return
	}
	base := c.baseURL(location) + "/v2/" + ociRepoPath(project, repo, pkg)

	type childInfo struct {
		parentDigest  string
		platform      string
		isAttestation bool
	}
	indexResults := make([][]ociDescriptor, len(versions))
	manifests := make([]*ociManifest, len(versions))

	var g errgroup.Group
	g.SetLimit(16)
	for i, v := range versions {
		if isImageIndex(v.MediaType) {
			g.Go(func() error {
				idx, ok, err := c.fetchIndex(ctx, base, v.Name)
				if err == nil && ok {
					indexResults[i] = idx.Manifests
				}
				return nil
			})
		} else if len(v.Tags) == 0 && !isSigTagVersion(v) {
			g.Go(func() error {
				m, _, ok, err := c.fetchManifest(ctx, base, v.Name)
				if err == nil && ok {
					manifests[i] = &m
				}
				return nil
			})
		}
	}
	_ = g.Wait()

	children := make(map[string]childInfo)
	for i, descs := range indexResults {
		parent := versions[i].Name
		for _, d := range descs {
			if d.Digest == "" {
				continue
			}
			if _, exists := children[d.Digest]; !exists {
				children[d.Digest] = childInfo{
					parentDigest:  parent,
					platform:      formatPlatform(d.Platform),
					isAttestation: isAttestationDescriptor(d),
				}
			}
		}
	}

	for i := range versions {
		v := &versions[i]
		if m := manifests[i]; m != nil {
			if m.Subject != nil && m.Subject.Digest != "" {
				v.SubjectDigest = m.Subject.Digest
			}
			if v.ArtifactType == "" {
				if m.ArtifactType != "" {
					v.ArtifactType = m.ArtifactType
				} else if !isImageConfigMediaType(m.Config.MediaType) {
					v.ArtifactType = m.Config.MediaType
				}
			}
		}
		if len(v.Tags) == 0 && v.SubjectDigest == "" {
			if ch, ok := children[v.Name]; ok {
				v.ParentDigest = ch.parentDigest
				v.Platform = ch.platform
				v.IsAttestation = ch.isAttestation
			}
		}
	}
}

func (c liveOCI) ListArtifacts(ctx context.Context, project, location, repo, pkg, digest string, limit int) ([]Version, bool, error) {
	if !strings.Contains(digest, ":") || limit <= 0 {
		return nil, false, nil
	}
	if c.http == nil {
		return nil, false, errors.New("artifactregistry: no OCI HTTP client")
	}
	base := c.baseURL(location) + "/v2/" + ociRepoPath(project, repo, pkg)
	seen := map[string]bool{}
	var out []Version

	referrers, err := c.fetchReferrers(ctx, base, digest)
	if err != nil {
		return nil, false, err
	}
	for _, desc := range referrers {
		if isSignatureReferrer(desc) || desc.Digest == "" || seen[desc.Digest] {
			continue
		}
		if len(out) == limit {
			return out, true, nil
		}
		seen[desc.Digest] = true
		out = append(out, Version{
			Name:          desc.Digest,
			Description:   cmp.Or(desc.Annotations["dev.sigstore.bundle.predicateType"], desc.Annotations["org.opencontainers.image.title"]),
			SizeBytes:     desc.Size,
			MediaType:     desc.MediaType,
			ArtifactType:  desc.ArtifactType,
			SubjectDigest: digest,
		})
	}

	idx, ok, err := c.fetchIndex(ctx, base, digest)
	if err != nil {
		return nil, false, err
	}
	if ok {
		for _, desc := range idx.Manifests {
			if desc.Digest == "" || seen[desc.Digest] {
				continue
			}
			if len(out) == limit {
				return out, true, nil
			}
			seen[desc.Digest] = true
			out = append(out, Version{
				Name:          desc.Digest,
				SizeBytes:     desc.Size,
				MediaType:     desc.MediaType,
				ArtifactType:  desc.ArtifactType,
				ParentDigest:  digest,
				Platform:      formatPlatform(desc.Platform),
				IsAttestation: isAttestationDescriptor(desc),
			})
		}
	}
	return out, false, nil
}

func (c liveOCI) fetchIndex(ctx context.Context, base, ref string) (ociIndex, bool, error) {
	body, _, ok, err := c.get(ctx, base+"/manifests/"+url.PathEscape(ref), ociIndexAccept, maxManifestBytes)
	if err != nil || !ok {
		return ociIndex{}, ok, err
	}
	var idx ociIndex
	if err := json.Unmarshal(body, &idx); err != nil {
		return ociIndex{}, false, nil
	}
	if len(idx.Manifests) == 0 {
		return ociIndex{}, false, nil
	}
	return idx, true, nil
}

func (c liveOCI) fetchReferrers(ctx context.Context, base, digest string) ([]ociDescriptor, error) {
	body, _, ok, err := c.get(ctx, base+"/referrers/"+url.PathEscape(digest), ociIndexAccept, maxManifestBytes)
	if err != nil {
		return nil, err
	}
	if !ok {
		fallback := strings.ReplaceAll(digest, ":", "-")
		body, _, ok, err = c.get(ctx, base+"/manifests/"+url.PathEscape(fallback), ociIndexAccept, maxManifestBytes)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
	}
	var idx ociIndex
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("artifactregistry: decode referrers: %w", err)
	}
	return idx.Manifests, nil
}

func (c liveOCI) fetchManifest(ctx context.Context, base, ref string) (ociManifest, string, bool, error) {
	body, hdrDigest, ok, err := c.get(ctx, base+"/manifests/"+url.PathEscape(ref), ociManifestAccept, maxManifestBytes)
	if err != nil || !ok {
		return ociManifest{}, "", ok, err
	}
	var m ociManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return ociManifest{}, "", false, fmt.Errorf("artifactregistry: decode manifest: %w", err)
	}
	digest := hdrDigest
	if digest == "" {
		sum := sha256.Sum256(body)
		digest = "sha256:" + hex.EncodeToString(sum[:])
	}
	return m, digest, true, nil
}

func (c liveOCI) fetchBlob(ctx context.Context, base, digest string, maxBytes int64) ([]byte, bool, error) {
	body, _, ok, err := c.get(ctx, base+"/blobs/"+url.PathEscape(digest), "", maxBytes)
	return body, ok, err
}

func (c liveOCI) get(ctx context.Context, reqURL, accept string, maxBytes int64) (body []byte, digest string, ok bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, "", false, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", false, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", false, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", false, ociHTTPError(resp, data)
	}
	if int64(len(data)) > maxBytes {
		return nil, "", false, fmt.Errorf("artifactregistry: response exceeds %d bytes", maxBytes)
	}
	return data, strings.TrimSpace(resp.Header.Get("Docker-Content-Digest")), true, nil
}

func ociHTTPError(resp *http.Response, body []byte) error {
	var ociErr struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	msg := ""
	if json.Unmarshal(body, &ociErr) == nil {
		if len(ociErr.Errors) > 0 {
			e := ociErr.Errors[0]
			msg = strings.TrimSpace(cmp.Or(e.Message, e.Code))
		} else if ociErr.Error.Message != "" {
			msg = strings.TrimSpace(ociErr.Error.Message)
		}
	}
	if msg == "" {
		msg = resp.Status
	}
	return &googleapi.Error{Code: resp.StatusCode, Message: msg}
}

type flexInt64 int64

func (f *flexInt64) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "" || s == "null" {
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		unq, err := strconv.Unquote(s)
		if err != nil {
			return err
		}
		s = unq
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	*f = flexInt64(v)
	return nil
}

type sigstoreBundle struct {
	VerificationMaterial struct {
		Certificate *struct {
			RawBytes string `json:"rawBytes"`
		} `json:"certificate"`
		X509CertificateChain *struct {
			Certificates []struct {
				RawBytes string `json:"rawBytes"`
			} `json:"certificates"`
		} `json:"x509CertificateChain"`
		PublicKey *struct {
			Hint string `json:"hint"`
		} `json:"publicKey"`
		TlogEntries []struct {
			LogIndex       *flexInt64 `json:"logIndex"`
			IntegratedTime *flexInt64 `json:"integratedTime"`
			LogID          *struct {
				KeyID string `json:"keyId"`
			} `json:"logId"`
		} `json:"tlogEntries"`
	} `json:"verificationMaterial"`
	MessageSignature *struct {
		Signature string `json:"signature"`
	} `json:"messageSignature"`
	DsseEnvelope *struct {
		PayloadType string `json:"payloadType"`
		Payload     string `json:"payload"`
		Signatures  []struct {
			KeyID string `json:"keyid"`
			Sig   string `json:"sig"`
		} `json:"signatures"`
	} `json:"dsseEnvelope"`
}

func parseSigstoreBundle(blob []byte, id, manifestDigest, payloadDigest, artifactType, predicateType string) (Signature, bool, error) {
	var b sigstoreBundle
	if err := json.Unmarshal(blob, &b); err != nil {
		return Signature{}, false, err
	}
	if b.DsseEnvelope != nil && b.DsseEnvelope.PayloadType == inTotoPayloadType && predicateType == "" {
		if pt := inTotoPredicateType(b.DsseEnvelope.Payload); pt != "" {
			predicateType = pt
			if pt != cosignSignPredicate {
				return Signature{}, false, nil
			}
		}
	}

	out := Signature{
		ID:             id,
		Source:         "referrer",
		ManifestDigest: manifestDigest,
		ArtifactType:   artifactType,
		PredicateType:  predicateType,
		PayloadDigest:  payloadDigest,
		BundleJSON:     strings.TrimSpace(string(blob)),
	}

	if b.MessageSignature != nil {
		out.Signature = b.MessageSignature.Signature
	} else if b.DsseEnvelope != nil && len(b.DsseEnvelope.Signatures) > 0 {
		out.Signature = b.DsseEnvelope.Signatures[0].Sig
		out.KeyHint = b.DsseEnvelope.Signatures[0].KeyID
	}

	vm := b.VerificationMaterial
	if vm.Certificate != nil && vm.Certificate.RawBytes != "" {
		out.CertificatePEM = derBase64ToPEM(vm.Certificate.RawBytes)
	}
	if vm.X509CertificateChain != nil {
		for _, c := range vm.X509CertificateChain.Certificates {
			p := derBase64ToPEM(c.RawBytes)
			if p == "" {
				continue
			}
			if out.CertificatePEM == "" {
				out.CertificatePEM = p
			} else {
				out.ChainPEM = append(out.ChainPEM, p)
			}
		}
	}
	if vm.PublicKey != nil && vm.PublicKey.Hint != "" {
		out.KeyHint = cmp.Or(out.KeyHint, vm.PublicKey.Hint)
	}
	if len(vm.TlogEntries) > 0 {
		entry := vm.TlogEntries[0]
		if entry.LogIndex != nil {
			out.HasRekor = true
			out.RekorLogIndex = int64(*entry.LogIndex)
		}
		if entry.IntegratedTime != nil && *entry.IntegratedTime > 0 {
			out.IntegratedTime = time.Unix(int64(*entry.IntegratedTime), 0).UTC()
		}
		if entry.LogID != nil && entry.LogID.KeyID != "" {
			out.RekorLogID = normalizeLogID(entry.LogID.KeyID)
		}
	}
	return out, true, nil
}

func inTotoPredicateType(b64Payload string) string {
	raw, err := base64.StdEncoding.DecodeString(b64Payload)
	if err != nil {
		return ""
	}
	var stmt struct {
		PredicateType string `json:"predicateType"`
	}
	if json.Unmarshal(raw, &stmt) != nil {
		return ""
	}
	return stmt.PredicateType
}

func derBase64ToPEM(b64 string) string {
	der, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(der) == 0 {
		return ""
	}
	return strings.TrimSpace(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
}

func normalizeLogID(s string) string {
	if raw, err := base64.StdEncoding.DecodeString(s); err == nil && len(raw) == 32 {
		return hex.EncodeToString(raw)
	}
	return s
}

func fromSimpleSigningLayer(layer ociDescriptor, id, source, tag, manifestDigest, artifactType, predicateType string) Signature {
	ann := layer.Annotations
	out := Signature{
		ID:             id,
		Source:         source,
		Tag:            tag,
		ManifestDigest: manifestDigest,
		ArtifactType:   cmp.Or(artifactType, layer.MediaType),
		PredicateType:  cmp.Or(predicateType, ann["dev.sigstore.bundle.predicateType"]),
		PayloadDigest:  layer.Digest,
		Signature:      ann["dev.cosignproject.cosign/signature"],
		CertificatePEM: strings.TrimSpace(ann["dev.sigstore.cosign/certificate"]),
		ChainPEM:       splitPEMBlocks(ann["dev.sigstore.cosign/chain"]),
		BundleJSON:     strings.TrimSpace(ann["dev.sigstore.cosign/bundle"]),
	}
	if out.BundleJSON != "" {
		var rekor struct {
			Payload struct {
				LogIndex       *flexInt64 `json:"logIndex"`
				IntegratedTime *flexInt64 `json:"integratedTime"`
				LogID          string     `json:"logID"`
			} `json:"Payload"`
		}
		if json.Unmarshal([]byte(out.BundleJSON), &rekor) == nil {
			if rekor.Payload.LogIndex != nil {
				out.HasRekor = true
				out.RekorLogIndex = int64(*rekor.Payload.LogIndex)
			}
			if rekor.Payload.IntegratedTime != nil && *rekor.Payload.IntegratedTime > 0 {
				out.IntegratedTime = time.Unix(int64(*rekor.Payload.IntegratedTime), 0).UTC()
			}
			out.RekorLogID = rekor.Payload.LogID
		}
	}
	return out
}

func splitPEMBlocks(s string) []string {
	var out []string
	rest := []byte(s)
	for len(rest) > 0 {
		var blk *pem.Block
		blk, rest = pem.Decode(rest)
		if blk == nil {
			break
		}
		out = append(out, strings.TrimSpace(string(pem.EncodeToMemory(blk))))
	}
	return out
}
