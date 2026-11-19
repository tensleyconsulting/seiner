package workspace

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	customer string
	users    []User
	groups   []Group
	members  map[string][]Member
	// err is returned from every method when set; listErr from the lists only.
	err, listErr error
	calls        *int
}

func (f fakeAPI) count() {
	if f.calls != nil {
		*f.calls++
	}
}

func (f fakeAPI) Customer(context.Context, string) (string, error) {
	f.count()
	if f.err != nil {
		return "", f.err
	}
	return f.customer, nil
}

func (f fakeAPI) ListUsers(_ context.Context, customer string, limit int) ([]User, bool, error) {
	f.count()
	if f.err != nil || f.listErr != nil {
		return nil, false, errors.Join(f.err, f.listErr)
	}
	if customer != f.customer {
		return nil, false, fmt.Errorf("customer %q", customer)
	}
	return f.users, false, nil
}

func (f fakeAPI) ListGroups(_ context.Context, customer string, limit int) ([]Group, bool, error) {
	f.count()
	if f.err != nil || f.listErr != nil {
		return nil, false, errors.Join(f.err, f.listErr)
	}
	if customer != f.customer {
		return nil, false, fmt.Errorf("customer %q", customer)
	}
	return f.groups, false, nil
}

func (f fakeAPI) ListMembers(_ context.Context, group string, limit int) ([]Member, bool, error) {
	f.count()
	if f.err != nil || f.listErr != nil {
		return nil, false, errors.Join(f.err, f.listErr)
	}
	return f.members[group], len(f.members[group]) >= limit, nil
}

var (
	created = time.Date(2023, 4, 1, 9, 0, 0, 0, time.UTC)
	login   = time.Date(2026, 11, 2, 8, 30, 0, 0, time.UTC)
	alice   = User{ID: "1001", Email: "alice@example.com", FullName: "Alice Liddell", OrgUnit: "/Engineering", Admin: true, Enrolled2SV: true, Enforced2SV: true, LastLogin: login, Created: created}
	bob     = User{ID: "1002", Email: "bob@example.com", OrgUnit: "/", Suspended: true}
	eng     = Group{ID: "03abc", Email: "eng@example.com", DisplayName: "Engineering", Description: "All engineers", Types: []string{"discussion forum", "security"}, Created: created, Updated: login}
)

func TestListScopes(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"projects/p", "folders/1", ""} {
		calls := 0
		api := fakeAPI{customer: "C01", users: []User{alice}, groups: []Group{eng}, calls: &calls}
		for _, p := range []model.ResourceProvider{NewUsers(api), NewGroups(api)} {
			got, err := p.List(context.Background(), scope)
			if got != nil || err != nil {
				t.Errorf("%s List(%q) = %v, %v; want nothing", p.Service(), scope, got, err)
			}
		}
		if calls != 0 {
			t.Errorf("List(%q) called the API %d times", scope, calls)
		}
	}
}

func TestListUsers(t *testing.T) {
	t.Parallel()
	got, err := NewUsers(fakeAPI{customer: "C01", users: []User{alice, bob}}).List(context.Background(), "organizations/1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d users", len(got))
	}
	a := got[0]
	if a.ID != "organizations/1/users/1001" || a.Name != "alice@example.com" || a.Service != model.ServiceWorkspaceUser ||
		a.ScopeID != "organizations/1" || a.Location != "global" || !a.Created.Equal(created) {
		t.Errorf("alice = %+v", a)
	}
	if a.State != "ACTIVE" || a.Status != model.StatusHealthy || a.Badge != "ADMIN" {
		t.Errorf("alice state %q status %q badge %q", a.State, a.Status, a.Badge)
	}
	want := []model.Field{
		{Key: "fullName", Value: "Alice Liddell"},
		{Key: model.MetaOrgUnit, Value: "/Engineering"},
		{Key: "admin", Value: "super admin"},
		{Key: "twoStepVerification", Value: "enrolled, enforced"},
		{Key: "lastLogin", Value: "2026-11-02 08:30 UTC"},
		{Key: "id", Value: "1001"},
	}
	if !slices.Equal(a.Fields, want) {
		t.Errorf("alice fields\n%v\nwant\n%v", a.Fields, want)
	}
	if err := a.Validate(); err != nil {
		t.Error(err)
	}
	b := got[1]
	if b.State != "SUSPENDED" || b.Status != model.StatusWarning || b.Badge != "" ||
		b.Metadata["lastLogin"] != "never" || b.Metadata["twoStepVerification"] != "not enrolled" || b.Metadata["admin"] != "" {
		t.Errorf("bob = %+v", b)
	}
}

func TestUserState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		u      User
		state  string
		status model.ResourceStatus
	}{
		{User{ID: "1"}, "ACTIVE", model.StatusHealthy},
		{User{ID: "1", Suspended: true}, "SUSPENDED", model.StatusWarning},
		{User{ID: "1", Archived: true}, "ARCHIVED", model.StatusWarning},
		{User{ID: "1", Archived: true, Suspended: true}, "ARCHIVED", model.StatusWarning},
	}
	for _, tc := range cases {
		r := userResource("organizations/1", tc.u)
		if r.State != tc.state || r.Status != tc.status {
			t.Errorf("%+v: state %q status %q, want %q %q", tc.u, r.State, r.Status, tc.state, tc.status)
		}
	}
	if r := userResource("organizations/1", User{ID: "1", Delegated: true}); r.Metadata["admin"] != "delegated admin" || r.Badge != "" {
		t.Errorf("delegated admin = %+v", r)
	}
	if r := userResource("organizations/1", User{ID: "1"}); r.Metadata[model.MetaOrgUnit] != "/" {
		t.Errorf("no org unit = %q, want /", r.Metadata[model.MetaOrgUnit])
	}
}

func TestListGroups(t *testing.T) {
	t.Parallel()
	got, err := NewGroups(fakeAPI{customer: "C01", groups: []Group{eng, {ID: "04def", Email: "ops@example.com", DisplayName: "ops@example.com"}}}).List(context.Background(), "organizations/1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d groups", len(got))
	}
	g := got[0]
	if g.ID != "organizations/1/groups/03abc" || g.Name != "eng@example.com" || g.Service != model.ServiceWorkspaceGroup ||
		g.ScopeID != "organizations/1" || g.Description != "All engineers" || !g.Created.Equal(created) {
		t.Errorf("eng = %+v", g)
	}
	want := []model.Field{
		{Key: "displayName", Value: "Engineering"},
		{Key: "groupType", Value: "discussion forum, security"},
		{Key: "updated", Value: "2026-11-02 08:30 UTC"},
		{Key: "id", Value: "03abc"},
	}
	if !slices.Equal(g.Fields, want) {
		t.Errorf("eng fields\n%v\nwant\n%v", g.Fields, want)
	}
	if f, ok := g.InlineFacet(); !ok || f.ID != model.FacetMembers {
		t.Errorf("eng facets %+v", g.Facets)
	}
	if _, ok := got[1].Metadata["displayName"]; ok {
		t.Error("a display name equal to the email is repeated")
	}
}

func TestListRelated(t *testing.T) {
	t.Parallel()
	api := fakeAPI{customer: "C01", members: map[string][]Member{"03abc": {
		{ID: "m1", Email: "alice@example.com", Type: "USER", Roles: []string{"MEMBER", "OWNER"}, Created: created},
		{ID: "m2", Email: "ops@example.com", Type: "GROUP", Roles: []string{"MEMBER"}},
	}}}
	p := NewGroups(api)
	group := groupResource("organizations/1", eng)
	got, truncated, err := p.ListRelated(context.Background(), group, model.FacetMembers)
	if err != nil || truncated {
		t.Fatalf("ListRelated = %v, %v", truncated, err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d members", len(got))
	}
	m := got[0]
	if m.ID != "organizations/1/groups/03abc/memberships/m1" || m.Name != "alice@example.com" || m.Service != model.ServiceWorkspaceMember ||
		m.ParentID != group.ID || m.FacetID != model.FacetMembers || m.ScopeID != "organizations/1" || m.Badge != "OWNER" || !m.Created.Equal(created) {
		t.Errorf("member = %+v", m)
	}
	if want := []model.Field{{Key: "type", Value: "USER"}, {Key: "roles", Value: "MEMBER, OWNER"}}; !slices.Equal(m.Fields, want) {
		t.Errorf("member fields %v", m.Fields)
	}
	if got[1].Badge != "" {
		t.Errorf("plain member badge %q", got[1].Badge)
	}
	if err := m.Validate(); err != nil {
		t.Error(err)
	}

	for _, tc := range []struct {
		name   string
		parent model.Resource
		facet  string
	}{
		{"another service", model.Resource{ID: group.ID, Service: model.ServiceWorkspaceUser}, model.FacetMembers},
		{"another facet", group, model.FacetUsers},
		{"malformed id", model.Resource{ID: "organizations/1/users/03abc", Service: model.ServiceWorkspaceGroup}, model.FacetMembers},
	} {
		got, truncated, err := p.ListRelated(context.Background(), tc.parent, tc.facet)
		if got != nil || truncated || err != nil {
			t.Errorf("%s: ListRelated = %v, %v, %v", tc.name, got, truncated, err)
		}
	}
}

func TestListRelatedTruncated(t *testing.T) {
	t.Parallel()
	many := make([]Member, maxMembers)
	for i := range many {
		many[i] = Member{ID: fmt.Sprint(i)}
	}
	_, truncated, err := NewGroups(fakeAPI{members: map[string][]Member{"03abc": many}}).
		ListRelated(context.Background(), groupResource("organizations/1", eng), model.FacetMembers)
	if err != nil || !truncated {
		t.Errorf("truncated = %v, %v", truncated, err)
	}
}

// A denial hides the rows and reports nothing; any other error is the
// provider's error, unchanged.
func TestErrors(t *testing.T) {
	t.Parallel()
	denied := fmt.Errorf("%w: googleapi: Error 403: Not Authorized to access this resource/api", ErrDenied)
	boom := errors.New("boom")
	group := groupResource("organizations/1", eng)
	cases := []struct {
		name    string
		api     fakeAPI
		wantErr error
	}{
		{"customer denied", fakeAPI{err: denied}, nil},
		{"list denied", fakeAPI{customer: "C01", listErr: denied}, nil},
		{"customer fails", fakeAPI{err: boom}, boom},
		{"list fails", fakeAPI{customer: "C01", listErr: boom}, boom},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, p := range []model.ResourceProvider{NewUsers(tc.api), NewGroups(tc.api)} {
				got, err := p.List(context.Background(), "organizations/1")
				if got != nil || !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
					t.Errorf("%s List = %v, %v; want nil, %v", p.Service(), got, err, tc.wantErr)
				}
			}
			if tc.api.customer == "" {
				return // ListRelated does not read the customer
			}
			got, _, err := NewGroups(tc.api).ListRelated(context.Background(), group, model.FacetMembers)
			if got != nil || !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Errorf("ListRelated = %v, %v; want nil, %v", got, err, tc.wantErr)
			}
		})
	}
}

func TestNoCustomer(t *testing.T) {
	t.Parallel()
	calls := 0
	got, err := NewUsers(fakeAPI{calls: &calls}).List(context.Background(), "organizations/1")
	if got != nil || err != nil || calls != 1 {
		t.Errorf("List = %v, %v after %d calls; want nothing after the customer read", got, err, calls)
	}
}

func TestCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	api := fakeAPI{customer: "C01"}
	if _, err := NewUsers(api).List(ctx, "organizations/1"); !errors.Is(err, context.Canceled) {
		t.Errorf("Users.List = %v", err)
	}
	if _, _, err := NewGroups(api).ListRelated(ctx, groupResource("organizations/1", eng), model.FacetMembers); !errors.Is(err, context.Canceled) {
		t.Errorf("ListRelated = %v", err)
	}
}
