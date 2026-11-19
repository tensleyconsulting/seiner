package compute

import (
	"context"
	"errors"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func projectScope(scopeID string) (string, bool) {
	return model.ProjectID(scopeID)
}

// Networks lists VPC networks and drills into subnets, firewalls, and routes.
type Networks struct{ api API }

func NewNetworks(api API) Networks { return Networks{api: api} }

func (p Networks) Service() model.ServiceType { return model.ServiceVPC }

// ListPermissions is what List needs.
func (p Networks) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "compute.networks.list")
}

func (p Networks) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	project, ok := projectScope(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("compute: no API")
	}
	items, err := p.api.ListNetworks(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(items))
	for _, n := range items {
		out = append(out, networkResource(project, n))
	}
	return out, nil
}

func (p Networks) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceVPC {
		return nil, false, nil
	}
	project, ok := projectScope(parent.ScopeID)
	if !ok {
		project, _, _, _ = parseID(parent.ID, "networks")
	}
	if project == "" || p.api == nil {
		return nil, false, nil
	}
	switch facetID {
	case model.FacetSubnets:
		items, err := p.api.ListSubnets(ctx, project)
		if err != nil {
			return nil, false, err
		}
		out := make([]model.Resource, 0)
		for _, s := range items {
			if s.Network != "" && s.Network != parent.Name {
				continue
			}
			out = append(out, subnetResource(project, s, parent.ID))
		}
		return out, false, nil
	case model.FacetFirewalls:
		items, err := p.api.ListFirewalls(ctx, project)
		if err != nil {
			return nil, false, err
		}
		out := make([]model.Resource, 0)
		for _, f := range items {
			if f.Network != "" && f.Network != parent.Name {
				continue
			}
			out = append(out, firewallResource(project, f, parent.ID))
		}
		return out, false, nil
	case model.FacetRoutes:
		items, err := p.api.ListRoutes(ctx, project)
		if err != nil {
			return nil, false, err
		}
		out := make([]model.Resource, 0)
		for _, r := range items {
			if r.Network != "" && r.Network != parent.Name {
				continue
			}
			out = append(out, routeResource(project, r, parent.ID))
		}
		return out, false, nil
	default:
		return nil, false, nil
	}
}

// Instances lists VMs via aggregated list.
type Instances struct{ api API }

func NewInstances(api API) Instances { return Instances{api: api} }

func (p Instances) Service() model.ServiceType { return model.ServiceComputeEngine }

// ListPermissions is what List needs.
func (p Instances) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "compute.instances.list")
}

func (p Instances) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	project, ok := projectScope(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("compute: no API")
	}
	items, err := p.api.ListInstances(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(items))
	for _, inst := range items {
		out = append(out, instanceResource(project, inst))
	}
	return out, nil
}

// Addresses lists reserved global and regional IP addresses.
type Addresses struct{ api API }

func NewAddresses(api API) Addresses { return Addresses{api: api} }

func (p Addresses) Service() model.ServiceType { return model.ServiceAddress }

// ListPermissions is what List needs.
func (p Addresses) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "compute.addresses.list")
}

func (p Addresses) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	project, ok := projectScope(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("compute: no API")
	}
	items, err := p.api.ListAddresses(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(items))
	for _, a := range items {
		out = append(out, addressResource(project, a))
	}
	return out, nil
}

// ForwardingRules lists global and regional forwarding rules that are not
// PSC endpoints (see PSCEndpoints): load balancer frontends and protocol
// forwarding.
type ForwardingRules struct{ api API }

func NewForwardingRules(api API) ForwardingRules { return ForwardingRules{api: api} }

func (p ForwardingRules) Service() model.ServiceType { return model.ServiceForwardingRule }

// ListPermissions is what List needs.
func (p ForwardingRules) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "compute.forwardingRules.list")
}

func (p ForwardingRules) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	project, ok := projectScope(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("compute: no API")
	}
	return listRules(ctx, p.api, project, false, forwardingRuleResource)
}

// PSCEndpoints lists the forwarding rules that are Private Service Connect
// endpoints: connections to a published service or to Google APIs.
type PSCEndpoints struct{ api API }

func NewPSCEndpoints(api API) PSCEndpoints { return PSCEndpoints{api: api} }

func (p PSCEndpoints) Service() model.ServiceType { return model.ServicePSCEndpoint }

// ListPermissions is what List needs.
func (p PSCEndpoints) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "compute.forwardingRules.list")
}

func (p PSCEndpoints) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	project, ok := projectScope(scopeID)
	if !ok {
		return nil, nil
	}
	return listRules(ctx, p.api, project, true, pscEndpointResource)
}

// listRules lists project's forwarding rules that are PSC endpoints, or
// that are not, as psc says.
func listRules(ctx context.Context, api API, project string, psc bool, mapper func(string, ForwardingRule) model.Resource) ([]model.Resource, error) {
	if api == nil {
		return nil, errors.New("compute: no API")
	}
	items, err := api.ListForwardingRules(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(items))
	for _, f := range items {
		if f.pscEndpoint() == psc {
			out = append(out, mapper(project, f))
		}
	}
	return out, nil
}

// ServiceAttachments lists Private Service Connect service attachments.
type ServiceAttachments struct{ api API }

func NewServiceAttachments(api API) ServiceAttachments { return ServiceAttachments{api: api} }

func (p ServiceAttachments) Service() model.ServiceType { return model.ServiceServiceAttachment }

// ListPermissions is what List needs.
func (p ServiceAttachments) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "compute.serviceAttachments.list")
}

func (p ServiceAttachments) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	project, ok := projectScope(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("compute: no API")
	}
	items, err := p.api.ListServiceAttachments(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(items))
	for _, s := range items {
		out = append(out, serviceAttachmentResource(project, s))
	}
	return out, nil
}

// parseID reads a project-scoped compute name such as
// projects/p/global/networks/n or projects/p/zones/z/instances/n. kind is the
// collection (networks, subnetworks, instances).
func parseID(id, kind string) (project, location, name string, ok bool) {
	n, ok := model.ParseName(id)
	if !ok || n.Project == "" || n.Kind != kind || len(n.Rest) > 0 {
		return "", "", "", false
	}
	return n.Project, n.Location, n.Name, true
}

var (
	_ model.ResourceProvider = Networks{}
	_ model.RelatedLister    = Networks{}
	_ model.ResourceProvider = Instances{}
	_ model.ResourceProvider = Addresses{}
	_ model.ResourceProvider = ForwardingRules{}
	_ model.ResourceProvider = PSCEndpoints{}
	_ model.ResourceProvider = ServiceAttachments{}
)
