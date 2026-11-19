package fake

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/workspace"
	"github.com/tensleyconsulting/seiner/internal/model"
)

const wsOrg = "organizations/example-org"

// workspaceFixtures is the example-org directory, mapped by the live
// providers: a few org units, a root org unit crowded enough to need
// letter buckets, and groups with their members.
func workspaceFixtures() []model.Resource {
	api := wsDirectory()
	ctx := context.Background()
	users, err := workspace.NewUsers(api).List(ctx, wsOrg)
	if err != nil {
		panic(err)
	}
	groups := workspace.NewGroups(api)
	gs, err := groups.List(ctx, wsOrg)
	if err != nil {
		panic(err)
	}
	out := slices.Concat(users, gs)
	for _, g := range gs {
		members, _, err := groups.ListRelated(ctx, g, model.FacetMembers)
		if err != nil {
			panic(err)
		}
		out = append(out, members...)
	}
	return out
}

// wsFirst names the people in the root org unit, one per row of the
// demo's letter buckets.
var wsFirst = []string{
	"aaron", "abby", "adele", "aiden", "amara", "ana", "arjun", "ava",
	"ben", "bianca", "boris", "caleb", "carmen", "chen", "chloe", "dario",
	"devi", "elena", "emil", "farah", "felix", "gabe", "greta", "hana",
	"hugo", "ines", "ivan", "jade", "jamal", "jonas", "julia", "kai",
	"kira", "lars", "leah", "lena", "luca", "maya", "mei", "milo",
	"nadia", "nico", "omar", "priya", "quinn", "rafael", "rosa", "sami",
	"sofia", "tariq", "uma", "vera", "wes", "yara", "zoe",
}

func wsDirectory() wsAPI {
	t := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			panic(err)
		}
		return v
	}
	users := []workspace.User{
		{ID: "100000000000000000001", Email: "alice@example.com", FullName: "Alice Liddell", OrgUnit: "/Engineering", Admin: true, Enrolled2SV: true, Enforced2SV: true, LastLogin: t("2026-11-05T08:12:00Z"), Created: t("2021-03-01T09:00:00Z")},
		{ID: "100000000000000000002", Email: "bob@example.com", FullName: "Bob Tanaka", OrgUnit: "/Engineering", Enrolled2SV: true, Enforced2SV: true, LastLogin: t("2026-11-04T17:40:00Z"), Created: t("2021-06-14T09:00:00Z")},
		{ID: "100000000000000000003", Email: "carol@example.com", FullName: "Carol Diaz", OrgUnit: "/Engineering/Platform", Delegated: true, Enrolled2SV: true, Enforced2SV: true, LastLogin: t("2026-11-05T07:55:00Z"), Created: t("2022-01-10T09:00:00Z")},
		{ID: "100000000000000000004", Email: "dave@example.com", FullName: "Dave Okafor", OrgUnit: "/Engineering/Platform", Enrolled2SV: true, Enforced2SV: true, LastLogin: t("2026-10-30T12:02:00Z"), Created: t("2022-09-05T09:00:00Z")},
		{ID: "100000000000000000005", Email: "dana@example.com", FullName: "Dana Weiss", OrgUnit: "/Finance", Enrolled2SV: true, LastLogin: t("2026-11-03T10:15:00Z"), Created: t("2021-03-01T09:30:00Z")},
		{ID: "100000000000000000006", Email: "erin@example.com", FullName: "Erin Walsh", OrgUnit: "/Sales", LastLogin: t("2026-09-22T14:00:00Z"), Created: t("2023-02-20T09:00:00Z")},
		{ID: "100000000000000000007", Email: "frank@example.com", FullName: "Frank Moreau", OrgUnit: "/Sales/EMEA", Enrolled2SV: true, LastLogin: t("2026-11-01T09:45:00Z"), Created: t("2023-05-08T09:00:00Z")},
		{ID: "100000000000000000008", Email: "gina.contractor@example.com", FullName: "Gina Russo", OrgUnit: "/Contractors", Suspended: true, LastLogin: t("2026-06-30T16:20:00Z"), Created: t("2025-01-06T09:00:00Z")},
		{ID: "100000000000000000009", Email: "hal.contractor@example.com", FullName: "Hal Berg", OrgUnit: "/Contractors", Created: t("2026-10-28T09:00:00Z")},
		{ID: "100000000000000000010", Email: "former.cfo@example.com", FullName: "Pat Lindqvist", OrgUnit: "/Finance", Archived: true, LastLogin: t("2025-12-19T11:00:00Z"), Created: t("2019-08-01T09:00:00Z")},
	}
	for i, first := range wsFirst {
		users = append(users, workspace.User{
			ID:          fmt.Sprintf("1000000000000000001%02d", i),
			Email:       first + "@example.com",
			FullName:    strings.ToUpper(first[:1]) + first[1:] + " Example",
			OrgUnit:     "/",
			Enrolled2SV: i%4 != 0,
			LastLogin:   t("2026-11-02T09:00:00Z").Add(-time.Duration(i) * 13 * time.Hour),
			Created:     t("2024-02-01T09:00:00Z").Add(time.Duration(i) * 72 * time.Hour),
		})
	}
	member := func(id, email, kind string, roles ...string) workspace.Member {
		if len(roles) == 0 {
			roles = []string{"MEMBER"}
		}
		return workspace.Member{ID: id, Email: email, Type: kind, Roles: roles, Created: t("2024-01-15T09:00:00Z")}
	}
	var everyone []workspace.Member
	for _, u := range users {
		if !u.Archived {
			everyone = append(everyone, member("m"+u.ID[len(u.ID)-3:], u.Email, "USER"))
		}
	}
	everyone[0].Roles = []string{"MEMBER", "OWNER"}
	return wsAPI{
		customer: "C0example12",
		users:    users,
		groups: []workspace.Group{
			{ID: "01d520gv4vjcrht", Email: "engineering@example.com", DisplayName: "Engineering", Description: "Every engineer", Types: []string{"discussion forum", "security"}, Created: t("2021-03-02T09:00:00Z"), Updated: t("2026-08-11T15:30:00Z")},
			{ID: "02grqrue1fe8m4k", Email: "contractors@example.com", DisplayName: "Contractors", Description: "External contractors on fixed-term access", Types: []string{"security"}, Created: t("2023-01-09T09:00:00Z"), Updated: t("2026-10-28T09:05:00Z")},
			{ID: "03x8tuzt1hmd2bz", Email: "break-glass@example.com", DisplayName: "Break glass", Description: "Emergency administrators", Types: []string{"locked", "security"}, Created: t("2022-04-01T09:00:00Z"), Updated: t("2025-04-01T09:00:00Z")},
			{ID: "04i7ojhp2wte0ys", Email: "gcp-org-admins@example.com", DisplayName: "GCP organization admins", Types: []string{"security"}, Created: t("2021-03-01T10:00:00Z"), Updated: t("2025-11-20T13:00:00Z")},
			{ID: "05mq7p3h1n2c9xd", Email: "security@example.com", DisplayName: "Security", Description: "Security team and on-call", Types: []string{"discussion forum", "security"}, Created: t("2021-03-01T10:05:00Z"), Updated: t("2026-02-14T08:00:00Z")},
			{ID: "06a1b2c3d4e5f6g", Email: "finance@example.com", DisplayName: "Finance", Types: []string{"discussion forum"}, Created: t("2021-03-01T10:10:00Z"), Updated: t("2025-12-19T11:05:00Z")},
			{ID: "07z9y8x7w6v5u4t", Email: "all@example.com", DisplayName: "Everyone", Description: "All active staff", Types: []string{"discussion forum", "dynamic"}, Created: t("2021-03-01T10:15:00Z"), Updated: t("2026-11-05T02:00:00Z")},
		},
		members: map[string][]workspace.Member{
			"01d520gv4vjcrht": {
				member("m001", "alice@example.com", "USER", "MEMBER", "OWNER"),
				member("m002", "bob@example.com", "USER"),
				member("m003", "carol@example.com", "USER", "MEMBER", "MANAGER"),
				member("m004", "dave@example.com", "USER"),
				member("m005", "ci-bot@seed-project.iam.gserviceaccount.com", "SERVICE_ACCOUNT"),
			},
			"02grqrue1fe8m4k": {
				member("m011", "gina.contractor@example.com", "USER"),
				member("m012", "hal.contractor@example.com", "USER"),
				member("m013", "erin@example.com", "USER", "MEMBER", "OWNER"),
			},
			"03x8tuzt1hmd2bz": {
				member("m021", "alice@example.com", "USER", "MEMBER", "OWNER"),
			},
			"04i7ojhp2wte0ys": {
				member("m031", "alice@example.com", "USER", "MEMBER", "OWNER"),
				member("m032", "security@example.com", "GROUP"),
			},
			"05mq7p3h1n2c9xd": {
				member("m041", "carol@example.com", "USER", "MEMBER", "OWNER"),
				member("m042", "dave@example.com", "USER"),
			},
			"06a1b2c3d4e5f6g": {
				member("m051", "dana@example.com", "USER", "MEMBER", "OWNER"),
			},
			"07z9y8x7w6v5u4t": everyone,
		},
	}
}

// wsAPI is a static workspace.API over the fixtures.
type wsAPI struct {
	customer string
	users    []workspace.User
	groups   []workspace.Group
	members  map[string][]workspace.Member
}

func (a wsAPI) Customer(context.Context, string) (string, error) { return a.customer, nil }

func (a wsAPI) ListUsers(context.Context, string, int) ([]workspace.User, bool, error) {
	return a.users, false, nil
}

func (a wsAPI) ListGroups(context.Context, string, int) ([]workspace.Group, bool, error) {
	return a.groups, false, nil
}

func (a wsAPI) ListMembers(_ context.Context, group string, _ int) ([]workspace.Member, bool, error) {
	return a.members[group], false, nil
}

var _ workspace.API = wsAPI{}
