package workspace

import (
	"context"
	"errors"
	"time"
)

// ErrDenied marks a call Google refused because the caller may not read
// the directory: no Workspace admin role, an API not enabled in the quota
// project, or a customer the caller cannot see. Workspace access is not
// granted through Cloud IAM, so most callers have none, and the explorer
// shows nothing rather than an error.
var ErrDenied = errors.New("workspace: directory not readable")

// User is the Google-free slice of a Directory API user. It keeps only
// account state: no phone numbers, addresses, or recovery details.
type User struct {
	ID          string // the directory's unique id
	Email       string // primary email
	FullName    string
	OrgUnit     string // org unit path, "/" for the root
	Suspended   bool
	Archived    bool
	Admin       bool // super admin
	Delegated   bool // holds a delegated admin role
	Enrolled2SV bool
	Enforced2SV bool
	LastLogin   time.Time // zero when the user never signed in
	Created     time.Time
}

// Group is the Google-free slice of a Cloud Identity group.
type Group struct {
	ID          string // the id in groups/{id}
	Email       string
	DisplayName string
	Description string
	Types       []string // "security", "discussion forum", …, from the group's labels
	Created     time.Time
	Updated     time.Time
}

// Member is one direct membership of a group.
type Member struct {
	ID      string   // the id in groups/{group}/memberships/{id}
	Email   string   // the member's key, an email for users and groups
	Type    string   // USER, GROUP, SERVICE_ACCOUNT, SHARED_DRIVE, OTHER
	Roles   []string // MEMBER, MANAGER, OWNER
	Created time.Time
}

// API is the Workspace ↔ Resource boundary. Tests inject a fake; live
// wraps the Resource Manager, Directory, and Cloud Identity clients.
// Each list reads at most limit items and reports truncated when more
// exist.
type API interface {
	// Customer is the Workspace or Cloud Identity customer id that owns
	// org (organizations/123), "" if it has none.
	Customer(ctx context.Context, org string) (string, error)
	ListUsers(ctx context.Context, customer string, limit int) (users []User, truncated bool, err error)
	ListGroups(ctx context.Context, customer string, limit int) (groups []Group, truncated bool, err error)
	ListMembers(ctx context.Context, group string, limit int) (members []Member, truncated bool, err error)
}
