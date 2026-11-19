package iam

import (
	"context"
	"net"
	"strconv"
	"sync"
	"testing"

	"cloud.google.com/go/iam/admin/apiv1/adminpb"
	iampb "cloud.google.com/go/iam/apiv1/iampb"
	"cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/crm"
	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
)

// fakeGoogle is an in-process Resource Manager and IAM Admin. Its
// services serve the RPCs the live adapters call from canned data,
// record each request they receive, and fail every RPC with err when err
// is set.
type fakeGoogle struct {
	policies map[string]*iampb.Policy // by resource name
	projects map[string]*resourcemanagerpb.Project
	folders  map[string]*resourcemanagerpb.Folder
	orgs     map[string]*resourcemanagerpb.Organization

	// accountPages and rolePages are ListServiceAccounts' and ListRoles'
	// result pages. Page i is served for the page token strconv.Itoa(i),
	// and page 0 for an empty token.
	accountPages [][]*adminpb.ServiceAccount
	rolePages    [][]*adminpb.Role
	keys         map[string][]*adminpb.ServiceAccountKey // by account name
	roles        map[string]*adminpb.Role                // GetRole, by name

	err error

	mu   sync.Mutex
	reqs []received
}

// received is one request the fake served, tagged with the service it
// arrived on.
type received struct {
	service string // "projects", "folders", "organizations", or "iam"
	req     proto.Message
}

func (f *fakeGoogle) record(service string, m proto.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, received{service, proto.Clone(m)})
}

// requests returns the requests served so far, in order.
func (f *fakeGoogle) requests() []received {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]received(nil), f.reqs...)
}

func (f *fakeGoogle) getIamPolicy(service string, req *iampb.GetIamPolicyRequest) (*iampb.Policy, error) {
	f.record(service, req)
	if f.err != nil {
		return nil, f.err
	}
	if p, ok := f.policies[req.GetResource()]; ok {
		return p, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s not found", req.GetResource())
}

// page serves page token tok of pages, returning its items and the next
// page's token ("" after the last page).
func page[T any](pages [][]T, tok string) ([]T, string, error) {
	i := 0
	if tok != "" {
		n, err := strconv.Atoi(tok)
		if err != nil || n < 0 || n >= len(pages) {
			return nil, "", status.Errorf(codes.InvalidArgument, "bad page token %q", tok)
		}
		i = n
	}
	var items []T
	if i < len(pages) {
		items = pages[i]
	}
	next := ""
	if i+1 < len(pages) {
		next = strconv.Itoa(i + 1)
	}
	return items, next, nil
}

func found[T any](f *fakeGoogle, m map[string]T, name string) (T, error) {
	var zero T
	if f.err != nil {
		return zero, f.err
	}
	if v, ok := m[name]; ok {
		return v, nil
	}
	return zero, status.Errorf(codes.NotFound, "%s not found", name)
}

type rmProjects struct {
	resourcemanagerpb.UnimplementedProjectsServer
	f *fakeGoogle
}

func (s rmProjects) GetProject(_ context.Context, req *resourcemanagerpb.GetProjectRequest) (*resourcemanagerpb.Project, error) {
	s.f.record("projects", req)
	return found(s.f, s.f.projects, req.GetName())
}

func (s rmProjects) GetIamPolicy(_ context.Context, req *iampb.GetIamPolicyRequest) (*iampb.Policy, error) {
	return s.f.getIamPolicy("projects", req)
}

type rmFolders struct {
	resourcemanagerpb.UnimplementedFoldersServer
	f *fakeGoogle
}

func (s rmFolders) GetFolder(_ context.Context, req *resourcemanagerpb.GetFolderRequest) (*resourcemanagerpb.Folder, error) {
	s.f.record("folders", req)
	return found(s.f, s.f.folders, req.GetName())
}

func (s rmFolders) GetIamPolicy(_ context.Context, req *iampb.GetIamPolicyRequest) (*iampb.Policy, error) {
	return s.f.getIamPolicy("folders", req)
}

type rmOrgs struct {
	resourcemanagerpb.UnimplementedOrganizationsServer
	f *fakeGoogle
}

func (s rmOrgs) GetOrganization(_ context.Context, req *resourcemanagerpb.GetOrganizationRequest) (*resourcemanagerpb.Organization, error) {
	s.f.record("organizations", req)
	return found(s.f, s.f.orgs, req.GetName())
}

func (s rmOrgs) GetIamPolicy(_ context.Context, req *iampb.GetIamPolicyRequest) (*iampb.Policy, error) {
	return s.f.getIamPolicy("organizations", req)
}

type iamAdmin struct {
	adminpb.UnimplementedIAMServer
	f *fakeGoogle
}

func (s iamAdmin) ListServiceAccounts(_ context.Context, req *adminpb.ListServiceAccountsRequest) (*adminpb.ListServiceAccountsResponse, error) {
	s.f.record("iam", req)
	if s.f.err != nil {
		return nil, s.f.err
	}
	items, next, err := page(s.f.accountPages, req.GetPageToken())
	if err != nil {
		return nil, err
	}
	return &adminpb.ListServiceAccountsResponse{Accounts: items, NextPageToken: next}, nil
}

func (s iamAdmin) ListServiceAccountKeys(_ context.Context, req *adminpb.ListServiceAccountKeysRequest) (*adminpb.ListServiceAccountKeysResponse, error) {
	s.f.record("iam", req)
	keys, err := found(s.f, s.f.keys, req.GetName())
	if err != nil {
		return nil, err
	}
	return &adminpb.ListServiceAccountKeysResponse{Keys: keys}, nil
}

func (s iamAdmin) ListRoles(_ context.Context, req *adminpb.ListRolesRequest) (*adminpb.ListRolesResponse, error) {
	s.f.record("iam", req)
	if s.f.err != nil {
		return nil, s.f.err
	}
	items, next, err := page(s.f.rolePages, req.GetPageToken())
	if err != nil {
		return nil, err
	}
	return &adminpb.ListRolesResponse{Roles: items, NextPageToken: next}, nil
}

func (s iamAdmin) GetRole(_ context.Context, req *adminpb.GetRoleRequest) (*adminpb.Role, error) {
	s.f.record("iam", req)
	return found(s.f, s.f.roles, req.GetName())
}

// serve starts f on a loopback gRPC server and returns the live policy
// and admin APIs, built the way the catalog builds them, with real
// clients that talk to it. All are torn down when t ends.
func serve(t *testing.T, f *fakeGoogle) (API, AdminAPI) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()
	resourcemanagerpb.RegisterProjectsServer(s, rmProjects{f: f})
	resourcemanagerpb.RegisterFoldersServer(s, rmFolders{f: f})
	resourcemanagerpb.RegisterOrganizationsServer(s, rmOrgs{f: f})
	adminpb.RegisterIAMServer(s, &iamAdmin{f: f}) // its Unimplemented methods have pointer receivers
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)

	opts := []option.ClientOption{
		option.WithEndpoint(lis.Addr().String()),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	}
	var g lazy.Group
	t.Cleanup(func() { _ = g.Close() })
	return NewLazyAPI(crm.NewLazyClients(&g, opts...)), NewLazyAdmin(&g, opts...)
}
