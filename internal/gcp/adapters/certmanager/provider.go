package certmanager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tensleyconsulting/seiner/internal/gcp/fanout"
	"github.com/tensleyconsulting/seiner/internal/model"
)

const (
	// maxEntries caps a map's entries. The explorer shows "more not shown"
	// past it.
	maxEntries = 500
	// locationConcurrency bounds the per-location list calls, as in swp.
	locationConcurrency = 24
	// expiryWarning is how close to expiry a certificate turns yellow.
	expiryWarning = 30 * 24 * time.Hour
)

// Certificates implements model.ResourceProvider for certificates.
type Certificates struct {
	api API
	now func() time.Time
}

func NewCertificates(api API) Certificates { return Certificates{api: api, now: time.Now} }

func (p Certificates) Service() model.ServiceType { return model.ServiceCertificate }

// ListPermissions is what List needs.
func (p Certificates) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "certificatemanager.locations.list", "certificatemanager.certs.list")
}

// List lists certificates in every Certificate Manager location.
func (p Certificates) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	now := p.now()
	return list(ctx, p.api, scopeID, API.ListCertificates, func(project string, c Certificate) model.Resource {
		return certificateResource(project, c, now)
	})
}

// Maps implements model.ResourceProvider for certificate maps. A map
// expands to its entries.
type Maps struct{ api API }

func NewMaps(api API) Maps { return Maps{api: api} }

func (p Maps) Service() model.ServiceType { return model.ServiceCertificateMap }

// ListPermissions is what List needs. Entries are a facet and need
// certificatemanager.certmapentries.list only when opened.
func (p Maps) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "certificatemanager.locations.list", "certificatemanager.certmaps.list")
}

// List lists certificate maps in every Certificate Manager location.
func (p Maps) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	return list(ctx, p.api, scopeID, API.ListMaps, mapResource)
}

// ListRelated lists a map's entries.
func (p Maps) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceCertificateMap || facetID != model.FacetMapEntries {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Location == "" || n.Kind != "certificateMaps" || len(n.Rest) != 0 {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("certmanager: no API")
	}
	entries, truncated, err := p.api.ListMapEntries(ctx, n.Project, n.Location, n.Name, maxEntries)
	if err != nil {
		return nil, false, err
	}
	out := make([]model.Resource, 0, len(entries))
	for _, e := range entries {
		out = append(out, entryResource(parent, n.Project, n.Location, e))
	}
	return out, truncated, nil
}

// DNSAuthorizations implements model.ResourceProvider for DNS
// authorizations.
type DNSAuthorizations struct{ api API }

func NewDNSAuthorizations(api API) DNSAuthorizations { return DNSAuthorizations{api: api} }

func (p DNSAuthorizations) Service() model.ServiceType { return model.ServiceDNSAuthorization }

// ListPermissions is what List needs.
func (p DNSAuthorizations) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "certificatemanager.locations.list", "certificatemanager.dnsauthorizations.list")
}

// List lists DNS authorizations in every Certificate Manager location.
func (p DNSAuthorizations) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	return list(ctx, p.api, scopeID, API.ListDNSAuthorizations, dnsAuthResource)
}

// IssuanceConfigs implements model.ResourceProvider for certificate
// issuance configs.
type IssuanceConfigs struct{ api API }

func NewIssuanceConfigs(api API) IssuanceConfigs { return IssuanceConfigs{api: api} }

func (p IssuanceConfigs) Service() model.ServiceType { return model.ServiceIssuanceConfig }

// ListPermissions is what List needs.
func (p IssuanceConfigs) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "certificatemanager.locations.list", "certificatemanager.certissuanceconfigs.list")
}

// List lists issuance configs in every Certificate Manager location.
func (p IssuanceConfigs) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	return list(ctx, p.api, scopeID, API.ListIssuanceConfigs, issuanceResource)
}

// TrustConfigs implements model.ResourceProvider for trust configs.
type TrustConfigs struct{ api API }

func NewTrustConfigs(api API) TrustConfigs { return TrustConfigs{api: api} }

func (p TrustConfigs) Service() model.ServiceType { return model.ServiceTrustConfig }

// ListPermissions is what List needs.
func (p TrustConfigs) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "certificatemanager.locations.list", "certificatemanager.trustconfigs.list")
}

// List lists trust configs in every Certificate Manager location.
func (p TrustConfigs) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	return list(ctx, p.api, scopeID, API.ListTrustConfigs, trustResource)
}

// list fans out one call per location. Locations that fail are ignored so
// unsupported or restricted regions never surface as errors; list fails only
// when every location fails or ctx ends.
func list[T any](ctx context.Context, api API, scopeID string,
	fetch func(API, context.Context, string, string) ([]T, error),
	toResource func(string, T) model.Resource,
) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil, nil
	}
	if api == nil {
		return nil, errors.New("certmanager: no API")
	}
	locs, err := api.Locations(ctx, project)
	if err != nil {
		return nil, err
	}
	items, locErrs := fanout.Regions(ctx, locs, locationConcurrency, func(ctx context.Context, loc string) ([]T, error) {
		return fetch(api, ctx, project, loc)
	})
	if len(locErrs) > 0 {
		if len(locErrs) == len(locs) {
			return nil, fmt.Errorf("certmanager: every location failed: %w", locErrs[0])
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		slog.Debug("certmanager: some locations failed", "project", project, "failed", len(locErrs), "locations", len(locs), "err", locErrs[0])
	}
	out := make([]model.Resource, 0, len(items))
	for _, it := range items {
		out = append(out, toResource(project, it))
	}
	return out, nil
}

func locatedID(project, location, kind, name string) string {
	return "projects/" + project + "/locations/" + url.PathEscape(location) + "/" + kind + "/" + url.PathEscape(name)
}

func certificateResource(project string, c Certificate, now time.Time) model.Resource {
	md := map[string]string{}
	set(md, "type", typeWord(c.Type))
	set(md, "scope", c.Scope)
	set(md, "domains", strings.Join(c.Domains, ", "))
	set(md, "expires", timeWord(c.Expires))
	set(md, "issue", c.Issue)
	set(md, "dnsAuthorizations", strings.Join(shortNames(project, c.Location, c.DNSAuthorizations), ", "))
	set(md, "issuanceConfig", shortName(project, c.Location, c.IssuanceConfig))
	set(md, "identity", c.Identity)
	set(md, "updated", timeWord(c.Updated))
	status, badge := certStatus(c, now)
	var lists []model.List
	if len(c.SANs) > 0 {
		lists = append(lists, model.List{ID: "sans", Label: "Subject alternative names", Items: append([]string(nil), c.SANs...)})
	}
	if len(c.Authorizations) > 0 {
		items := make([]string, 0, len(c.Authorizations))
		for _, a := range c.Authorizations {
			s := a.Domain + ": " + a.State
			if a.Reason != "" {
				s += " (" + a.Reason + ")"
			}
			if a.Details != "" {
				s += " " + a.Details
			}
			items = append(items, s)
		}
		lists = append(lists, model.List{ID: "authorizations", Label: "Authorization", Items: items})
	}
	if len(c.UsedBy) > 0 {
		lists = append(lists, model.List{ID: "usedBy", Label: "Used by", Items: append([]string(nil), c.UsedBy...)})
	}
	return model.Resource{
		ID:          locatedID(project, c.Location, "certificates", c.Name),
		Name:        c.Name,
		Service:     model.ServiceCertificate,
		ScopeID:     "projects/" + project,
		Location:    c.Location,
		Status:      status,
		State:       c.State,
		Description: c.Description,
		Created:     c.Created,
		Labels:      maps.Clone(c.Labels),
		Metadata:    md,
		Fields: model.FieldsOf(md, "type", "scope", "domains", "expires", "issue", "dnsAuthorizations",
			"issuanceConfig", "identity", "updated"),
		Lists: lists,
		Badge: badge,
	}
}

// certStatus is the worse of the managed state and the expiry: expired is
// an error, and so is a failed managed certificate; expiring within 30
// days or still provisioning is a warning. The badge names an expiry
// problem.
func certStatus(c Certificate, now time.Time) (model.ResourceStatus, string) {
	status := model.StatusUnknown
	switch c.State {
	case "ACTIVE":
		status = model.StatusHealthy
	case "PROVISIONING":
		status = model.StatusWarning
	case "FAILED":
		status = model.StatusError
	default:
		status = model.StatusUnknown
	}
	if c.Expires.IsZero() {
		return status, ""
	}
	left := c.Expires.Sub(now)
	switch {
	case left <= 0:
		return model.StatusError, "EXPIRED"
	case left <= expiryWarning:
		if status != model.StatusError {
			status = model.StatusWarning
		}
		return status, strconv.Itoa(int(left.Hours()/24)) + "d"
	}
	return status, ""
}

func mapResource(project string, m Map) model.Resource {
	md := map[string]string{}
	set(md, "targets", strings.Join(lastSegs(m.Targets), ", "))
	set(md, "addresses", strings.Join(m.Addresses, ", "))
	set(md, "updated", timeWord(m.Updated))
	return model.Resource{
		ID:          locatedID(project, m.Location, "certificateMaps", m.Name),
		Name:        m.Name,
		Service:     model.ServiceCertificateMap,
		ScopeID:     "projects/" + project,
		Location:    m.Location,
		Description: m.Description,
		Created:     m.Created,
		Labels:      maps.Clone(m.Labels),
		Metadata:    md,
		Fields:      model.FieldsOf(md, "targets", "addresses", "updated"),
		Facets:      []model.Facet{{ID: model.FacetMapEntries, Label: "Entries", Inline: true}},
	}
}

// entryResource is one entry under parent, its map. It is named after its
// hostname (or PRIMARY), which is what the entry serves.
func entryResource(parent model.Resource, project, location string, e MapEntry) model.Resource {
	md := map[string]string{"entry": e.Name}
	set(md, "hostname", e.Hostname)
	set(md, "matcher", e.Matcher)
	set(md, "certificates", strings.Join(shortNames(project, location, e.Certificates), ", "))
	set(md, "updated", timeWord(e.Updated))
	name := e.Hostname
	if name == "" {
		name = e.Matcher
	}
	if name == "" {
		name = e.Name
	}
	status := model.StatusUnknown
	switch e.State {
	case "ACTIVE":
		status = model.StatusHealthy
	case "PENDING":
		status = model.StatusWarning
	default:
		status = model.StatusUnknown
	}
	return model.Resource{
		ID:          parent.ID + "/certificateMapEntries/" + url.PathEscape(e.Name),
		Name:        name,
		Service:     model.ServiceCertMapEntry,
		ScopeID:     parent.ScopeID,
		Location:    location,
		Status:      status,
		State:       e.State,
		Description: e.Description,
		Created:     e.Created,
		Labels:      maps.Clone(e.Labels),
		Metadata:    md,
		Fields:      model.FieldsOf(md, "hostname", "matcher", "certificates", "entry", "updated"),
		Badge:       e.Matcher,
		ParentID:    parent.ID,
		FacetID:     model.FacetMapEntries,
	}
}

func dnsAuthResource(project string, d DNSAuthorization) model.Resource {
	md := map[string]string{}
	set(md, "domain", d.Domain)
	set(md, "type", strings.ReplaceAll(strings.ToLower(d.Type), "_", " "))
	set(md, "recordName", d.RecordName)
	set(md, "recordType", d.RecordType)
	set(md, "recordData", d.RecordData)
	set(md, "updated", timeWord(d.Updated))
	return model.Resource{
		ID:          locatedID(project, d.Location, "dnsAuthorizations", d.Name),
		Name:        d.Name,
		Service:     model.ServiceDNSAuthorization,
		ScopeID:     "projects/" + project,
		Location:    d.Location,
		Description: d.Description,
		Created:     d.Created,
		Labels:      maps.Clone(d.Labels),
		Metadata:    md,
		Fields:      model.FieldsOf(md, "domain", "type", "recordName", "recordType", "recordData", "updated"),
	}
}

func issuanceResource(project string, c IssuanceConfig) model.Resource {
	md := map[string]string{}
	set(md, "caPool", c.CAPool)
	set(md, "keyAlgorithm", c.KeyAlgorithm)
	set(md, "lifetime", durationWord(c.Lifetime))
	if c.RotationWindow > 0 {
		md["rotationWindow"] = strconv.FormatInt(c.RotationWindow, 10) + "%"
	}
	set(md, "updated", timeWord(c.Updated))
	return model.Resource{
		ID:          locatedID(project, c.Location, "certificateIssuanceConfigs", c.Name),
		Name:        c.Name,
		Service:     model.ServiceIssuanceConfig,
		ScopeID:     "projects/" + project,
		Location:    c.Location,
		Description: c.Description,
		Created:     c.Created,
		Labels:      maps.Clone(c.Labels),
		Metadata:    md,
		Fields:      model.FieldsOf(md, "caPool", "keyAlgorithm", "lifetime", "rotationWindow", "updated"),
	}
}

func trustResource(project string, t TrustConfig) model.Resource {
	md := map[string]string{
		"trustAnchors":  strconv.Itoa(t.TrustAnchors),
		"intermediates": strconv.Itoa(t.Intermediates),
	}
	if t.Allowlisted > 0 {
		md["allowlisted"] = strconv.Itoa(t.Allowlisted)
	}
	set(md, "spiffeTrustDomains", strings.Join(t.SPIFFEDomains, ", "))
	set(md, "updated", timeWord(t.Updated))
	return model.Resource{
		ID:          locatedID(project, t.Location, "trustConfigs", t.Name),
		Name:        t.Name,
		Service:     model.ServiceTrustConfig,
		ScopeID:     "projects/" + project,
		Location:    t.Location,
		Description: t.Description,
		Created:     t.Created,
		Labels:      maps.Clone(t.Labels),
		Metadata:    md,
		Fields:      model.FieldsOf(md, "trustAnchors", "intermediates", "allowlisted", "spiffeTrustDomains", "updated"),
	}
}

// typeWord reads MANAGED as "Google-managed", as the Console does.
func typeWord(t string) string {
	switch t {
	case "MANAGED":
		return "Google-managed"
	case "SELF_MANAGED":
		return "self-managed"
	case "MANAGED_IDENTITY":
		return "managed identity"
	}
	return t
}

// durationWord reads an API duration ("2592000s") as "720h0m0s"; other
// strings stay as they are.
func durationWord(s string) string {
	if d, err := time.ParseDuration(s); err == nil {
		return d.String()
	}
	return s
}

func timeWord(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

// shortName drops the project/location prefix from a sibling resource's
// full name. Names from another project or location stay whole.
func shortName(project, location, name string) string {
	n, ok := model.ParseName(name)
	if !ok || n.Project != project || n.Location != location || len(n.Rest) != 0 {
		return name
	}
	return n.Name
}

func shortNames(project, location string, names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, shortName(project, location, n))
	}
	return out
}

func lastSegs(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if i := strings.LastIndexByte(n, '/'); i >= 0 {
			n = n[i+1:]
		}
		out = append(out, n)
	}
	return out
}

// set stores v under k when v is not empty.
func set(md map[string]string, k, v string) {
	if v != "" {
		md[k] = v
	}
}

var (
	_ model.ResourceProvider   = Certificates{}
	_ model.PermissionDeclarer = Certificates{}
	_ model.ResourceProvider   = Maps{}
	_ model.PermissionDeclarer = Maps{}
	_ model.RelatedLister      = Maps{}
	_ model.ResourceProvider   = DNSAuthorizations{}
	_ model.PermissionDeclarer = DNSAuthorizations{}
	_ model.ResourceProvider   = IssuanceConfigs{}
	_ model.PermissionDeclarer = IssuanceConfigs{}
	_ model.ResourceProvider   = TrustConfigs{}
	_ model.PermissionDeclarer = TrustConfigs{}
)
