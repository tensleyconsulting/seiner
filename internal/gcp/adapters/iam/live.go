package iam

import (
	"context"
	"errors"
	"path"
	"slices"
	"strings"

	admin "cloud.google.com/go/iam/admin/apiv1"
	"cloud.google.com/go/iam/admin/apiv1/adminpb"
	iampb "cloud.google.com/go/iam/apiv1/iampb"
	"cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/crm"
	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
)

// liveAPI reads IAM policies and node names through the Resource Manager
// clients the crm package builds.
type liveAPI struct{ crm.Clients }

// NewLazyAPI returns an API over the Resource Manager clients, which are
// built on first use. Pass the crm package's set (crm.NewLazyClients), so
// the tree, IAM, and service accounts share one set of clients.
func NewLazyAPI(clients *lazy.Value[crm.Clients]) API {
	return lazyAPI{l: clients}
}

type lazyAPI struct {
	l *lazy.Value[crm.Clients]
}

func (a lazyAPI) GetPolicy(ctx context.Context, resource string) (Policy, error) {
	api, err := a.l.Get(ctx)
	if err != nil {
		return Policy{}, err
	}
	return liveAPI{api}.GetPolicy(ctx, resource)
}

func (a lazyAPI) GetNode(ctx context.Context, name string) (Node, error) {
	api, err := a.l.Get(ctx)
	if err != nil {
		return Node{}, err
	}
	return liveAPI{api}.GetNode(ctx, name)
}

var _ API = lazyAPI{}

// GetNode reads a project, folder, or organization's display name and parent.
func (a liveAPI) GetNode(ctx context.Context, name string) (Node, error) {
	switch {
	case strings.HasPrefix(name, "organizations/"):
		o, err := a.Orgs.GetOrganization(ctx, &resourcemanagerpb.GetOrganizationRequest{Name: name})
		if err != nil {
			return Node{}, err
		}
		return Node{DisplayName: o.GetDisplayName()}, nil
	case strings.HasPrefix(name, "folders/"):
		f, err := a.Folders.GetFolder(ctx, &resourcemanagerpb.GetFolderRequest{Name: name})
		if err != nil {
			return Node{}, err
		}
		return Node{DisplayName: f.GetDisplayName(), Parent: f.GetParent()}, nil
	default:
		p, err := a.Projects.GetProject(ctx, &resourcemanagerpb.GetProjectRequest{Name: name})
		if err != nil {
			return Node{}, err
		}
		return Node{DisplayName: p.GetDisplayName(), Parent: p.GetParent()}, nil
	}
}

func (a liveAPI) GetPolicy(ctx context.Context, resource string) (Policy, error) {
	req := policyRequest(resource)
	var (
		pol *iampb.Policy
		err error
	)
	switch {
	case strings.HasPrefix(resource, "organizations/"):
		pol, err = a.Orgs.GetIamPolicy(ctx, req)
	case strings.HasPrefix(resource, "folders/"):
		pol, err = a.Folders.GetIamPolicy(ctx, req)
	default:
		pol, err = a.Projects.GetIamPolicy(ctx, req)
	}
	if err != nil {
		return Policy{}, err
	}
	return fromPolicy(pol), nil
}

// policyVersion is the IAM policy format to request. Version 3 is required
// to read conditional bindings; older versions mangle their roles and drop
// the condition.
const policyVersion = 3

func policyRequest(resource string) *iampb.GetIamPolicyRequest {
	return &iampb.GetIamPolicyRequest{
		Resource: resource,
		Options:  &iampb.GetPolicyOptions{RequestedPolicyVersion: policyVersion},
	}
}

func fromPolicy(pol *iampb.Policy) Policy {
	out := Policy{Bindings: make([]Binding, 0, len(pol.GetBindings()))}
	for _, b := range pol.GetBindings() {
		out.Bindings = append(out.Bindings, Binding{
			Role:      b.GetRole(),
			Members:   append([]string(nil), b.GetMembers()...),
			Condition: b.GetCondition().GetTitle(),
		})
	}
	return out
}

// NewLazyAdmin returns an AdminAPI whose IAM Admin client is built on first
// use and closed by g. ServiceAccounts and CustomRoles share it, so it
// builds once.
func NewLazyAdmin(g *lazy.Group, opts ...option.ClientOption) AdminAPI {
	l := lazy.NewIn(g, func(ctx context.Context) (*admin.IamClient, error) {
		return admin.NewIamClient(ctx, opts...)
	})
	return lazyAdmin{l: l}
}

type lazyAdmin struct {
	l *lazy.Value[*admin.IamClient]
}

func (a lazyAdmin) ListServiceAccounts(ctx context.Context, project string) ([]ServiceAccount, error) {
	c, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAdmin{client: c}.ListServiceAccounts(ctx, project)
}

func (a lazyAdmin) ListRoles(ctx context.Context, parent string) ([]Role, error) {
	c, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAdmin{client: c}.ListRoles(ctx, parent)
}

func (a lazyAdmin) GetRole(ctx context.Context, name string) (Role, error) {
	c, err := a.l.Get(ctx)
	if err != nil {
		return Role{}, err
	}
	return liveAdmin{client: c}.GetRole(ctx, name)
}

func (a lazyAdmin) ListServiceAccountKeys(ctx context.Context, project, email string) ([]ServiceAccountKey, error) {
	c, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAdmin{client: c}.ListServiceAccountKeys(ctx, project, email)
}

func (a lazyAdmin) GetServiceAccountPolicy(ctx context.Context, project, email string) (Policy, error) {
	c, err := a.l.Get(ctx)
	if err != nil {
		return Policy{}, err
	}
	return liveAdmin{client: c}.GetServiceAccountPolicy(ctx, project, email)
}

var _ AdminAPI = lazyAdmin{}

type liveAdmin struct {
	client *admin.IamClient
}

func (a liveAdmin) ListServiceAccounts(ctx context.Context, project string) ([]ServiceAccount, error) {
	it := a.client.ListServiceAccounts(ctx, &adminpb.ListServiceAccountsRequest{Name: "projects/" + project})
	var out []ServiceAccount
	for {
		sa, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		out = append(out, ServiceAccount{
			Email:          sa.GetEmail(),
			DisplayName:    sa.GetDisplayName(),
			Description:    sa.GetDescription(),
			UniqueID:       sa.GetUniqueId(),
			OAuth2ClientID: sa.GetOauth2ClientId(),
			Disabled:       sa.GetDisabled(),
		})
	}
}

func (a liveAdmin) ListServiceAccountKeys(ctx context.Context, project, email string) ([]ServiceAccountKey, error) {
	resp, err := a.client.ListServiceAccountKeys(ctx, &adminpb.ListServiceAccountKeysRequest{Name: "projects/" + project + "/serviceAccounts/" + email})
	if err != nil {
		return nil, err
	}
	out := make([]ServiceAccountKey, 0, len(resp.GetKeys()))
	for _, k := range resp.GetKeys() {
		out = append(out, fromKey(k))
	}
	return out, nil
}

func (a liveAdmin) GetServiceAccountPolicy(ctx context.Context, project, email string) (Policy, error) {
	pol, err := a.client.GetIamPolicy(ctx, policyRequest("projects/"+project+"/serviceAccounts/"+email))
	if err != nil {
		return Policy{}, err
	}
	if pol == nil {
		return Policy{}, nil
	}
	return fromPolicy(pol.InternalProto), nil
}

func fromKey(k *adminpb.ServiceAccountKey) ServiceAccountKey {
	out := ServiceAccountKey{
		ID:        path.Base(k.GetName()),
		Type:      k.GetKeyType().String(),
		Origin:    k.GetKeyOrigin().String(),
		Algorithm: k.GetKeyAlgorithm().String(),
		Disabled:  k.GetDisabled(),
	}
	if t := k.GetValidAfterTime(); t != nil {
		out.ValidAfter = t.AsTime()
	}
	if t := k.GetValidBeforeTime(); t != nil {
		out.ValidBefore = t.AsTime()
	}
	return out
}

// ListRoles asks for the FULL view, which includes each role's permissions.
// Deleted roles are left out.
func (a liveAdmin) ListRoles(ctx context.Context, parent string) ([]Role, error) {
	it := a.client.ListRolesIter(ctx, &adminpb.ListRolesRequest{Parent: parent, View: adminpb.RoleView_FULL})
	var out []Role
	for {
		r, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if r.GetDeleted() {
			continue
		}
		out = append(out, fromRole(r))
	}
}

// GetRole reads one role, with its permissions.
func (a liveAdmin) GetRole(ctx context.Context, name string) (Role, error) {
	r, err := a.client.GetRole(ctx, &adminpb.GetRoleRequest{Name: name})
	if err != nil {
		return Role{}, err
	}
	return fromRole(r), nil
}

func fromRole(r *adminpb.Role) Role {
	return Role{
		Name:        r.GetName(),
		Title:       r.GetTitle(),
		Description: r.GetDescription(),
		Stage:       r.GetStage().String(),
		Permissions: slices.Clone(r.GetIncludedPermissions()),
	}
}
