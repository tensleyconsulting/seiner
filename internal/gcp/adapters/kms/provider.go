package kms

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tensleyconsulting/seiner/internal/gcp/display"
	"github.com/tensleyconsulting/seiner/internal/gcp/fanout"
	"github.com/tensleyconsulting/seiner/internal/model"
)

const (
	// maxKeys caps a key ring's keys and import jobs. The explorer shows
	// "more not shown" past it.
	maxKeys = 500
	// maxVersions caps a key's versions.
	maxVersions = 500
	// versionConcurrency bounds the version lists made to summarize keys
	// without a primary.
	versionConcurrency = 8
	// maxSummarized caps how many keys without a primary a ring summarizes,
	// so a ring of hundreds of signing keys opens with one call, not
	// hundreds; past it, keys list without a summary.
	maxSummarized = 20
	// summaryVersions caps the versions read to summarize one key.
	summaryVersions = 100
	// locationConcurrency bounds the per-location key ring list calls. A
	// project has a few dozen locations; 24 lists them in about two rounds
	// instead of five or more, without a large burst against read quota.
	locationConcurrency = 24
)

// Provider implements model.ResourceProvider for Cloud KMS key rings. A key
// ring expands to its crypto keys.
type Provider struct{ api API }

func New(api API) Provider { return Provider{api: api} }

func (p Provider) Service() model.ServiceType { return model.ServiceKMS }

// ListPermissions is what List needs.
func (p Provider) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "cloudkms.locations.list", "cloudkms.keyRings.list")
}

// List lists key rings in every KMS location. Locations that fail are
// ignored so restricted or unreachable regions never surface as errors; List
// fails only when every location fails or ctx ends.
func (p Provider) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("kms: no API")
	}
	locations, err := p.api.ListLocations(ctx, project)
	if err != nil {
		return nil, err
	}
	rings, locErrs := fanout.Regions(ctx, locations, locationConcurrency, func(ctx context.Context, loc string) ([]KeyRing, error) {
		return p.api.ListKeyRings(ctx, project, loc)
	})
	if len(locErrs) > 0 {
		if len(locErrs) == len(locations) {
			return nil, fmt.Errorf("kms: every location failed: %w", locErrs[0])
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		slog.Debug("kms: some locations failed", "project", project, "failed", len(locErrs), "locations", len(locations), "err", locErrs[0])
	}
	slog.Debug("kms: listed rings", "project", project, "locations", len(locations), "failed", len(locErrs), "rings", len(rings))
	out := make([]model.Resource, 0, len(rings))
	for _, r := range rings {
		out = append(out, ringResource(project, r))
	}
	return out, nil
}

// ListRelated lists a key ring's crypto keys, import jobs, and IAM
// bindings, a key's versions and IAM bindings, and an HSM version's
// attestation.
func (p Provider) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service == model.ServiceKMSKeyVersion && facetID == model.FacetAttestation {
		out, err := p.attestation(ctx, parent)
		return out, false, err
	}
	var ring bool
	switch {
	case parent.Service == model.ServiceKMS && (facetID == model.FacetKeys || facetID == model.FacetImportJobs || facetID == model.FacetPermissions):
		ring = true
	case parent.Service == model.ServiceKMSKey && (facetID == model.FacetVersions || facetID == model.FacetPermissions):
	default:
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Location == "" || n.Kind != "keyRings" {
		return nil, false, nil
	}
	var key string
	switch {
	case ring && len(n.Rest) == 0:
	case !ring && len(n.Rest) == 2 && n.Rest[0] == "cryptoKeys":
		key = n.Rest[1]
	default:
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("kms: no API")
	}
	switch facetID {
	case model.FacetKeys:
		return p.keys(ctx, n, parent)
	case model.FacetImportJobs:
		jobs, truncated, err := p.api.ListImportJobs(ctx, n.Project, n.Location, n.Name, maxKeys)
		if err != nil {
			return nil, false, err
		}
		out := make([]model.Resource, 0, len(jobs))
		for _, j := range jobs {
			r := importJobResource(n.Location, parent.ID, j)
			r.ScopeID = parent.ScopeID
			out = append(out, r)
		}
		return out, truncated, nil
	case model.FacetVersions:
		versions, truncated, err := p.api.ListVersions(ctx, n.Project, n.Location, n.Name, key, maxVersions)
		if err != nil {
			return nil, false, err
		}
		sortNewestFirst(versions)
		primary := parent.Metadata["primaryVersion"]
		out := make([]model.Resource, 0, len(versions))
		for _, v := range versions {
			r := versionResource(n.Location, parent.ID, v, v.Name == primary)
			r.ScopeID = parent.ScopeID
			out = append(out, r)
		}
		return out, truncated, nil
	default: // model.FacetPermissions
		bindings, err := p.api.GetIAMPolicy(ctx, n.Project, n.Location, n.Name, key)
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
}

// attestation gets an HSM version's attestation and lists it as one row,
// which copies the attestation and all its certificates, and a row per
// certificate chain. seiner does not verify the attestation.
func (p Provider) attestation(ctx context.Context, parent model.Resource) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Location == "" || n.Kind != "keyRings" ||
		len(n.Rest) != 4 || n.Rest[0] != "cryptoKeys" || n.Rest[2] != "cryptoKeyVersions" {
		return nil, nil
	}
	version, err := url.PathUnescape(n.Rest[3])
	if err != nil {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("kms: no API")
	}
	a, ok, err := p.api.GetAttestation(ctx, n.Project, n.Location, n.Name, n.Rest[1], version)
	if err != nil || !ok {
		return nil, err
	}
	certs := len(a.Cavium) + len(a.GoogleCard) + len(a.GooglePartition)
	slog.Debug("kms: got attestation", "version", parent.ID, "bytes", len(a.Content), "certs", certs)
	out := []model.Resource{attestationResource(parent, a)}
	now := time.Now()
	for _, c := range chains(a) {
		if len(c.pem) > 0 {
			out = append(out, chainResource(parent, c, now))
		}
	}
	return out, nil
}

// chain is one of an attestation's certificate chains.
type chain struct {
	id, name string
	pem      []string
}

// chains orders the chains as gcloud's get-certificate-chain does.
func chains(a Attestation) []chain {
	return []chain{
		{"cavium", "Cavium", a.Cavium},
		{"google-card", "Google card", a.GoogleCard},
		{"google-partition", "Google partition", a.GooglePartition},
	}
}

func attestationResource(parent model.Resource, a Attestation) model.Resource {
	md := map[string]string{}
	set(md, "format", a.Format)
	md["size"] = strconv.Itoa(len(a.Content)) + " bytes"
	var pems []string
	var counts []string
	for _, c := range chains(a) {
		pems = append(pems, c.pem...)
		if len(c.pem) > 0 {
			counts = append(counts, fmt.Sprintf("%s %d", c.name, len(c.pem)))
		}
	}
	set(md, "certificates", strings.Join(counts, ", "))
	md["verified"] = "no (seiner does not verify attestations)"
	copies := []model.Copy{{Title: "Attestation", Label: "attestation", Text: base64.StdEncoding.EncodeToString(a.Content)}}
	if len(pems) > 0 {
		copies = append(copies, model.Copy{Title: "Cert chains", Label: "cert chains", Text: joinPEM(pems)})
	}
	return model.Resource{
		ID:       parent.ID + "/attestation",
		Name:     "attestation",
		Service:  model.ServiceKMSAttestation,
		ScopeID:  parent.ScopeID,
		Location: parent.Location,
		Status:   model.StatusUnknown,
		Description: "Signed by the HSM when it made this version. Copy it as base64 and the chains as PEM " +
			"to check them with Google's verify_attestation_chains.py.",
		Metadata: md,
		Fields:   model.FieldsOf(md, "format", "size", "certificates", "verified"),
		Badge:    attestationBadge(a.Format),
		ParentID: parent.ID,
		FacetID:  model.FacetAttestation,
		Copies:   copies,
	}
}

// attestationBadge shortens CAVIUM_V2_COMPRESSED to V2.
func attestationBadge(format string) string {
	f := strings.TrimSuffix(strings.TrimPrefix(format, "CAVIUM_"), "_COMPRESSED")
	if f == "" || f == "ATTESTATION_FORMAT_UNSPECIFIED" {
		return ""
	}
	return f
}

// chainResource lists a chain's certificates, leaf first. A certificate
// past its expiry makes the chain a warning; that is all seiner checks.
func chainResource(parent model.Resource, c chain, now time.Time) model.Resource {
	md := map[string]string{"certificates": strconv.Itoa(len(c.pem))}
	var lists []model.List
	var expires time.Time
	expired := false
	for i, p := range c.pem {
		l := model.List{ID: "cert-" + strconv.Itoa(i+1), Label: "Certificate " + strconv.Itoa(i+1)}
		cert, err := parseCert(p)
		if err != nil {
			slog.Debug("kms: attestation certificate not parsed", "chain", c.id, "index", i, "err", err)
			l.Items = []string{"not parsed: " + err.Error()}
			lists = append(lists, l)
			continue
		}
		l.Items = []string{
			"Subject " + cert.Subject.String(),
			"Issuer  " + cert.Issuer.String(),
			"Serial  " + cert.SerialNumber.Text(16),
			"Valid   " + display.Stamp(cert.NotBefore) + " to " + display.Stamp(cert.NotAfter),
		}
		lists = append(lists, l)
		if expires.IsZero() || cert.NotAfter.Before(expires) {
			expires = cert.NotAfter
		}
		if now.After(cert.NotAfter) {
			expired = true
		}
	}
	set(md, "expires", display.Stamp(expires))
	status, badge := model.StatusUnknown, ""
	if expired {
		status, badge = model.StatusWarning, "EXPIRED"
		md["expired"] = "yes"
	}
	return model.Resource{
		ID:       parent.ID + "/attestation/" + c.id,
		Name:     c.name,
		Service:  model.ServiceKMSCertChain,
		ScopeID:  parent.ScopeID,
		Location: parent.Location,
		Status:   status,
		Metadata: md,
		Fields:   model.FieldsOf(md, "certificates", "expires", "expired"),
		Lists:    lists,
		Badge:    badge,
		ParentID: parent.ID,
		FacetID:  model.FacetAttestation,
		Copies:   []model.Copy{{Title: "PEM", Label: c.name + " chain", Text: joinPEM(c.pem)}},
	}
}

func parseCert(s string) (*x509.Certificate, error) {
	b, _ := pem.Decode([]byte(s))
	if b == nil {
		return nil, errors.New("no PEM block")
	}
	return x509.ParseCertificate(b.Bytes)
}

// joinPEM concatenates certificates, each ending in a newline, as a PEM
// bundle file does.
func joinPEM(pems []string) string {
	var b strings.Builder
	for _, p := range pems {
		b.WriteString(strings.TrimRight(p, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

// keys lists a ring's keys. A key without a primary (asymmetric, MAC, raw)
// has no single state, so up to maxSummarized such keys also have their
// versions listed and summarized; past that, none are, so a large ring
// still opens with one call. A key whose versions cannot be listed is
// logged and keeps no summary, but a context that ended fails the list,
// so a half-summarized result is not cached.
func (p Provider) keys(ctx context.Context, n model.ParsedName, parent model.Resource) ([]model.Resource, bool, error) {
	keys, truncated, err := p.api.ListKeys(ctx, n.Project, n.Location, n.Name, maxKeys)
	if err != nil {
		return nil, false, err
	}
	var noPrimary []string
	for _, k := range keys {
		if k.Primary == "" {
			noPrimary = append(noPrimary, k.Name)
		}
	}
	if len(noPrimary) > maxSummarized {
		slog.Debug("kms: too many keys without a primary to summarize", "ring", parent.ID, "keys", len(noPrimary))
		noPrimary = nil
	}
	type summary struct {
		key       string
		versions  []KeyVersion
		truncated bool
	}
	sums, failed := fanout.Regions(ctx, noPrimary, versionConcurrency, func(ctx context.Context, key string) ([]summary, error) {
		vs, more, err := p.api.ListVersions(ctx, n.Project, n.Location, n.Name, key, summaryVersions)
		if err != nil {
			return nil, err
		}
		return []summary{{key: key, versions: vs, truncated: more}}, nil
	})
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	for _, f := range failed {
		slog.Warn("kms: key versions failed", "ring", parent.ID, "key", f.Location, "err", f.Err)
	}
	sumOf := make(map[string]summary, len(sums))
	for _, s := range sums {
		sumOf[s.key] = s
	}
	out := make([]model.Resource, 0, len(keys))
	for _, k := range keys {
		r := keyResource(n.Location, parent.ID, k)
		if s, ok := sumOf[k.Name]; ok {
			withVersions(&r, s.versions, s.truncated)
		}
		r.ScopeID = parent.ScopeID
		out = append(out, r)
	}
	return out, truncated, nil
}

func ringID(project, location, ring string) string {
	return "projects/" + project + "/locations/" + url.PathEscape(location) + "/keyRings/" + url.PathEscape(ring)
}

func ringResource(project string, r KeyRing) model.Resource {
	return model.Resource{
		ID:       ringID(project, r.Location, r.Name),
		Name:     r.Name,
		Service:  model.ServiceKMS,
		ScopeID:  "projects/" + project,
		Location: r.Location,
		Created:  r.Created,
		Metadata: map[string]string{},
		Facets: []model.Facet{
			{ID: model.FacetKeys, Label: "Keys"},
			{ID: model.FacetImportJobs, Label: "Import Jobs"},
			{ID: model.FacetPermissions, Label: "Permissions"},
		},
	}
}

// keyFields is the order of a key's fields.
var keyFields = []string{"purpose", "algorithm", "protectionLevel", "primaryVersion", "versions", "template",
	"rotation", "nextRotation", "destroyDelay", "importOnly", "backend", "accessReasons"}

func keyResource(location, parentID string, k Key) model.Resource {
	md := map[string]string{}
	set(md, "purpose", k.Purpose)
	set(md, "algorithm", k.Algorithm)
	set(md, "protectionLevel", k.ProtectionLevel)
	set(md, "primaryVersion", k.Primary)
	set(md, "rotation", duration(k.RotationPeriod))
	set(md, "nextRotation", display.Stamp(k.NextRotation))
	set(md, "destroyDelay", duration(k.DestroyDelay))
	if k.ImportOnly {
		md["importOnly"] = "on"
	}
	set(md, "backend", k.Backend)
	if k.TemplateAlgorithm != "" && (k.TemplateAlgorithm != k.Algorithm || k.TemplateProtection != k.ProtectionLevel) {
		md["template"] = strings.TrimSuffix(k.TemplateAlgorithm+" "+k.TemplateProtection, " ")
	}
	var lists []model.List
	if len(k.AccessReasons) > 0 {
		md["accessReasons"] = strconv.Itoa(len(k.AccessReasons))
		lists = []model.List{{ID: "accessReasons", Label: "Allowed access reasons", Items: slices.Clone(k.AccessReasons)}}
	}
	return model.Resource{
		ID:       parentID + "/cryptoKeys/" + url.PathEscape(k.Name),
		Name:     k.Name,
		Service:  model.ServiceKMSKey,
		Location: location,
		Status:   keyStatus(k.PrimaryState),
		State:    k.PrimaryState,
		Created:  k.Created,
		Labels:   maps.Clone(k.Labels),
		Metadata: md,
		Fields:   model.FieldsOf(md, keyFields...),
		Lists:    lists,
		Badge:    badge(k.Purpose),
		ParentID: parentID,
		FacetID:  model.FacetKeys,
		Facets: []model.Facet{
			{ID: model.FacetVersions, Label: "Versions"},
			{ID: model.FacetPermissions, Label: "Permissions"},
		},
	}
}

// withVersions summarizes the versions of a key without a primary: a count
// per state, and a status that is healthy while any version is enabled and
// otherwise the worst of the rest. When the list was cut off (truncated),
// the counts say so, and a key with no enabled version among those read
// has no status, since an enabled one may be among the rest.
func withVersions(r *model.Resource, vs []KeyVersion, truncated bool) {
	counts := map[string]int{}
	var order []string
	status := model.StatusUnknown
	enabled := false
	for _, v := range vs {
		if counts[v.State] == 0 {
			order = append(order, v.State)
		}
		counts[v.State]++
		if v.State == "ENABLED" {
			enabled = true
		}
		if s := keyStatus(v.State); worse(s, status) {
			status = s
		}
	}
	slices.SortFunc(order, func(a, b string) int { return stateRank(a) - stateRank(b) })
	parts := make([]string, 0, len(order))
	for _, st := range order {
		parts = append(parts, strconv.Itoa(counts[st])+" "+strings.ToLower(strings.ReplaceAll(st, "_", " ")))
	}
	if len(parts) == 0 {
		parts = []string{"none"}
	}
	summary := strings.Join(parts, ", ")
	if truncated {
		summary += " (first " + strconv.Itoa(len(vs)) + " versions)"
	}
	r.Metadata["versions"] = summary
	switch {
	case enabled:
		status = model.StatusHealthy
	case truncated:
		status = model.StatusUnknown
	}
	r.Status = status
	r.Fields = model.FieldsOf(r.Metadata, keyFields...)
}

// stateRank orders version states in a summary: usable first, then
// pending, failed, and gone.
func stateRank(s string) int {
	switch s {
	case "ENABLED":
		return 0
	case "DISABLED":
		return 1
	case "PENDING_GENERATION", "PENDING_IMPORT":
		return 2
	case "DESTROY_SCHEDULED", "PENDING_EXTERNAL_DESTRUCTION":
		return 3
	case "GENERATION_FAILED", "IMPORT_FAILED", "EXTERNAL_DESTRUCTION_FAILED":
		return 4
	case "DESTROYED":
		return 5
	default:
		return 6
	}
}

// worse reports whether a is a worse status than b.
func worse(a, b model.ResourceStatus) bool {
	rank := func(s model.ResourceStatus) int {
		switch s {
		case model.StatusError:
			return 3
		case model.StatusWarning:
			return 2
		case model.StatusHealthy:
			return 1
		default:
			return 0
		}
	}
	return rank(a) > rank(b)
}

// sortNewestFirst orders versions by number, highest first. Version names
// are numbers; any that are not sort after them by name.
func sortNewestFirst(vs []KeyVersion) {
	slices.SortStableFunc(vs, func(a, b KeyVersion) int {
		an, aerr := strconv.Atoi(a.Name)
		bn, berr := strconv.Atoi(b.Name)
		switch {
		case aerr == nil && berr == nil:
			return bn - an
		case aerr == nil:
			return -1
		case berr == nil:
			return 1
		default:
			return strings.Compare(a.Name, b.Name)
		}
	})
}

func versionResource(location, keyID string, v KeyVersion, primary bool) model.Resource {
	md := map[string]string{}
	set(md, "algorithm", v.Algorithm)
	set(md, "protectionLevel", v.ProtectionLevel)
	set(md, "generated", display.Stamp(v.Generated))
	set(md, "imported", display.Stamp(v.Imported))
	set(md, "importJob", v.ImportJob)
	set(md, "destroyScheduled", display.Stamp(v.DestroyScheduled))
	set(md, "destroyed", display.Stamp(v.Destroyed))
	set(md, "failure", v.FailureReason)
	set(md, "externalKey", v.ExternalKeyURI)
	if v.ReimportEligible {
		md["reimportEligible"] = "yes"
	}
	badge := v.State
	if primary {
		md["primary"] = "yes"
		badge = "PRIMARY"
	}
	// An HSM version expands to its attestation, read only then.
	var facets []model.Facet
	if v.ProtectionLevel == "HSM" {
		facets = []model.Facet{{ID: model.FacetAttestation, Label: "Attestation", Inline: true}}
	}
	return model.Resource{
		ID:       keyID + "/cryptoKeyVersions/" + url.PathEscape(v.Name),
		Name:     v.Name,
		Service:  model.ServiceKMSKeyVersion,
		Location: location,
		Status:   keyStatus(v.State),
		State:    v.State,
		Created:  v.Created,
		Metadata: md,
		Fields: model.FieldsOf(md, "primary", "algorithm", "protectionLevel", "generated", "imported", "importJob",
			"destroyScheduled", "destroyed", "failure", "externalKey", "reimportEligible"),
		Badge:    badge,
		Facets:   facets,
		ParentID: keyID,
		FacetID:  model.FacetVersions,
	}
}

func importJobResource(location, ringID string, j ImportJob) model.Resource {
	md := map[string]string{}
	set(md, "method", j.Method)
	set(md, "protectionLevel", j.ProtectionLevel)
	set(md, "backend", j.Backend)
	set(md, "generated", display.Stamp(j.Generated))
	set(md, "expires", display.Stamp(j.Expires))
	set(md, "expired", display.Stamp(j.Expired))
	return model.Resource{
		ID:       ringID + "/importJobs/" + url.PathEscape(j.Name),
		Name:     j.Name,
		Service:  model.ServiceKMSImportJob,
		Location: location,
		Status:   importJobStatus(j.State),
		State:    j.State,
		Created:  j.Created,
		Metadata: md,
		Fields:   model.FieldsOf(md, "method", "protectionLevel", "backend", "generated", "expires", "expired"),
		Badge:    j.State,
		ParentID: ringID,
		FacetID:  model.FacetImportJobs,
	}
}

// importJobStatus: an active job can take imports. Jobs expire after three
// days by design, so an expired one has no status.
func importJobStatus(state string) model.ResourceStatus {
	switch state {
	case "ACTIVE":
		return model.StatusHealthy
	case "PENDING_GENERATION":
		return model.StatusWarning
	default: // EXPIRED, or unknown
		return model.StatusUnknown
	}
}

// keyStatus maps the primary version's state. Keys without a primary
// (asymmetric and MAC keys) have no single state.
func keyStatus(state string) model.ResourceStatus {
	switch state {
	case "ENABLED":
		return model.StatusHealthy
	case "DISABLED", "DESTROY_SCHEDULED", "PENDING_GENERATION", "PENDING_IMPORT", "PENDING_EXTERNAL_DESTRUCTION":
		return model.StatusWarning
	case "GENERATION_FAILED", "IMPORT_FAILED", "EXTERNAL_DESTRUCTION_FAILED":
		return model.StatusError
	default: // DESTROYED, or unknown
		return model.StatusUnknown
	}
}

// badge is a short tag for a key's purpose.
func badge(purpose string) string {
	switch purpose {
	case "ENCRYPT_DECRYPT":
		return "SYM"
	case "ASYMMETRIC_SIGN":
		return "SIGN"
	case "ASYMMETRIC_DECRYPT":
		return "ASYM"
	case "RAW_ENCRYPT_DECRYPT":
		return "RAW"
	case "MAC":
		return "MAC"
	default:
		return ""
	}
}

// duration turns the API's "7776000s" into "90 days" (or "36h0m0s" when not
// whole days); other strings are returned as-is.
func duration(s string) string {
	if s == "" {
		return ""
	}
	secs, err := strconv.ParseFloat(strings.TrimSuffix(s, "s"), 64)
	if err != nil || !strings.HasSuffix(s, "s") {
		return s
	}
	d := time.Duration(secs * float64(time.Second))
	if d > 0 && d%(24*time.Hour) == 0 {
		days := int(d / (24 * time.Hour))
		if days == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", days)
	}
	return d.String()
}

// set stores v under k when v is not empty.
func set(md map[string]string, k, v string) {
	if v != "" {
		md[k] = v
	}
}

var (
	_ model.ResourceProvider = Provider{}
	_ model.RelatedLister    = Provider{}
)
