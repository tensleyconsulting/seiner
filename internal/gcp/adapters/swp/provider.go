package swp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/tensleyconsulting/seiner/internal/gcp/fanout"
	"github.com/tensleyconsulting/seiner/internal/model"
)

const (
	// maxRules caps a policy's rules. The explorer shows "more not shown"
	// past it.
	maxRules = 500
	// locationConcurrency bounds the per-location list calls, as in kms.
	locationConcurrency = 24
)

// WebProxies implements model.ResourceProvider for Secure Web Proxy
// gateways. A proxy expands to the rules of the policy it enforces.
type WebProxies struct{ api API }

func NewWebProxies(api API) WebProxies { return WebProxies{api: api} }

func (p WebProxies) Service() model.ServiceType { return model.ServiceWebProxy }

// ListPermissions is what List needs.
func (p WebProxies) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "networkservices.locations.list", "networkservices.gateways.list")
}

// List lists Secure Web Proxy gateways in every Network Services location.
func (p WebProxies) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	return list(ctx, p.api, scopeID, API.GatewayLocations, API.ListGateways, gatewayResource)
}

// ListRelated lists the rules of the proxy's policy.
func (p WebProxies) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceWebProxy || facetID != model.FacetRules {
		return nil, false, nil
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Location == "" || n.Kind != "gateways" || len(n.Rest) != 0 {
		return nil, false, nil
	}
	policy := policyName(n.Project, n.Location, parent.Metadata["policy"])
	if policy == "" {
		return nil, false, nil
	}
	return rules(ctx, p.api, parent, policy)
}

// Policies implements model.ResourceProvider for gateway security policies.
// A policy expands to its rules.
type Policies struct{ api API }

func NewPolicies(api API) Policies { return Policies{api: api} }

func (p Policies) Service() model.ServiceType { return model.ServiceSWPPolicy }

// ListPermissions is what List needs.
func (p Policies) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "networksecurity.locations.list", "networksecurity.gatewaySecurityPolicies.list")
}

// List lists gateway security policies in every Network Security location.
func (p Policies) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	return list(ctx, p.api, scopeID, API.SecurityLocations, API.ListPolicies, policyResource)
}

// ListRelated lists a policy's rules.
func (p Policies) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceSWPPolicy || facetID != model.FacetRules {
		return nil, false, nil
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Location == "" || n.Kind != "gatewaySecurityPolicies" || len(n.Rest) != 0 {
		return nil, false, nil
	}
	return rules(ctx, p.api, parent, parent.ID)
}

// URLLists implements model.ResourceProvider for URL lists.
type URLLists struct{ api API }

func NewURLLists(api API) URLLists { return URLLists{api: api} }

func (p URLLists) Service() model.ServiceType { return model.ServiceURLList }

// ListPermissions is what List needs.
func (p URLLists) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "networksecurity.locations.list", "networksecurity.urlLists.list")
}

// List lists URL lists in every Network Security location.
func (p URLLists) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	return list(ctx, p.api, scopeID, API.SecurityLocations, API.ListURLLists, urlListResource)
}

// ListRelated loads the URL lists a rule's sessionMatcher or
// applicationMatcher references via inUrlList(...).
func (p URLLists) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceSWPRule || facetID != model.FacetURLLists {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Location == "" || n.Kind != "gatewaySecurityPolicies" || len(n.Rest) != 2 || n.Rest[0] != "rules" {
		return nil, false, nil
	}
	refs := urlListRefs(n.Project, n.Location, parent.Metadata["sessionMatcher"], parent.Metadata["applicationMatcher"])
	if len(refs) == 0 {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("swp: no API")
	}
	out := make([]model.Resource, 0, len(refs))
	for _, ref := range refs {
		u, ok := model.ParseName(ref)
		if !ok || u.Project == "" || u.Location == "" || u.Kind != "urlLists" || len(u.Rest) != 0 {
			continue
		}
		ul, err := p.api.GetURLList(ctx, u.Project, u.Location, u.Name)
		if err != nil {
			return nil, false, err
		}
		res := urlListResource(u.Project, ul)
		res.ScopeID = parent.ScopeID
		res.ParentID = parent.ID
		res.FacetID = model.FacetURLLists
		out = append(out, res)
	}
	return out, false, nil
}

// TLSInspectionPolicies implements model.ResourceProvider for TLS
// inspection policies.
type TLSInspectionPolicies struct{ api API }

func NewTLSInspectionPolicies(api API) TLSInspectionPolicies { return TLSInspectionPolicies{api: api} }

func (p TLSInspectionPolicies) Service() model.ServiceType { return model.ServiceTLSInspection }

// ListPermissions is what List needs.
func (p TLSInspectionPolicies) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "networksecurity.locations.list", "networksecurity.tlsInspectionPolicies.list")
}

// List lists TLS inspection policies in every Network Security location.
func (p TLSInspectionPolicies) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	return list(ctx, p.api, scopeID, API.SecurityLocations, API.ListTLSInspectionPolicies, tlsResource)
}

// list fans out one call per location. Locations that fail are ignored so
// unsupported or restricted regions never surface as errors; list fails only
// when every location fails or ctx ends.
func list[T any](ctx context.Context, api API, scopeID string,
	locations func(API, context.Context, string) ([]string, error),
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
		return nil, errors.New("swp: no API")
	}
	locs, err := locations(api, ctx, project)
	if err != nil {
		return nil, err
	}
	items, locErrs := fanout.Regions(ctx, locs, locationConcurrency, func(ctx context.Context, loc string) ([]T, error) {
		return fetch(api, ctx, project, loc)
	})
	if len(locErrs) > 0 {
		if len(locErrs) == len(locs) {
			return nil, fmt.Errorf("swp: every location failed: %w", locErrs[0])
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		slog.Debug("swp: some locations failed", "project", project, "failed", len(locErrs), "locations", len(locs), "err", locErrs[0])
	}
	out := make([]model.Resource, 0, len(items))
	for _, it := range items {
		out = append(out, toResource(project, it))
	}
	return out, nil
}

// rules lists policy's rules as children of parent, a proxy or a policy.
func rules(ctx context.Context, api API, parent model.Resource, policy string) ([]model.Resource, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(policy)
	if !ok || n.Project == "" || n.Location == "" || n.Kind != "gatewaySecurityPolicies" || len(n.Rest) != 0 {
		return nil, false, nil
	}
	if api == nil {
		return nil, false, errors.New("swp: no API")
	}
	rs, truncated, err := api.ListRules(ctx, n.Project, n.Location, n.Name, maxRules)
	if err != nil {
		return nil, false, err
	}
	out := make([]model.Resource, 0, len(rs))
	for _, r := range rs {
		res := ruleResource(policyID(n.Project, n.Location, n.Name), n.Location, r)
		res.ScopeID = parent.ScopeID
		res.ParentID = parent.ID
		out = append(out, res)
	}
	return out, truncated, nil
}

func locatedID(project, location, kind, name string) string {
	return "projects/" + project + "/locations/" + url.PathEscape(location) + "/" + kind + "/" + url.PathEscape(name)
}

func policyID(project, location, name string) string {
	return locatedID(project, location, "gatewaySecurityPolicies", name)
}

// policyName expands a proxy's policy field back to a full name: a short
// name is the proxy's own project and location.
func policyName(project, location, policy string) string {
	if policy == "" || strings.Contains(policy, "/") {
		return policy
	}
	return policyID(project, location, policy)
}

// shortName drops the project/location prefix from a sibling resource's
// full name, so a field reads "egress-policy" rather than the whole path.
// Names from another project or location stay whole.
func shortName(project, location, name string) string {
	n, ok := model.ParseName(name)
	if !ok || n.Project != project || n.Location != location || len(n.Rest) != 0 {
		return name
	}
	return n.Name
}

func gatewayResource(project string, g Gateway) model.Resource {
	md := map[string]string{}
	set(md, "policy", shortName(project, g.Location, g.Policy))
	set(md, "addresses", strings.Join(g.Addresses, ", "))
	if len(g.Ports) > 0 {
		ports := make([]string, 0, len(g.Ports))
		for _, port := range g.Ports {
			ports = append(ports, strconv.FormatInt(port, 10))
		}
		md["ports"] = strings.Join(ports, ", ")
	}
	set(md, "routingMode", routingMode(g.RoutingMode))
	set(md, "network", lastSeg(g.Network))
	set(md, "subnetwork", lastSeg(g.Subnetwork))
	set(md, "certificates", strings.Join(lastSegs(g.Certificates), ", "))
	set(md, "scope", g.Scope)
	r := model.Resource{
		ID:          locatedID(project, g.Location, "gateways", g.Name),
		Name:        g.Name,
		Service:     model.ServiceWebProxy,
		ScopeID:     "projects/" + project,
		Location:    g.Location,
		Description: g.Description,
		Created:     g.Created,
		Labels:      maps.Clone(g.Labels),
		Metadata:    md,
		Fields:      model.FieldsOf(md, "policy", "addresses", "ports", "routingMode", "network", "subnetwork", "certificates", "scope"),
	}
	if g.Policy != "" {
		r.Facets = []model.Facet{{ID: model.FacetRules, Label: "Rules", Inline: true}}
	}
	return r
}

func policyResource(project string, p Policy) model.Resource {
	md := map[string]string{}
	set(md, "tlsInspectionPolicy", shortName(project, p.Location, p.TLSInspection))
	return model.Resource{
		ID:          policyID(project, p.Location, p.Name),
		Name:        p.Name,
		Service:     model.ServiceSWPPolicy,
		ScopeID:     "projects/" + project,
		Location:    p.Location,
		Description: p.Description,
		Created:     p.Created,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "tlsInspectionPolicy"),
		Facets:      []model.Facet{{ID: model.FacetRules, Label: "Rules", Inline: true}},
	}
}

// inURLListRE matches CEL inUrlList(target, 'urlList') calls in a rule's
// sessionMatcher or applicationMatcher.
var inURLListRE = regexp.MustCompile(`inUrlList\s*\([^,]+,\s*['"]([^'"]+)['"]\s*\)`)

// urlListRefs returns the distinct full URL list names referenced by exprs,
// in first-seen order. Bare names expand in project and location.
func urlListRefs(project, location string, exprs ...string) []string {
	var out []string
	for _, expr := range exprs {
		for _, m := range inURLListRE.FindAllStringSubmatch(expr, -1) {
			ref := strings.TrimSpace(m[1])
			if ref == "" {
				continue
			}
			if !strings.Contains(ref, "/") && project != "" && location != "" {
				ref = locatedID(project, location, "urlLists", ref)
			}
			if !slices.Contains(out, ref) {
				out = append(out, ref)
			}
		}
	}
	return out
}

// ruleResource is a rule under policyID. The caller sets ScopeID and
// ParentID, since a rule lists under both its policy and the proxies that
// enforce it. Rules that call inUrlList(...) expand inline to those URL lists.
func ruleResource(policyID, location string, r Rule) model.Resource {
	var project string
	if n, ok := model.ParseName(policyID); ok {
		project = n.Project
	}
	md := map[string]string{"priority": strconv.FormatInt(r.Priority, 10)}
	set(md, "action", r.Action)
	set(md, "sessionMatcher", r.SessionMatcher)
	set(md, "applicationMatcher", r.ApplicationMatcher)
	refs := urlListRefs(project, location, r.SessionMatcher, r.ApplicationMatcher)
	if len(refs) > 0 {
		short := make([]string, len(refs))
		for i, ref := range refs {
			short[i] = shortName(project, location, ref)
		}
		md["urlLists"] = strings.Join(short, ", ")
	}
	if r.TLSInspection {
		md["tlsInspection"] = "on"
	}
	var st model.ResourceStatus
	if !r.Enabled {
		md["disabled"] = "on"
		st = model.StatusWarning
	}
	var facets []model.Facet
	if len(refs) > 0 {
		facets = []model.Facet{{ID: model.FacetURLLists, Label: "URL Lists", Inline: true}}
	}
	return model.Resource{
		ID:          policyID + "/rules/" + url.PathEscape(r.Name),
		Name:        r.Name,
		Service:     model.ServiceSWPRule,
		Location:    location,
		Status:      st,
		Description: r.Description,
		Created:     r.Created,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "priority", "action", "sessionMatcher", "applicationMatcher", "urlLists", "tlsInspection", "disabled"),
		Facets:      facets,
		Badge:       r.Action,
		FacetID:     model.FacetRules,
	}
}

func urlListResource(project string, u URLList) model.Resource {
	r := model.Resource{
		ID:          locatedID(project, u.Location, "urlLists", u.Name),
		Name:        u.Name,
		Service:     model.ServiceURLList,
		ScopeID:     "projects/" + project,
		Location:    u.Location,
		Description: u.Description,
		Created:     u.Created,
		Metadata:    map[string]string{},
	}
	if len(u.Values) > 0 {
		r.Lists = []model.List{{ID: model.ListURLs, Label: "URLs", Items: append([]string(nil), u.Values...)}}
	}
	return r
}

func tlsResource(project string, t TLSInspectionPolicy) model.Resource {
	md := map[string]string{}
	set(md, "caPool", t.CAPool)
	set(md, "trustConfig", t.TrustConfig)
	set(md, "minTlsVersion", t.MinTLSVersion)
	set(md, "profile", strings.TrimPrefix(t.Profile, "PROFILE_"))
	if t.ExcludePublic {
		md["excludePublicCAs"] = "on"
	}
	return model.Resource{
		ID:          locatedID(project, t.Location, "tlsInspectionPolicies", t.Name),
		Name:        t.Name,
		Service:     model.ServiceTLSInspection,
		ScopeID:     "projects/" + project,
		Location:    t.Location,
		Description: t.Description,
		Created:     t.Created,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "caPool", "trustConfig", "minTlsVersion", "profile", "excludePublicCAs"),
	}
}

// routingMode drops the API's _ROUTING_MODE suffix: EXPLICIT, NEXT_HOP.
func routingMode(s string) string {
	return strings.TrimSuffix(s, "_ROUTING_MODE")
}

func lastSegs(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, lastSeg(n))
	}
	return out
}

func lastSeg(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// set stores v under k when v is not empty.
func set(md map[string]string, k, v string) {
	if v != "" {
		md[k] = v
	}
}

var (
	_ model.ResourceProvider   = WebProxies{}
	_ model.PermissionDeclarer = WebProxies{}
	_ model.RelatedLister      = WebProxies{}
	_ model.ResourceProvider   = Policies{}
	_ model.PermissionDeclarer = Policies{}
	_ model.RelatedLister      = Policies{}
	_ model.ResourceProvider   = URLLists{}
	_ model.PermissionDeclarer = URLLists{}
	_ model.RelatedLister      = URLLists{}
	_ model.ResourceProvider   = TLSInspectionPolicies{}
	_ model.PermissionDeclarer = TLSInspectionPolicies{}
)
