package artifactregistry

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tensleyconsulting/seiner/internal/gcp/fanout"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// Limits on facet children. The explorer shows "more not shown" past them.
const (
	maxPackages        = 500
	maxVersions        = 200
	maxSignatures      = 100
	maxVulnerabilities = 500
	// locationConcurrency bounds the per-location repository list calls. A
	// project has a few dozen locations; 24 lists them in about two rounds
	// instead of five or more, without a large burst against read quota.
	locationConcurrency = 24
)

// Provider implements model.ResourceProvider for Artifact Registry
// repositories. Packages and versions are not listed at the top level; they
// are the repository's Packages facet and the package's Versions facet. A
// repository's Permissions facet lists the bindings set on it.
type Provider struct{ api API }

func New(api API) Provider { return Provider{api: api} }

func (p Provider) Service() model.ServiceType { return model.ServiceArtifactRegistry }

// ListPermissions is what List needs.
func (p Provider) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "artifactregistry.locations.list", "artifactregistry.repositories.list")
}

// List lists repositories in every Artifact Registry location. Locations that
// fail are ignored so restricted or unreachable regions never surface as
// errors; List fails only when every location fails or ctx ends.
func (p Provider) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("artifactregistry: no API")
	}
	locations, err := p.api.ListLocations(ctx, project)
	if err != nil {
		return nil, err
	}
	repos, locErrs := fanout.Regions(ctx, locations, locationConcurrency, func(ctx context.Context, loc string) ([]Repository, error) {
		return p.api.ListRepositories(ctx, project, loc)
	})
	if len(locErrs) > 0 {
		if len(locErrs) == len(locations) {
			return nil, fmt.Errorf("artifactregistry: every location failed: %w", locErrs[0])
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		slog.Debug("artifactregistry: some locations failed", "project", project, "failed", len(locErrs), "locations", len(locations), "err", locErrs[0])
	}
	slog.Debug("artifactregistry: listed repositories", "project", project, "locations", len(locations), "failed", len(locErrs), "repositories", len(repos))
	out := make([]model.Resource, 0, len(repos))
	for _, r := range repos {
		out = append(out, repositoryResource(project, r))
	}
	return out, nil
}

// ListRelated lists a repository's bindings and packages, a package's
// versions (and Docker signature tags), and a Docker digest's signatures.
func (p Provider) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	format := parent.Metadata[model.MetaArtifactFormat]
	switch {
	case parent.Service == model.ServiceArtifactRegistry && facetID == model.FacetPermissions:
	case parent.Service == model.ServiceArtifactRegistry && facetID == model.FacetPackages:
	case parent.Service == model.ServiceArtifactPackage && facetID == model.FacetVersions:
	case parent.Service == model.ServiceArtifactPackage && facetID == model.FacetSignatureTags && isDocker(format):
	case parent.Service == model.ServiceArtifactVersion && facetID == model.FacetSignatures:
	case parent.Service == model.ServiceArtifactVersion && facetID == model.FacetArtifacts:
	default:
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Location == "" || n.Kind != "repositories" {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("artifactregistry: no API")
	}

	if parent.Service == model.ServiceArtifactRegistry {
		if len(n.Rest) != 0 {
			return nil, false, nil
		}
		if facetID == model.FacetPermissions {
			bindings, err := p.api.GetRepositoryPolicy(ctx, n.Project, n.Location, n.Name)
			if err != nil {
				return nil, false, err
			}
			out := make([]model.Resource, 0, len(bindings))
			for _, b := range bindings {
				r := model.BindingResource(parent.ID, parent.ScopeID, b.Role, b.Members, b.Condition)
				r.ParentID = parent.ID
				r.FacetID = model.FacetPermissions
				out = append(out, r)
			}
			return out, false, nil
		}
		pkgs, truncated, err := p.api.ListPackages(ctx, n.Project, n.Location, n.Name, maxPackages)
		if err != nil {
			return nil, false, err
		}
		out := make([]model.Resource, 0, len(pkgs))
		for _, pkg := range pkgs {
			out = append(out, packageResource(n.Project, n.Location, parent.ID, format, pkg))
		}
		// The explorer keeps facet children in provider order, and the API's
		// order is arbitrary.
		model.SortNatural(out)
		return out, truncated, nil
	}

	if parent.Service == model.ServiceArtifactVersion {
		if len(n.Rest) != 4 || n.Rest[0] != "packages" || n.Rest[2] != "versions" {
			return nil, false, nil
		}
		if facetID == model.FacetArtifacts {
			artifacts, truncated, err := p.api.ListArtifacts(ctx, n.Project, n.Location, n.Name, n.Rest[1], n.Rest[3], maxVersions)
			if err != nil {
				return nil, false, err
			}
			pkgID := packageID(repositoryID(n.Project, n.Location, n.Name), n.Rest[1])
			out := make([]model.Resource, 0, len(artifacts))
			for _, a := range artifacts {
				r := versionResource(n.Project, n.Location, n.Name, n.Rest[1], pkgID, format, a)
				r.ParentID = parent.ID
				r.FacetID = model.FacetArtifacts
				out = append(out, r)
			}
			return out, truncated, nil
		}
		sigs, truncated, err := p.api.ListSignatures(ctx, n.Project, n.Location, n.Name, n.Rest[1], n.Rest[3], maxSignatures)
		if err != nil {
			return nil, false, err
		}
		sigs = slices.Clone(sigs)
		slices.SortStableFunc(sigs, compareSignatures)
		img := imageRef(n.Project, n.Location, n.Name, n.Rest[1], n.Rest[3])
		out := make([]model.Resource, 0, len(sigs))
		for _, sig := range sigs {
			out = append(out, signatureResource(n.Project, parent, img, sig))
		}
		return out, truncated, nil
	}

	if len(n.Rest) != 2 || n.Rest[0] != "packages" {
		return nil, false, nil
	}
	docker := isDocker(format)
	limit := maxVersions
	if docker {
		// Fetch extra so .sig tag artifacts do not crowd out image digests on the first page.
		limit = maxVersions * 2
	}
	versions, truncated, err := p.api.ListVersions(ctx, n.Project, n.Location, n.Name, n.Rest[1], limit)
	if err != nil {
		return nil, false, err
	}
	if docker {
		wantSig := facetID == model.FacetSignatureTags
		filtered := make([]Version, 0, len(versions))
		for _, v := range versions {
			if wantSig {
				if isSigTagVersion(v) {
					filtered = append(filtered, v)
				}
				continue
			}
			if isSigTagVersion(v) || v.SubjectDigest != "" || v.ParentDigest != "" {
				continue
			}
			filtered = append(filtered, v)
		}
		versions = filtered
	} else {
		versions = slices.Clone(versions)
	}
	slices.SortStableFunc(versions, compareVersions(docker))
	if len(versions) > maxVersions {
		versions = versions[:maxVersions]
		truncated = true
	} else if len(versions) == 0 {
		truncated = false
	}
	out := make([]model.Resource, 0, len(versions))
	for _, v := range versions {
		out = append(out, versionResource(n.Project, n.Location, n.Name, n.Rest[1], parent.ID, format, v))
	}
	return out, truncated, nil
}

// Vulnerabilities implements model.ResourceProvider for the vulnerability
// occurrences Artifact Analysis records against Docker image versions. It
// lists nothing at the top level; vulnerabilities are an image version's
// Vulnerabilities facet.
type Vulnerabilities struct{ api ScanAPI }

func NewVulnerabilities(api ScanAPI) Vulnerabilities { return Vulnerabilities{api: api} }

func (v Vulnerabilities) Service() model.ServiceType { return model.ServiceVulnerability }

func (v Vulnerabilities) List(ctx context.Context, _ string) ([]model.Resource, error) {
	return nil, ctx.Err()
}

// ListRelated lists the vulnerabilities found in a Docker image version.
func (v Vulnerabilities) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceArtifactVersion || facetID != model.FacetVulnerabilities {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Location == "" || n.Kind != "repositories" ||
		len(n.Rest) != 4 || n.Rest[0] != "packages" || n.Rest[2] != "versions" {
		return nil, false, nil
	}
	if v.api == nil {
		return nil, false, errors.New("artifactregistry: no scan API")
	}
	image := imageRef(n.Project, n.Location, n.Name, n.Rest[1], n.Rest[3])
	vulns, truncated, err := v.api.ListVulnerabilities(ctx, n.Project, "https://"+image, maxVulnerabilities)
	if err != nil {
		return nil, false, err
	}
	// The explorer keeps facet children in provider order, so the worst
	// findings come first here.
	vulns = slices.Clone(vulns)
	slices.SortStableFunc(vulns, compareVulnerabilities)
	out := make([]model.Resource, 0, len(vulns))
	for _, vuln := range vulns {
		out = append(out, vulnerabilityResource(n.Project, parent, image, vuln))
	}
	return out, truncated, nil
}

func repositoryID(project, location, name string) string {
	return "projects/" + project + "/locations/" + location + "/repositories/" + name
}

func packageID(repoID, pkg string) string {
	return repoID + "/packages/" + url.PathEscape(pkg)
}

// imageRef is a Docker image digest's address without a scheme:
// us-central1-docker.pkg.dev/p/repo/web/frontend@sha256:….
func imageRef(project, location, repo, pkg, digest string) string {
	return model.ArtifactRepositoryURI(project, location, "docker", repo) + "/" + pkg + "@" + digest
}

func isDocker(format string) bool { return format == "DOCKER" }

func repositoryResource(project string, r Repository) model.Resource {
	md := map[string]string{}
	set(md, model.MetaArtifactFormat, r.Format)
	set(md, "mode", modeWord(r.Mode))
	set(md, "registryUri", r.RegistryURI)
	set(md, "upstream", r.Upstream)
	set(md, "remoteSource", r.RemoteSource)
	if r.UpstreamUser != "" || r.UpstreamSecret != "" {
		c := r.UpstreamUser
		if r.UpstreamSecret != "" {
			c = strings.TrimSpace(c + " (password in " + r.UpstreamSecret + ")")
		}
		md["upstreamCredentials"] = c
	}
	if r.NoUpstreamValidate {
		md["upstreamValidation"] = "off"
	}
	if r.VirtualUpstreams > 0 {
		md["upstreams"] = strconv.Itoa(r.VirtualUpstreams)
	}
	set(md, "versionPolicy", r.MavenPolicy)
	if r.MavenOverwrites {
		md["snapshotOverwrites"] = "on"
	}
	md["size"] = humanSize(r.SizeBytes)
	set(md, "vulnerabilityScanning", scanWord(r.ScanState, r.ScanReason))
	if isDocker(r.Format) {
		md["immutableTags"] = onOff(r.ImmutableTags)
	}
	if r.CleanupPolicies > 0 {
		c := strconv.Itoa(r.CleanupPolicies)
		if r.CleanupDryRun {
			c += " (dry run)"
		}
		md["cleanupPolicies"] = c
	}
	set(md, "kmsKey", r.KMSKey)
	setTime(md, "updated", r.Updated)

	pkgs := model.Facet{ID: model.FacetPackages, Label: "Packages"}
	if isDocker(r.Format) {
		pkgs.Label = "Images"
	}
	var lists []model.List
	if len(r.UpstreamRules) > 0 {
		lists = append(lists, model.List{ID: "upstreams", Label: "Upstreams", Items: slices.Clone(r.UpstreamRules)})
	}
	if len(r.CleanupRules) > 0 {
		lists = append(lists, model.List{ID: "cleanupPolicies", Label: "Cleanup policies", Items: slices.Clone(r.CleanupRules)})
	}
	return model.Resource{
		ID:          repositoryID(project, r.Location, r.Name),
		Name:        r.Name,
		Service:     model.ServiceArtifactRegistry,
		ScopeID:     "projects/" + project,
		Location:    r.Location,
		Description: r.Description,
		Created:     r.Created,
		Labels:      maps.Clone(r.Labels),
		Metadata:    md,
		Fields: model.FieldsOf(md, model.MetaArtifactFormat, "mode", "registryUri", "upstream", "remoteSource",
			"upstreamCredentials", "upstreamValidation", "upstreams", "size", "vulnerabilityScanning", "immutableTags",
			"versionPolicy", "snapshotOverwrites", "cleanupPolicies", "kmsKey", "updated"),
		Lists:  lists,
		Facets: []model.Facet{{ID: model.FacetPermissions, Label: "Permissions", Optional: true}, pkgs},
	}
}

func packageResource(project, location, repoID, format string, p Package) model.Resource {
	md := map[string]string{}
	set(md, model.MetaArtifactFormat, format)
	if p.DisplayName != "" && p.DisplayName != p.Name {
		md["displayName"] = p.DisplayName
	}
	setTime(md, "updated", p.Updated)
	facets := []model.Facet{{ID: model.FacetVersions, Label: "Versions", Inline: true}}
	if isDocker(format) {
		facets = []model.Facet{
			{ID: model.FacetVersions, Label: "Digests"},
			{ID: model.FacetSignatureTags, Label: "Signature Tags", Optional: true},
		}
	}
	return model.Resource{
		ID:       packageID(repoID, p.Name),
		Name:     p.Name,
		Service:  model.ServiceArtifactPackage,
		ScopeID:  "projects/" + project,
		Location: location,
		Created:  p.Created,
		Metadata: md,
		Fields:   model.FieldsOf(md, model.MetaArtifactFormat, "displayName", "updated"),
		Facets:   facets,
		ParentID: repoID,
		FacetID:  model.FacetPackages,
	}
}

func versionResource(project, location, repo, pkg, pkgID, format string, v Version) model.Resource {
	md := map[string]string{}
	set(md, model.MetaArtifactFormat, format)
	set(md, "tags", strings.Join(v.Tags, ", "))
	docker := isDocker(format)
	sigTag := docker && isSigTagVersion(v)
	facetID := model.FacetVersions
	if docker {
		set(md, "digest", v.Name)
		if sigTag {
			facetID = model.FacetSignatureTags
			set(md, "signedDigest", signedDigestFromTags(v.Tags))
		}
		md["uri"] = imageRef(project, location, repo, pkg, v.Name)
	} else {
		set(md, "version", v.Name)
	}
	if v.SizeBytes > 0 {
		md["size"] = humanSize(v.SizeBytes)
	}
	set(md, "mediaType", v.MediaType)
	set(md, "artifactType", v.ArtifactType)
	set(md, "platform", v.Platform)
	setTime(md, "built", v.BuildTime)
	setTime(md, "updated", v.Updated)

	r := model.Resource{
		ID:          pkgID + "/versions/" + url.PathEscape(v.Name),
		Name:        versionName(v, docker),
		Service:     model.ServiceArtifactVersion,
		ScopeID:     "projects/" + project,
		Location:    location,
		Description: v.Description,
		Created:     v.Created,
		Metadata:    md,
		Fields: model.FieldsOf(md, model.MetaArtifactFormat, "tags", "digest", "signedDigest", "version", "uri", "size",
			"platform", "mediaType", "artifactType", "built", "updated"),
		ParentID: pkgID,
		FacetID:  facetID,
	}
	if docker && !sigTag && !isNonImageArtifact(v) {
		r.Facets = []model.Facet{
			{ID: model.FacetSignatures, Label: "Signatures", Optional: true},
			{ID: model.FacetVulnerabilities, Label: "Vulnerabilities", Optional: true},
			{ID: model.FacetArtifacts, Label: "Artifacts", Optional: true},
		}
	}
	return r
}

func isNonImageArtifact(v Version) bool {
	if v.SubjectDigest != "" || v.IsAttestation {
		return true
	}
	if v.ArtifactType == "" {
		return false
	}
	switch v.ArtifactType {
	case "application/vnd.oci.image.manifest.v1+json",
		"application/vnd.oci.image.config.v1+json",
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.docker.container.image.v1+json",
		"application/vnd.docker.distribution.manifest.list.v2+json":
		return false
	default:
		return true
	}
}

// shortDigest is how many hex digits of an untagged digest the explorer shows.
const shortDigest = 12

// shortenDigest truncates algo:hex digests to shortDigest hex characters.
func shortenDigest(d string) string {
	algo, hex, ok := strings.Cut(d, ":")
	if ok && len(hex) > shortDigest {
		return algo + ":" + hex[:shortDigest]
	}
	return d
}

// versionName is what the explorer shows: a Docker digest's tags, platform
// plus short digest for index children, or a short digest when untagged;
// other formats use the version id.
func versionName(v Version, docker bool) string {
	if !docker {
		return v.Name
	}
	if len(v.Tags) > 0 {
		return strings.Join(v.Tags, ", ")
	}
	if v.Platform != "" {
		return v.Platform + " (" + shortenDigest(v.Name) + ")"
	}
	return shortenDigest(v.Name)
}

func vulnerabilityResource(project string, parent model.Resource, image string, v Vulnerability) model.Resource {
	md := map[string]string{model.MetaArtifactImage: image}
	if v.Severity != "" && v.Severity != v.Effective {
		md["noteSeverity"] = v.Severity
	}
	if v.CVSS > 0 {
		s := strconv.FormatFloat(v.CVSS, 'f', 1, 64)
		if ver := cvssVersion(v.CVSSVersion); ver != "" {
			s += " (" + ver + ")"
		}
		md["cvss"] = s
	}
	md["fixAvailable"] = yesNo(v.FixAvailable)
	pkgs := make([]string, 0, len(v.Packages))
	for i, p := range v.Packages {
		pkgs = append(pkgs, affectedLine(p))
		if i == 0 {
			set(md, "package", p.Name)
			set(md, "packageType", p.Type)
			set(md, "installed", p.Installed)
			if p.Fixed != "" {
				md["fixedIn"] = p.Fixed
			}
			set(md, "path", p.Path)
		}
	}
	setTime(md, "published", v.Published)
	setTime(md, "updated", v.Updated)

	desc, details := v.Summary, v.Details
	if desc == "" {
		desc, details = details, ""
	}
	name := v.CVE
	if name == "" {
		name = lastSeg(v.ID)
	}
	var lists []model.List
	if details != "" && details != desc {
		lists = append(lists, model.List{ID: "details", Label: "Details", Items: []string{details}})
	}
	if len(pkgs) > 1 {
		lists = append(lists, model.List{ID: "packages", Label: "Affected packages", Items: pkgs})
	}
	if len(v.URLs) > 0 {
		lists = append(lists, model.List{ID: "references", Label: "References", Items: append([]string(nil), v.URLs...)})
	}
	return model.Resource{
		ID:          v.ID,
		Name:        name,
		Service:     model.ServiceVulnerability,
		ScopeID:     "projects/" + project,
		Location:    parent.Location,
		Status:      model.SeverityStatus(v.Effective),
		State:       v.Effective,
		Badge:       severityBadge(v.Effective),
		Description: desc,
		Created:     v.Created,
		Metadata:    md,
		Fields: model.FieldsOf(md, "cvss", "noteSeverity", "fixAvailable", "package", "packageType", "installed",
			"fixedIn", "path", model.MetaArtifactImage, "published", "updated"),
		Lists:    lists,
		ParentID: parent.ID,
		FacetID:  model.FacetVulnerabilities,
	}
}

// compareVersions orders versions by the name the explorer shows, reading
// numbers as numbers, so neighbouring releases sit together. Untagged Docker
// digests come after tagged ones, newest first, since their names say
// nothing.
func compareVersions(docker bool) func(a, b Version) int {
	return func(a, b Version) int {
		if docker {
			if ua, ub := len(a.Tags) == 0, len(b.Tags) == 0; ua != ub {
				if ua {
					return 1
				}
				return -1
			}
			if len(a.Tags) == 0 {
				if c := b.Created.Compare(a.Created); c != 0 {
					return c
				}
				return cmp.Compare(a.Name, b.Name)
			}
		}
		return model.CompareNatural(versionName(a, docker), versionName(b, docker))
	}
}

// compareVulnerabilities orders by severity (critical first), then CVSS
// score (highest first), then CVE id.
func compareVulnerabilities(a, b Vulnerability) int {
	if c := cmp.Compare(model.SeverityRank(a.Effective), model.SeverityRank(b.Effective)); c != 0 {
		return c
	}
	if c := cmp.Compare(b.CVSS, a.CVSS); c != 0 {
		return c
	}
	return cmp.Compare(a.CVE, b.CVE)
}

// affectedLine is "openssl 3.0.11-1 → 3.0.13-1 (OS)".
func affectedLine(p AffectedPackage) string {
	s := p.Name
	if p.Installed != "" {
		s += " " + p.Installed
	}
	if p.Fixed != "" {
		s += " → " + p.Fixed
	} else {
		s += " (no fix)"
	}
	if p.Type != "" {
		s += " [" + p.Type + "]"
	}
	return s
}

// severityBadge is the explorer badge for a severity; none when unknown.
func severityBadge(s string) string {
	if model.SeverityStatus(s) == model.StatusUnknown {
		return ""
	}
	return s
}

func modeWord(m string) string {
	switch m {
	case "STANDARD_REPOSITORY":
		return "Standard"
	case "REMOTE_REPOSITORY":
		return "Remote"
	case "VIRTUAL_REPOSITORY":
		return "Virtual"
	case "", "MODE_UNSPECIFIED":
		return ""
	default:
		return m
	}
}

func scanWord(state, reason string) string {
	var s string
	switch state {
	case "SCANNING_ACTIVE":
		s = "active"
	case "SCANNING_DISABLED":
		s = "disabled"
	case "SCANNING_UNSUPPORTED":
		s = "unsupported"
	case "", "ENABLEMENT_STATE_UNSPECIFIED":
		return ""
	default:
		s = state
	}
	if reason != "" {
		s += " (" + reason + ")"
	}
	return s
}

func cvssVersion(v string) string {
	switch v {
	case "CVSS_VERSION_2":
		return "v2"
	case "CVSS_VERSION_3":
		return "v3"
	case "CVSS_VERSION_4":
		return "v4"
	default:
		return ""
	}
}

// humanSize renders n bytes with binary units.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// set stores v under k when v is not empty.
func set(md map[string]string, k, v string) {
	if v != "" {
		md[k] = v
	}
}

func setTime(md map[string]string, k string, t time.Time) {
	if !t.IsZero() {
		md[k] = t.UTC().Format("2006-01-02 15:04 UTC")
	}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func lastSeg(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

var (
	_ model.ResourceProvider = Provider{}
	_ model.RelatedLister    = Provider{}
	_ model.ResourceProvider = Vulnerabilities{}
	_ model.RelatedLister    = Vulnerabilities{}
)
