package swp

import (
	"context"
	"errors"
	"time"

	nsec "google.golang.org/api/networksecurity/v1"
	nsvc "google.golang.org/api/networkservices/v1"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
	"github.com/tensleyconsulting/seiner/internal/gcp/regions"
)

// secureWebGateway is the gateway type Secure Web Proxy creates. The same
// list also returns Traffic Director (OPEN_MESH) gateways.
const secureWebGateway = "SECURE_WEB_GATEWAY"

type liveAPI struct {
	svc *nsvc.Service
	sec *nsec.Service
}

// NewLazy defers client construction to the first call, so a slow or failing
// constructor cannot block startup; the failure surfaces as that service's
// ProviderError instead. Share the result across this package's providers so
// each client and location list is built once.
func NewLazy(opts ...option.ClientOption) API {
	svc := lazy.New(func(ctx context.Context) (*nsvc.Service, error) {
		return nsvc.NewService(ctx, opts...)
	})
	sec := lazy.New(func(ctx context.Context) (*nsec.Service, error) {
		return nsec.NewService(ctx, opts...)
	})
	a := lazyAPI{svc: svc, sec: sec}
	a.svcLocs = regions.Cached(func(ctx context.Context, project string) ([]string, error) {
		c, err := svc.Get(ctx)
		if err != nil {
			return nil, err
		}
		return liveAPI{svc: c}.GatewayLocations(ctx, project)
	})
	a.secLocs = regions.Cached(func(ctx context.Context, project string) ([]string, error) {
		c, err := sec.Get(ctx)
		if err != nil {
			return nil, err
		}
		return liveAPI{sec: c}.SecurityLocations(ctx, project)
	})
	return a
}

// lazyAPI builds each client on first use. Location lists are cached for
// the session, since they change rarely and every list starts with one.
type lazyAPI struct {
	svc     *lazy.Value[*nsvc.Service]
	sec     *lazy.Value[*nsec.Service]
	svcLocs regions.Lister
	secLocs regions.Lister
}

func (a lazyAPI) GatewayLocations(ctx context.Context, project string) ([]string, error) {
	return a.svcLocs.Regions(ctx, project)
}

func (a lazyAPI) SecurityLocations(ctx context.Context, project string) ([]string, error) {
	return a.secLocs.Regions(ctx, project)
}

func (a lazyAPI) ListGateways(ctx context.Context, project, location string) ([]Gateway, error) {
	c, err := a.svc.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: c}.ListGateways(ctx, project, location)
}

func (a lazyAPI) ListPolicies(ctx context.Context, project, location string) ([]Policy, error) {
	c, err := a.sec.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{sec: c}.ListPolicies(ctx, project, location)
}

func (a lazyAPI) ListRules(ctx context.Context, project, location, policy string, limit int) ([]Rule, bool, error) {
	c, err := a.sec.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveAPI{sec: c}.ListRules(ctx, project, location, policy, limit)
}

func (a lazyAPI) ListURLLists(ctx context.Context, project, location string) ([]URLList, error) {
	c, err := a.sec.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{sec: c}.ListURLLists(ctx, project, location)
}

func (a lazyAPI) GetURLList(ctx context.Context, project, location, name string) (URLList, error) {
	c, err := a.sec.Get(ctx)
	if err != nil {
		return URLList{}, err
	}
	return liveAPI{sec: c}.GetURLList(ctx, project, location, name)
}

func (a lazyAPI) ListTLSInspectionPolicies(ctx context.Context, project, location string) ([]TLSInspectionPolicy, error) {
	c, err := a.sec.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{sec: c}.ListTLSInspectionPolicies(ctx, project, location)
}

var (
	_ API = lazyAPI{}
	_ API = liveAPI{}
)

// errStop ends paging once the rule limit is reached.
var errStop = errors.New("stop")

func (a liveAPI) GatewayLocations(ctx context.Context, project string) ([]string, error) {
	var out []string
	err := a.svc.Projects.Locations.List("projects/"+project).Pages(ctx, func(resp *nsvc.ListLocationsResponse) error {
		for _, l := range resp.Locations {
			if l.LocationId != "" && !isZone(l.LocationId) {
				out = append(out, l.LocationId)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a liveAPI) SecurityLocations(ctx context.Context, project string) ([]string, error) {
	var out []string
	err := a.sec.Projects.Locations.List("projects/"+project).Pages(ctx, func(resp *nsec.ListLocationsResponse) error {
		for _, l := range resp.Locations {
			if l.LocationId != "" && !isZone(l.LocationId) {
				out = append(out, l.LocationId)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// isZone reports whether loc is a Compute zone ("us-central1-a"). Both
// Locations APIs include zones for other resource types, while SWP resources
// live only in regions (or global).
func isZone(loc string) bool {
	n := len(loc)
	return n >= 4 && loc[n-2] == '-' && loc[n-1] >= 'a' && loc[n-1] <= 'z' && loc[n-3] >= '0' && loc[n-3] <= '9'
}

func (a liveAPI) ListGateways(ctx context.Context, project, location string) ([]Gateway, error) {
	var out []Gateway
	err := a.svc.Projects.Locations.Gateways.List(parent(project, location)).Pages(ctx, func(resp *nsvc.ListGatewaysResponse) error {
		for _, g := range resp.Gateways {
			if g.Type == secureWebGateway {
				out = append(out, fromGateway(location, g))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a liveAPI) ListPolicies(ctx context.Context, project, location string) ([]Policy, error) {
	var out []Policy
	err := a.sec.Projects.Locations.GatewaySecurityPolicies.List(parent(project, location)).Pages(ctx, func(resp *nsec.ListGatewaySecurityPoliciesResponse) error {
		for _, p := range resp.GatewaySecurityPolicies {
			out = append(out, Policy{
				Name:          lastSeg(p.Name),
				Location:      location,
				Description:   p.Description,
				TLSInspection: p.TlsInspectionPolicy,
				Created:       parseTime(p.CreateTime),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a liveAPI) ListRules(ctx context.Context, project, location, policy string, limit int) ([]Rule, bool, error) {
	var out []Rule
	truncated := false
	name := parent(project, location) + "/gatewaySecurityPolicies/" + policy
	err := a.sec.Projects.Locations.GatewaySecurityPolicies.Rules.List(name).Pages(ctx, func(resp *nsec.ListGatewaySecurityPolicyRulesResponse) error {
		for _, r := range resp.GatewaySecurityPolicyRules {
			if len(out) == limit {
				truncated = true
				return errStop
			}
			out = append(out, fromRule(r))
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return nil, false, err
	}
	return out, truncated, nil
}

func (a liveAPI) ListURLLists(ctx context.Context, project, location string) ([]URLList, error) {
	var out []URLList
	err := a.sec.Projects.Locations.UrlLists.List(parent(project, location)).Pages(ctx, func(resp *nsec.ListUrlListsResponse) error {
		for _, u := range resp.UrlLists {
			out = append(out, fromURLList(location, u))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a liveAPI) GetURLList(ctx context.Context, project, location, name string) (URLList, error) {
	u, err := a.sec.Projects.Locations.UrlLists.Get(parent(project, location) + "/urlLists/" + name).Context(ctx).Do()
	if err != nil {
		return URLList{}, err
	}
	return fromURLList(location, u), nil
}

func fromURLList(location string, u *nsec.UrlList) URLList {
	return URLList{
		Name:        lastSeg(u.Name),
		Location:    location,
		Description: u.Description,
		Values:      u.Values,
		Created:     parseTime(u.CreateTime),
	}
}

func (a liveAPI) ListTLSInspectionPolicies(ctx context.Context, project, location string) ([]TLSInspectionPolicy, error) {
	var out []TLSInspectionPolicy
	err := a.sec.Projects.Locations.TlsInspectionPolicies.List(parent(project, location)).Pages(ctx, func(resp *nsec.ListTlsInspectionPoliciesResponse) error {
		for _, t := range resp.TlsInspectionPolicies {
			out = append(out, fromTLS(location, t))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func fromGateway(location string, g *nsvc.Gateway) Gateway {
	return Gateway{
		Name:         lastSeg(g.Name),
		Location:     location,
		Description:  g.Description,
		Policy:       g.GatewaySecurityPolicy,
		Addresses:    g.Addresses,
		Ports:        g.Ports,
		RoutingMode:  g.RoutingMode,
		Network:      g.Network,
		Subnetwork:   g.Subnetwork,
		Certificates: g.CertificateUrls,
		Scope:        g.Scope,
		Created:      parseTime(g.CreateTime),
		Labels:       g.Labels,
	}
}

func fromRule(r *nsec.GatewaySecurityPolicyRule) Rule {
	return Rule{
		Name:               lastSeg(r.Name),
		Description:        r.Description,
		Priority:           r.Priority,
		Action:             r.BasicProfile,
		Enabled:            r.Enabled,
		SessionMatcher:     r.SessionMatcher,
		ApplicationMatcher: r.ApplicationMatcher,
		TLSInspection:      r.TlsInspectionEnabled,
		Created:            parseTime(r.CreateTime),
	}
}

func fromTLS(location string, t *nsec.TlsInspectionPolicy) TLSInspectionPolicy {
	return TLSInspectionPolicy{
		Name:          lastSeg(t.Name),
		Location:      location,
		Description:   t.Description,
		CAPool:        t.CaPool,
		TrustConfig:   t.TrustConfig,
		MinTLSVersion: t.MinTlsVersion,
		Profile:       t.TlsFeatureProfile,
		ExcludePublic: t.ExcludePublicCaSet,
		Created:       parseTime(t.CreateTime),
	}
}

func parent(project, location string) string {
	return "projects/" + project + "/locations/" + location
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
