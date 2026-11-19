package workspace

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	resourcemanager "cloud.google.com/go/resourcemanager/apiv3"
	"cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	admin "google.golang.org/api/admin/directory/v1"
	ci "google.golang.org/api/cloudidentity/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
)

// userPartial is the partial response a user list asks for: account state
// only, so personal details never leave Google.
const userPartial = "nextPageToken,users(id,primaryEmail,name/fullName,orgUnitPath,suspended,archived," +
	"isAdmin,isDelegatedAdmin,isEnrolledIn2Sv,isEnforcedIn2Sv,lastLoginTime,creationTime)"

// groupTypes names the Cloud Identity labels that say what a group is.
var groupTypes = map[string]string{
	"cloudidentity.googleapis.com/groups.discussion_forum": "discussion forum",
	"cloudidentity.googleapis.com/groups.security":         "security",
	"cloudidentity.googleapis.com/groups.dynamic":          "dynamic",
	"cloudidentity.googleapis.com/groups.locked":           "locked",
	"system/groups/external":                               "external",
}

// cloudPlatform is the only scope the clients ask for. The Directory
// client's default scopes are every admin.directory scope, write scopes
// included; cloud-platform reads users and groups, which is all this
// package lists.
const cloudPlatform = "https://www.googleapis.com/auth/cloud-platform"

// errLimit stops paging once a list has its limit.
var errLimit = errors.New("limit reached")

type liveAPI struct {
	orgs *resourcemanager.OrganizationsClient
	dir  *admin.Service
	ci   *ci.Service
}

// NewLazy defers client construction to the first call, so a slow or
// failing constructor cannot block startup; the failure surfaces as the
// provider's ProviderError instead. Users and Groups share the API it
// returns, so each client builds once. g closes the Resource Manager
// client once built.
func NewLazy(g *lazy.Group, opts ...option.ClientOption) API {
	opts = append([]option.ClientOption{option.WithScopes(cloudPlatform)}, opts...)
	return &lazyAPI{
		orgs: lazy.NewIn(g, func(ctx context.Context) (*resourcemanager.OrganizationsClient, error) {
			return resourcemanager.NewOrganizationsClient(ctx, opts...)
		}),
		dir: lazy.New(func(ctx context.Context) (*admin.Service, error) { return admin.NewService(ctx, opts...) }),
		ci:  lazy.New(func(ctx context.Context) (*ci.Service, error) { return ci.NewService(ctx, opts...) }),
	}
}

// lazyAPI builds each client on first use. An organization's customer
// does not change, so it is read once per session.
type lazyAPI struct {
	orgs *lazy.Value[*resourcemanager.OrganizationsClient]
	dir  *lazy.Value[*admin.Service]
	ci   *lazy.Value[*ci.Service]

	mu        sync.Mutex
	customers map[string]string
}

func (a *lazyAPI) Customer(ctx context.Context, org string) (string, error) {
	a.mu.Lock()
	c, ok := a.customers[org]
	a.mu.Unlock()
	if ok {
		return c, nil
	}
	oc, err := a.orgs.Get(ctx)
	if err != nil {
		return "", err
	}
	c, err = liveAPI{orgs: oc}.Customer(ctx, org)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	if a.customers == nil {
		a.customers = map[string]string{}
	}
	a.customers[org] = c
	a.mu.Unlock()
	return c, nil
}

func (a *lazyAPI) ListUsers(ctx context.Context, customer string, limit int) ([]User, bool, error) {
	d, err := a.dir.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveAPI{dir: d}.ListUsers(ctx, customer, limit)
}

func (a *lazyAPI) ListGroups(ctx context.Context, customer string, limit int) ([]Group, bool, error) {
	c, err := a.ci.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveAPI{ci: c}.ListGroups(ctx, customer, limit)
}

func (a *lazyAPI) ListMembers(ctx context.Context, group string, limit int) ([]Member, bool, error) {
	c, err := a.ci.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveAPI{ci: c}.ListMembers(ctx, group, limit)
}

var (
	_ API = (*lazyAPI)(nil)
	_ API = liveAPI{}
)

func (a liveAPI) Customer(ctx context.Context, org string) (string, error) {
	o, err := a.orgs.GetOrganization(ctx, &resourcemanagerpb.GetOrganizationRequest{Name: org})
	if err != nil {
		return "", deny(err)
	}
	return o.GetDirectoryCustomerId(), nil
}

func (a liveAPI) ListUsers(ctx context.Context, customer string, limit int) ([]User, bool, error) {
	var out []User
	truncated := false
	err := a.dir.Users.List().Customer(customer).Projection("basic").MaxResults(500).
		Fields(userPartial).Context(ctx).Pages(ctx, func(page *admin.Users) error {
		for _, u := range page.Users {
			if len(out) == limit {
				truncated = true
				return errLimit
			}
			out = append(out, fromUser(u))
		}
		return nil
	})
	if err != nil && !errors.Is(err, errLimit) {
		return nil, false, deny(err)
	}
	return out, truncated, nil
}

func (a liveAPI) ListGroups(ctx context.Context, customer string, limit int) ([]Group, bool, error) {
	var out []Group
	truncated := false
	err := a.ci.Groups.List().Parent("customers/"+customer).View("FULL").PageSize(500).
		Context(ctx).Pages(ctx, func(page *ci.ListGroupsResponse) error {
		for _, g := range page.Groups {
			if len(out) == limit {
				truncated = true
				return errLimit
			}
			out = append(out, fromGroup(g))
		}
		return nil
	})
	if err != nil && !errors.Is(err, errLimit) {
		return nil, false, deny(err)
	}
	return out, truncated, nil
}

func (a liveAPI) ListMembers(ctx context.Context, group string, limit int) ([]Member, bool, error) {
	var out []Member
	truncated := false
	err := a.ci.Groups.Memberships.List("groups/"+group).View("FULL").PageSize(500).
		Context(ctx).Pages(ctx, func(page *ci.ListMembershipsResponse) error {
		for _, m := range page.Memberships {
			if len(out) == limit {
				truncated = true
				return errLimit
			}
			out = append(out, fromMember(m))
		}
		return nil
	})
	if err != nil && !errors.Is(err, errLimit) {
		return nil, false, deny(err)
	}
	return out, truncated, nil
}

// deny marks err with ErrDenied when Google refused to show the
// directory: a 403 that is not a rate limit, or a 404 for a customer or
// group the caller cannot see. Other errors return unchanged.
func deny(err error) error {
	if denied(err) {
		return fmt.Errorf("%w: %w", ErrDenied, err)
	}
	return err
}

func denied(err error) bool {
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		switch gerr.Code {
		case http.StatusNotFound:
			return true
		case http.StatusForbidden:
			// The Directory API reports its rate limits as 403s.
			for _, e := range gerr.Errors {
				if r := strings.ToLower(e.Reason); strings.Contains(r, "ratelimit") || strings.Contains(r, "quota") {
					return false
				}
			}
			return true
		}
		return false
	}
	if s, ok := status.FromError(err); ok {
		return s.Code() == codes.PermissionDenied || s.Code() == codes.NotFound
	}
	return false
}

func fromUser(u *admin.User) User {
	out := User{
		ID:          u.Id,
		Email:       u.PrimaryEmail,
		OrgUnit:     u.OrgUnitPath,
		Suspended:   u.Suspended,
		Archived:    u.Archived,
		Admin:       u.IsAdmin,
		Delegated:   u.IsDelegatedAdmin,
		Enrolled2SV: u.IsEnrolledIn2Sv,
		Enforced2SV: u.IsEnforcedIn2Sv,
		Created:     asTime(u.CreationTime),
	}
	if u.Name != nil {
		out.FullName = u.Name.FullName
	}
	// A user who never signed in has the Unix epoch.
	if t := asTime(u.LastLoginTime); t.Year() > 1970 {
		out.LastLogin = t
	}
	return out
}

func fromGroup(g *ci.Group) Group {
	out := Group{
		ID:          strings.TrimPrefix(g.Name, "groups/"),
		DisplayName: g.DisplayName,
		Description: g.Description,
		Created:     asTime(g.CreateTime),
		Updated:     asTime(g.UpdateTime),
	}
	if g.GroupKey != nil {
		out.Email = g.GroupKey.Id
	}
	for k := range g.Labels {
		if t, ok := groupTypes[k]; ok {
			out.Types = append(out.Types, t)
		}
	}
	slices.Sort(out.Types)
	return out
}

func fromMember(m *ci.Membership) Member {
	out := Member{
		ID:      m.Name[strings.LastIndex(m.Name, "/")+1:],
		Type:    m.Type,
		Created: asTime(m.CreateTime),
	}
	if m.PreferredMemberKey != nil {
		out.Email = m.PreferredMemberKey.Id
	}
	for _, r := range m.Roles {
		if r != nil && r.Name != "" {
			out.Roles = append(out.Roles, r.Name)
		}
	}
	return out
}

// asTime parses an RFC 3339 timestamp, keeping the zero time for "" or a
// value that does not parse.
func asTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
