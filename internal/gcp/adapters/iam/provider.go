package iam

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// Provider lists IAM policy bindings for the current CRM scope: those set
// on the scope itself, then those it inherits from its folders and
// organization.
type Provider struct {
	api API
}

func New(api API) Provider {
	return Provider{api: api}
}

func (p Provider) Service() model.ServiceType { return model.ServiceIAMBinding }

// ListPermissions is what List needs: the scope's own policy. Ancestor
// policies only add inherited bindings, so they are not required.
func (p Provider) ListPermissions(scopeID string) []string {
	switch model.ScopeKind(scopeID) {
	case model.KindOrg:
		return []string{"resourcemanager.organizations.getIamPolicy"}
	case model.KindFolder:
		return []string{"resourcemanager.folders.getIamPolicy"}
	case model.KindProject:
		return []string{"resourcemanager.projects.getIamPolicy"}
	}
	return nil
}

// List returns the scope's own bindings and its inherited ones. Only the
// scope's own policy must load: an ancestor whose lookup or policy fails
// (no folder or organization access is common) is logged and skipped.
func (p Provider) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resource := model.IAMResourceName(scopeID)
	if resource == "" {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("iam: no API")
	}
	pol, err := p.api.GetPolicy(ctx, resource)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(pol.Bindings))
	for _, b := range pol.Bindings {
		out = append(out, resourceFrom(scopeID, resource, b))
	}
	inherited, err := p.inherited(ctx, scopeID, resource)
	if err != nil {
		return nil, err
	}
	return append(out, inherited...), nil
}

// inherited lists the bindings resource inherits, nearest ancestor first.
// It fails only when ctx ends, so a timed-out load is not mistaken for a
// complete one.
func (p Provider) inherited(ctx context.Context, scopeID, resource string) ([]model.Resource, error) {
	ancestors, err := Ancestors(ctx, p.api, resource)
	if err != nil {
		slog.Warn("iam: ancestry incomplete; inherited bindings may be missing", "scope", scopeID, "found", len(ancestors), "err", err)
	}
	policies := make([]Policy, len(ancestors))
	var wg sync.WaitGroup
	for i, a := range ancestors {
		wg.Go(func() {
			pol, err := p.api.GetPolicy(ctx, a.Name)
			if err != nil {
				slog.Warn("iam: ancestor policy unavailable", "scope", scopeID, "ancestor", a.Name, "err", err)
				return
			}
			policies[i] = pol
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out []model.Resource
	for i, a := range ancestors {
		for _, b := range policies[i].Bindings {
			out = append(out, model.InheritedBindingResource(scopeID, a, b.Role, b.Members, b.Condition))
		}
	}
	return out, nil
}

// maxDepth bounds the ancestry walk. Resource Manager allows ten levels
// of folders under an organization.
const maxDepth = 12

// Ancestors lists the folders and organization above resource, a CRM
// name, nearest first. When a lookup fails part way it returns the
// ancestors found so far with the error. An ancestor that cannot be read
// is still listed, without a display name, because its policy may be
// readable.
func Ancestors(ctx context.Context, api API, resource string) ([]model.Ancestor, error) {
	if model.ScopeKind(resource) == model.KindOrg {
		return nil, nil
	}
	n, err := api.GetNode(ctx, resource)
	if err != nil {
		return nil, err
	}
	var out []model.Ancestor
	for parent := n.Parent; parent != "" && len(out) < maxDepth; parent = n.Parent {
		n, err = api.GetNode(ctx, parent)
		if err != nil {
			return append(out, model.Ancestor{Name: parent, Level: len(out) + 1}), err
		}
		out = append(out, model.Ancestor{Name: parent, DisplayName: n.DisplayName, Level: len(out) + 1})
	}
	return out, nil
}

func resourceFrom(scopeID, resource string, b Binding) model.Resource {
	return model.BindingResource(resource, scopeID, b.Role, b.Members, b.Condition)
}

var _ model.ResourceProvider = Provider{}
