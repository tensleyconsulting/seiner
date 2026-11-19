package workspace

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	admin "google.golang.org/api/admin/directory/v1"
	ci "google.golang.org/api/cloudidentity/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDeny(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"not an admin", &googleapi.Error{Code: http.StatusForbidden, Message: "Not Authorized to access this resource/api"}, true},
		{"api disabled", &googleapi.Error{Code: http.StatusForbidden, Errors: []googleapi.ErrorItem{{Reason: "accessNotConfigured"}}}, true},
		{"unknown customer", &googleapi.Error{Code: http.StatusNotFound}, true},
		{"rate limited", &googleapi.Error{Code: http.StatusForbidden, Errors: []googleapi.ErrorItem{{Reason: "userRateLimitExceeded"}}}, false},
		{"quota", &googleapi.Error{Code: http.StatusForbidden, Errors: []googleapi.ErrorItem{{Reason: "quotaExceeded"}}}, false},
		{"signed out", &googleapi.Error{Code: http.StatusUnauthorized}, false},
		{"outage", &googleapi.Error{Code: http.StatusServiceUnavailable}, false},
		{"wrapped", fmt.Errorf("list: %w", &googleapi.Error{Code: http.StatusForbidden}), true},
		{"grpc denied", status.Error(codes.PermissionDenied, "no"), true},
		{"grpc not found", status.Error(codes.NotFound, "no"), true},
		{"grpc unauthenticated", status.Error(codes.Unauthenticated, "no"), false},
		{"plain", errors.New("boom"), false},
	}
	for _, tc := range cases {
		err := deny(tc.err)
		if got := errors.Is(err, ErrDenied); got != tc.want {
			t.Errorf("%s: denied = %v, want %v", tc.name, got, tc.want)
		}
		if !errors.Is(err, tc.err) {
			t.Errorf("%s: deny lost the Google error", tc.name)
		}
	}
}

func TestFromUser(t *testing.T) {
	t.Parallel()
	got := fromUser(&admin.User{
		Id: "1001", PrimaryEmail: "alice@example.com", Name: &admin.UserName{FullName: "Alice Liddell"},
		OrgUnitPath: "/Engineering", IsAdmin: true, IsEnrolledIn2Sv: true,
		LastLoginTime: "2026-11-02T08:30:00.000Z", CreationTime: "2023-04-01T09:00:00.000Z",
	})
	want := User{ID: "1001", Email: "alice@example.com", FullName: "Alice Liddell", OrgUnit: "/Engineering", Admin: true, Enrolled2SV: true,
		LastLogin: time.Date(2026, 11, 2, 8, 30, 0, 0, time.UTC), Created: time.Date(2023, 4, 1, 9, 0, 0, 0, time.UTC)}
	if !got.LastLogin.Equal(want.LastLogin) || !got.Created.Equal(want.Created) {
		t.Errorf("times %v %v", got.LastLogin, got.Created)
	}
	got.LastLogin, got.Created, want.LastLogin, want.Created = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	if got != want {
		t.Errorf("fromUser = %+v", got)
	}
	never := fromUser(&admin.User{Id: "1", LastLoginTime: "1970-01-01T00:00:00.000Z"})
	if !never.LastLogin.IsZero() || never.FullName != "" {
		t.Errorf("never signed in = %+v", never)
	}
}

func TestFromGroup(t *testing.T) {
	t.Parallel()
	got := fromGroup(&ci.Group{
		Name: "groups/03abc", GroupKey: &ci.EntityKey{Id: "eng@example.com"}, DisplayName: "Engineering",
		Labels: map[string]string{
			"example.com/custom":                                   "x",
			"cloudidentity.googleapis.com/groups.security":         "",
			"cloudidentity.googleapis.com/groups.discussion_forum": "",
		},
		CreateTime: "2023-04-01T09:00:00Z",
	})
	if got.ID != "03abc" || got.Email != "eng@example.com" || got.DisplayName != "Engineering" || got.Created.IsZero() {
		t.Errorf("fromGroup = %+v", got)
	}
	if want := []string{"discussion forum", "security"}; !slices.Equal(got.Types, want) {
		t.Errorf("types %v, want %v", got.Types, want)
	}
}

func TestFromMember(t *testing.T) {
	t.Parallel()
	got := fromMember(&ci.Membership{
		Name: "groups/03abc/memberships/m1", PreferredMemberKey: &ci.EntityKey{Id: "alice@example.com"}, Type: "USER",
		Roles: []*ci.MembershipRole{{Name: "MEMBER"}, nil, {Name: "OWNER"}},
	})
	if got.ID != "m1" || got.Email != "alice@example.com" || got.Type != "USER" || !slices.Equal(got.Roles, []string{"MEMBER", "OWNER"}) {
		t.Errorf("fromMember = %+v", got)
	}
}
