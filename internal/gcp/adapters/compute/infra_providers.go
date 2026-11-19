package compute

import (
	"context"
	"errors"
	"strings"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// listProject is the List shape every project-scoped compute collection
// shares: read the project, call the API, map each item.
func listProject[T any](ctx context.Context, api API, scopeID string, list func(API, context.Context, string) ([]T, error), mapItem func(string, T) model.Resource) ([]model.Resource, error) {
	project, ok := projectScope(scopeID)
	if !ok {
		return nil, nil
	}
	if api == nil {
		return nil, errors.New("compute: no API")
	}
	items, err := list(api, ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(items))
	for _, it := range items {
		out = append(out, mapItem(project, it))
	}
	return out, nil
}

// Disks lists zonal and regional persistent disks.
type Disks struct{ api API }

func NewDisks(api API) Disks { return Disks{api: api} }

func (p Disks) Service() model.ServiceType { return model.ServiceDisk }

// ListPermissions is what List needs.
func (p Disks) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "compute.disks.list")
}

func (p Disks) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	return listProject(ctx, p.api, scopeID, API.ListDisks, diskResource)
}

// InstanceTemplates lists global and regional instance templates.
type InstanceTemplates struct{ api API }

func NewInstanceTemplates(api API) InstanceTemplates { return InstanceTemplates{api: api} }

func (p InstanceTemplates) Service() model.ServiceType { return model.ServiceInstanceTemplate }

// ListPermissions is what List needs.
func (p InstanceTemplates) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "compute.instanceTemplates.list")
}

func (p InstanceTemplates) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	return listProject(ctx, p.api, scopeID, API.ListInstanceTemplates, instanceTemplateResource)
}

// InstanceGroups lists zonal and regional managed instance groups.
type InstanceGroups struct{ api API }

func NewInstanceGroups(api API) InstanceGroups { return InstanceGroups{api: api} }

func (p InstanceGroups) Service() model.ServiceType { return model.ServiceInstanceGroup }

// ListPermissions is what List needs.
func (p InstanceGroups) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "compute.instanceGroupManagers.list")
}

func (p InstanceGroups) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	return listProject(ctx, p.api, scopeID, API.ListInstanceGroups, instanceGroupResource)
}

// Routers lists Cloud Routers and drills into their NAT gateways.
type Routers struct{ api API }

func NewRouters(api API) Routers { return Routers{api: api} }

func (p Routers) Service() model.ServiceType { return model.ServiceRouter }

// ListPermissions is what List needs.
func (p Routers) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "compute.routers.list")
}

func (p Routers) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	return listProject(ctx, p.api, scopeID, API.ListRouters, routerResource)
}

// ListRelated lists a router's NAT gateways, which the API keeps on the
// router itself.
func (p Routers) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceRouter || facetID != model.FacetNATs {
		return nil, false, nil
	}
	project, region, name, ok := parseID(parent.ID, "routers")
	if !ok || region == "" || p.api == nil {
		return nil, false, nil
	}
	r, err := p.api.GetRouter(ctx, project, region, name)
	if err != nil {
		return nil, false, err
	}
	out := make([]model.Resource, 0, len(r.NATs))
	for _, n := range r.NATs {
		out = append(out, natResource(parent, n))
	}
	return out, false, nil
}

// maxManagedInstances caps the instances listed under one group, or under
// one node pool across its groups.
const maxManagedInstances = 500

// ManagedInstances lists the instances of a managed instance group, and
// the nodes of a GKE node pool through the groups the pool names
// (model.ListInstanceGroups). It lists nothing at a scope.
type ManagedInstances struct{ api API }

func NewManagedInstances(api API) ManagedInstances { return ManagedInstances{api: api} }

func (p ManagedInstances) Service() model.ServiceType { return model.ServiceManagedInstance }

func (p ManagedInstances) List(ctx context.Context, _ string) ([]model.Resource, error) {
	return nil, ctx.Err()
}

func (p ManagedInstances) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	var groups []string
	switch {
	case parent.Service == model.ServiceInstanceGroup && facetID == model.FacetInstances:
		groups = []string{parent.ID}
	case parent.Service == model.ServiceGKENodePool && facetID == model.FacetNodes:
		groups = parent.List(model.ListInstanceGroups)
	default:
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if p.api == nil {
		return nil, false, errors.New("compute: no API")
	}
	var (
		out       []model.Resource
		truncated bool
	)
	for _, g := range groups {
		project, zone, region, name, ok := parseGroupID(g)
		if !ok {
			continue
		}
		limit := maxManagedInstances - len(out)
		if limit <= 0 {
			truncated = true
			break
		}
		insts, more, err := p.api.ListManagedInstances(ctx, project, zone, region, name, limit)
		if err != nil {
			return nil, false, err
		}
		truncated = truncated || more
		for _, m := range insts {
			if m.Name == "" {
				continue // no instance yet; nothing to name the row by
			}
			r := managedInstanceResource(project, parent.ID, facetID, name, zone+region, m)
			r.ScopeID = parent.ScopeID
			out = append(out, r)
		}
	}
	return out, truncated, nil
}

// parseGroupID splits projects/p/zones/z/instanceGroupManagers/n (or
// regions/r). ParseName keeps zone and region in one Location, so the
// segment is read here.
func parseGroupID(id string) (project, zone, region, name string, ok bool) {
	project, loc, name, ok := parseID(id, "instanceGroupManagers")
	if !ok || loc == "" {
		return "", "", "", "", false
	}
	if strings.Contains(id, "/zones/") {
		return project, loc, "", name, true
	}
	return project, "", loc, name, true
}

var (
	_ model.ResourceProvider = ManagedInstances{}
	_ model.RelatedLister    = ManagedInstances{}
	_ model.ResourceProvider = Disks{}
	_ model.ResourceProvider = InstanceTemplates{}
	_ model.ResourceProvider = InstanceGroups{}
	_ model.ResourceProvider = Routers{}
	_ model.RelatedLister    = Routers{}
)
