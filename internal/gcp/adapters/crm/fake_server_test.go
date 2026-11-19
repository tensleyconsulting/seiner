package crm

import (
	"context"
	"net"
	"strconv"
	"sync"
	"testing"

	iampb "cloud.google.com/go/iam/apiv1/iampb"
	"cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// fakeRM is an in-process Resource Manager. Its three services serve the
// RPCs the live adapter calls from canned data, record each request they
// receive, and fail every RPC with err when err is set.
type fakeRM struct {
	// pages are SearchProjects' result pages. Page i is served for the
	// page token strconv.Itoa(i), and page 0 for an empty token.
	pages   [][]*resourcemanagerpb.Project
	folders map[string]*resourcemanagerpb.Folder
	orgs    map[string]*resourcemanagerpb.Organization
	granted []string // the TestIamPermissions answer
	err     error

	mu   sync.Mutex
	reqs []received
}

// received is one request the fake served, tagged with the service it
// arrived on.
type received struct {
	service string // "projects", "folders", or "organizations"
	req     proto.Message
}

func (f *fakeRM) record(service string, m proto.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, received{service, proto.Clone(m)})
}

// requests returns the requests served so far, in order.
func (f *fakeRM) requests() []received {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]received(nil), f.reqs...)
}

func (f *fakeRM) testIamPermissions(service string, req *iampb.TestIamPermissionsRequest) (*iampb.TestIamPermissionsResponse, error) {
	f.record(service, req)
	if f.err != nil {
		return nil, f.err
	}
	return &iampb.TestIamPermissionsResponse{Permissions: f.granted}, nil
}

type rmProjects struct {
	resourcemanagerpb.UnimplementedProjectsServer
	f *fakeRM
}

func (s rmProjects) SearchProjects(_ context.Context, req *resourcemanagerpb.SearchProjectsRequest) (*resourcemanagerpb.SearchProjectsResponse, error) {
	s.f.record("projects", req)
	if s.f.err != nil {
		return nil, s.f.err
	}
	i := 0
	if tok := req.GetPageToken(); tok != "" {
		n, err := strconv.Atoi(tok)
		if err != nil || n < 0 || n >= len(s.f.pages) {
			return nil, status.Errorf(codes.InvalidArgument, "bad page token %q", tok)
		}
		i = n
	}
	resp := &resourcemanagerpb.SearchProjectsResponse{}
	if i < len(s.f.pages) {
		resp.Projects = s.f.pages[i]
	}
	if i+1 < len(s.f.pages) {
		resp.NextPageToken = strconv.Itoa(i + 1)
	}
	return resp, nil
}

func (s rmProjects) TestIamPermissions(_ context.Context, req *iampb.TestIamPermissionsRequest) (*iampb.TestIamPermissionsResponse, error) {
	return s.f.testIamPermissions("projects", req)
}

type rmFolders struct {
	resourcemanagerpb.UnimplementedFoldersServer
	f *fakeRM
}

func (s rmFolders) GetFolder(_ context.Context, req *resourcemanagerpb.GetFolderRequest) (*resourcemanagerpb.Folder, error) {
	s.f.record("folders", req)
	if s.f.err != nil {
		return nil, s.f.err
	}
	if fo, ok := s.f.folders[req.GetName()]; ok {
		return fo, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s not found", req.GetName())
}

func (s rmFolders) TestIamPermissions(_ context.Context, req *iampb.TestIamPermissionsRequest) (*iampb.TestIamPermissionsResponse, error) {
	return s.f.testIamPermissions("folders", req)
}

type rmOrgs struct {
	resourcemanagerpb.UnimplementedOrganizationsServer
	f *fakeRM
}

func (s rmOrgs) GetOrganization(_ context.Context, req *resourcemanagerpb.GetOrganizationRequest) (*resourcemanagerpb.Organization, error) {
	s.f.record("organizations", req)
	if s.f.err != nil {
		return nil, s.f.err
	}
	if o, ok := s.f.orgs[req.GetName()]; ok {
		return o, nil
	}
	return nil, status.Errorf(codes.NotFound, "%s not found", req.GetName())
}

func (s rmOrgs) TestIamPermissions(_ context.Context, req *iampb.TestIamPermissionsRequest) (*iampb.TestIamPermissionsResponse, error) {
	return s.f.testIamPermissions("organizations", req)
}

// serveRM starts f on a loopback gRPC server and returns a live API whose
// real clients talk to it. Both are torn down when t ends.
func serveRM(t *testing.T, f *fakeRM) liveAPI {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()
	resourcemanagerpb.RegisterProjectsServer(s, rmProjects{f: f})
	resourcemanagerpb.RegisterFoldersServer(s, rmFolders{f: f})
	resourcemanagerpb.RegisterOrganizationsServer(s, rmOrgs{f: f})
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)

	c, err := buildClients(context.Background(),
		option.WithEndpoint(lis.Addr().String()),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return liveAPI{c}
}
