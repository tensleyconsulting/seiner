package crm

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	iampb "cloud.google.com/go/iam/apiv1/iampb"
	resourcemanager "cloud.google.com/go/resourcemanager/apiv3"
	"cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// Client is the Resource Manager API plus permission checks, sharing one
// set of clients.
type Client interface {
	API
	model.PermissionTester
}

// Clients is one set of Resource Manager clients. The tree, permission
// checks, and the iam package's policy reads share one set.
type Clients struct {
	Projects *resourcemanager.ProjectsClient
	Folders  *resourcemanager.FoldersClient
	Orgs     *resourcemanager.OrganizationsClient
}

// Close closes each client.
func (c Clients) Close() error {
	return errors.Join(c.Projects.Close(), c.Folders.Close(), c.Orgs.Close())
}

func buildClients(ctx context.Context, opts ...option.ClientOption) (Clients, error) {
	pc, err := resourcemanager.NewProjectsClient(ctx, opts...)
	if err != nil {
		return Clients{}, err
	}
	fc, err := resourcemanager.NewFoldersClient(ctx, opts...)
	if err != nil {
		_ = pc.Close()
		return Clients{}, err
	}
	oc, err := resourcemanager.NewOrganizationsClient(ctx, opts...)
	if err != nil {
		_ = pc.Close()
		_ = fc.Close()
		return Clients{}, err
	}
	return Clients{Projects: pc, Folders: fc, Orgs: oc}, nil
}

// NewLazyClients returns the Resource Manager clients, built together on
// the first Get and closed by g. Construct one per catalog and pass it to
// NewLazy and iam.NewLazyAPI.
func NewLazyClients(g *lazy.Group, opts ...option.ClientOption) *lazy.Value[Clients] {
	return lazy.NewIn(g, func(ctx context.Context) (Clients, error) {
		return buildClients(ctx, opts...)
	})
}

// NewLazy returns an API over clients, which are constructed on first use
// rather than at startup. A construction failure is returned from that
// first call instead of blocking Open; crm.Build turns it into the same
// provider error a failed tree fetch would produce.
func NewLazy(clients *lazy.Value[Clients]) Client {
	return lazyAPI{l: clients}
}

type lazyAPI struct {
	l *lazy.Value[Clients]
}

// liveAPI reads Resource Manager through a built client set.
type liveAPI struct{ Clients }

func (a lazyAPI) SearchProjects(ctx context.Context) ([]Project, error) {
	api, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{api}.SearchProjects(ctx)
}

func (a lazyAPI) GetFolder(ctx context.Context, name string) (Folder, error) {
	api, err := a.l.Get(ctx)
	if err != nil {
		return Folder{}, err
	}
	return liveAPI{api}.GetFolder(ctx, name)
}

func (a lazyAPI) GetOrg(ctx context.Context, name string) (Org, error) {
	api, err := a.l.Get(ctx)
	if err != nil {
		return Org{}, err
	}
	return liveAPI{api}.GetOrg(ctx, name)
}

func (a lazyAPI) TestPermissions(ctx context.Context, scopeID string, perms []string) ([]string, error) {
	api, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{api}.TestPermissions(ctx, scopeID, perms)
}

var _ Client = lazyAPI{}

func (a liveAPI) SearchProjects(ctx context.Context) ([]Project, error) {
	it := a.Projects.SearchProjects(ctx, &resourcemanagerpb.SearchProjectsRequest{})
	var out []Project
	for {
		p, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if p.GetState() != resourcemanagerpb.Project_ACTIVE {
			continue
		}
		id := p.GetProjectId()
		if id == "" {
			continue
		}
		out = append(out, Project{
			ID:          id,
			Number:      strings.TrimPrefix(p.GetName(), "projects/"),
			DisplayName: p.GetDisplayName(),
			Parent:      p.GetParent(),
			Created:     asTime(p.GetCreateTime()),
			Labels:      maps.Clone(p.GetLabels()),
		})
	}
}

func (a liveAPI) GetFolder(ctx context.Context, name string) (Folder, error) {
	f, err := a.Folders.GetFolder(ctx, &resourcemanagerpb.GetFolderRequest{Name: name})
	if err != nil {
		return Folder{}, err
	}
	return Folder{
		Name:        f.GetName(),
		DisplayName: f.GetDisplayName(),
		Parent:      f.GetParent(),
		Created:     asTime(f.GetCreateTime()),
	}, nil
}

func (a liveAPI) GetOrg(ctx context.Context, name string) (Org, error) {
	o, err := a.Orgs.GetOrganization(ctx, &resourcemanagerpb.GetOrganizationRequest{Name: name})
	if err != nil {
		return Org{}, err
	}
	return Org{
		Name:        o.GetName(),
		DisplayName: o.GetDisplayName(),
		Created:     asTime(o.GetCreateTime()),
	}, nil
}

// asTime converts a protobuf timestamp, keeping the zero time for nil.
func asTime(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}

// TestPermissions asks Resource Manager which of perms the caller holds on
// scopeID (organizations/…, folders/…, or projects/…).
func (a liveAPI) TestPermissions(ctx context.Context, scopeID string, perms []string) ([]string, error) {
	req := &iampb.TestIamPermissionsRequest{Resource: scopeID, Permissions: perms}
	var (
		resp *iampb.TestIamPermissionsResponse
		err  error
	)
	switch model.ScopeKind(scopeID) {
	case model.KindOrg:
		resp, err = a.Orgs.TestIamPermissions(ctx, req)
	case model.KindFolder:
		resp, err = a.Folders.TestIamPermissions(ctx, req)
	case model.KindProject:
		resp, err = a.Projects.TestIamPermissions(ctx, req)
	default:
		return nil, fmt.Errorf("crm: not a CRM scope: %q", scopeID)
	}
	if err != nil {
		return nil, err
	}
	return resp.GetPermissions(), nil
}
