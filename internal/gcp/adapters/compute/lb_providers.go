package compute

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"golang.org/x/sync/errgroup"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// LoadBalancers lists load balancers assembled from URL maps, backend
// services, target proxies, and forwarding rules, as the Console does.
// There is no load balancer resource in the API.
type LoadBalancers struct{ api API }

func NewLoadBalancers(api API) LoadBalancers { return LoadBalancers{api: api} }

func (p LoadBalancers) Service() model.ServiceType { return model.ServiceLoadBalancer }

// ListPermissions is what List needs.
func (p LoadBalancers) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID,
		"compute.urlMaps.list",
		"compute.backendServices.list",
		// No target proxy kind is required on its own: one that cannot be
		// listed leaves a partial list (ListTargetProxies).
		"compute.forwardingRules.list",
	)
}

func (p LoadBalancers) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	project, ok := projectScope(scopeID)
	if !ok {
		return nil, nil
	}
	lbs, err := p.load(ctx, project)
	if err != nil && !model.IsPartial(err) {
		return nil, err
	}
	out := make([]model.Resource, 0, len(lbs))
	for _, lb := range lbs {
		out = append(out, loadBalancerResource(project, lb))
	}
	return out, err
}

// ListRelated lists a load balancer's frontends, backend services, or
// health checks. It assembles the load balancers again, from the list calls
// List just made when the API is a Memo; the catalog caches the result.
func (p LoadBalancers) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service == model.ServiceBackendService && facetID == model.FacetPermissions {
		return listPermissions(ctx, p.api, parent, "backendServices")
	}
	if parent.Service != model.ServiceLoadBalancer {
		return nil, false, nil
	}
	switch facetID {
	case model.FacetFrontends, model.FacetBackends, model.FacetHealthChecks:
	default:
		return nil, false, nil
	}
	project, ok := projectScope(parent.ScopeID)
	if !ok {
		return nil, false, nil
	}
	// List already reported a partial load; a facet shows what it has.
	lbs, err := p.load(ctx, project)
	if err != nil && !model.IsPartial(err) {
		return nil, false, err
	}
	i := slices.IndexFunc(lbs, func(lb loadBalancer) bool { return lbID(project, lb) == parent.ID })
	if i < 0 {
		return []model.Resource{}, false, nil
	}
	lb := lbs[i]
	out := []model.Resource{}
	switch facetID {
	case model.FacetFrontends:
		for _, f := range lb.Frontends {
			r := forwardingRuleResource(project, f)
			r.ParentID, r.FacetID = parent.ID, model.FacetFrontends
			out = append(out, r)
		}
	case model.FacetBackends:
		for _, b := range lb.Backends {
			out = append(out, backendServiceResource(project, b, parent.ID))
		}
	case model.FacetHealthChecks:
		refs := lb.healthCheckRefs()
		if len(refs) == 0 {
			return out, false, nil
		}
		hcs, err := p.api.ListHealthChecks(ctx, project)
		if err != nil {
			return nil, false, err
		}
		for _, h := range hcs {
			if slices.Contains(refs, refFor(h.Region, "healthChecks", h.Name)) {
				out = append(out, healthCheckResource(project, h, parent.ID))
			}
		}
	}
	return out, false, nil
}

// load fetches the load balancer parts concurrently and assembles them. A
// model.Partial error from the proxies comes back with the load balancers.
func (p LoadBalancers) load(ctx context.Context, project string) ([]loadBalancer, error) {
	if p.api == nil {
		return nil, errors.New("compute: no API")
	}
	var in lbInputs
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) { in.maps, err = p.api.ListURLMaps(gctx, project); return err })
	g.Go(func() (err error) { in.services, err = p.api.ListBackendServices(gctx, project); return err })
	var partial error
	g.Go(func() (err error) {
		in.proxies, err = p.api.ListTargetProxies(gctx, project)
		if model.IsPartial(err) {
			partial, err = err, nil
		}
		return err
	})
	g.Go(func() (err error) { in.rules, err = p.api.ListForwardingRules(gctx, project); return err })
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return buildLoadBalancers(in), partial
}

// HealthChecks lists global and regional health checks.
type HealthChecks struct{ api API }

func NewHealthChecks(api API) HealthChecks { return HealthChecks{api: api} }

func (p HealthChecks) Service() model.ServiceType { return model.ServiceHealthCheck }

// ListPermissions is what List needs.
func (p HealthChecks) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "compute.healthChecks.list")
}

func (p HealthChecks) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	project, ok := projectScope(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("compute: no API")
	}
	items, err := p.api.ListHealthChecks(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(items))
	for _, h := range items {
		out = append(out, healthCheckResource(project, h, ""))
	}
	return out, nil
}

// SecurityPolicies lists Cloud Armor policies and the backend services each
// is attached to.
type SecurityPolicies struct{ api API }

func NewSecurityPolicies(api API) SecurityPolicies { return SecurityPolicies{api: api} }

func (p SecurityPolicies) Service() model.ServiceType { return model.ServiceCloudArmor }

// ListPermissions is what List needs.
func (p SecurityPolicies) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "compute.securityPolicies.list")
}

func (p SecurityPolicies) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	project, ok := projectScope(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("compute: no API")
	}
	items, err := p.api.ListSecurityPolicies(ctx, project)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return []model.Resource{}, nil
	}
	// Attachments only add detail, so a failure here keeps the policies.
	usedBy := map[string][]string{}
	services, err := p.api.ListBackendServices(ctx, project)
	if err != nil {
		slog.Warn("compute: cloud armor attachments unavailable", "project", project, "err", err)
	}
	for _, b := range services {
		for _, ref := range []string{b.SecurityPolicy, b.EdgeSecurityPolicy} {
			if ref != "" {
				usedBy[ref] = append(usedBy[ref], "backendServices/"+b.Name)
			}
		}
	}
	out := make([]model.Resource, 0, len(items))
	for _, sp := range items {
		out = append(out, securityPolicyResource(project, sp, usedBy[refFor(sp.Region, "securityPolicies", sp.Name)]))
	}
	return out, nil
}

// SSLPolicies lists global and regional SSL policies, each with the target
// proxies that apply it.
type SSLPolicies struct{ api API }

func NewSSLPolicies(api API) SSLPolicies { return SSLPolicies{api: api} }

func (p SSLPolicies) Service() model.ServiceType { return model.ServiceSSLPolicy }

// ListPermissions is what List needs.
func (p SSLPolicies) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "compute.sslPolicies.list")
}

func (p SSLPolicies) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	project, ok := projectScope(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("compute: no API")
	}
	items, err := p.api.ListSSLPolicies(ctx, project)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return []model.Resource{}, nil
	}
	// Proxies only add detail, so a failure here keeps the policies.
	proxies, err := p.api.ListTargetProxies(ctx, project)
	if err != nil {
		slog.Warn("compute: ssl policy targets unavailable", "project", project, "err", err)
	}
	usedBy := sslPolicyUsers(proxies)
	out := make([]model.Resource, 0, len(items))
	for _, sp := range items {
		out = append(out, sslPolicyResource(project, sp, usedBy[[2]string{sp.Region, sp.Name}]))
	}
	return out, nil
}

var (
	_ model.ResourceProvider = LoadBalancers{}
	_ model.RelatedLister    = LoadBalancers{}
	_ model.ResourceProvider = HealthChecks{}
	_ model.ResourceProvider = SecurityPolicies{}
	_ model.ResourceProvider = SSLPolicies{}
)
