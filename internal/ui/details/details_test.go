package details

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/icons"
	"github.com/tensleyconsulting/seiner/internal/ui/keys"
)

func render(t *testing.T, r model.Resource, width int) string {
	t.Helper()
	m := New(icons.TextRenderer{}, keys.Default())
	m.SetSize(width, 80)
	m.SetResource(&r)
	return ansi.Strip(m.View())
}

func TestViewRendersFieldsInDeclaredOrder(t *testing.T) {
	t.Parallel()
	r := model.Normalize(model.Resource{
		ID:      "projects/p/zones/z/instances/web-1",
		Name:    "web-1",
		Service: model.ServiceComputeEngine,
		Status:  model.StatusWarning,
		State:   "TERMINATED",
		Created: time.Date(2025, 1, 15, 18, 4, 0, 0, time.UTC),
		Labels:  map[string]string{"team": "web", "env": "prod"},
		Fields: []model.Field{
			{Key: "zeta", Value: "1"},
			{Key: "alpha", Value: "2"},
			{Key: "mid", Value: "3"},
		},
	})
	out := render(t, r, 60)

	order := []*regexp.Regexp{
		regexp.MustCompile(`Status +Warning · TERMINATED`),
		regexp.MustCompile(`Created +2025-01-15 18:04 UTC`),
		regexp.MustCompile(`zeta +1`), regexp.MustCompile(`alpha +2`), regexp.MustCompile(`mid +3`),
		regexp.MustCompile(`Labels ─`), regexp.MustCompile(`env +prod`), regexp.MustCompile(`team +web`),
	}
	at := 0
	for _, want := range order {
		loc := want.FindStringIndex(out[at:])
		if loc == nil {
			t.Fatalf("%q missing or out of order after offset %d:\n%s", want, at, out)
		}
		at += loc[1]
	}
}

func TestViewHeaderPutsNameAndServiceBesideIcon(t *testing.T) {
	t.Parallel()
	r := model.Normalize(model.Resource{ID: "projects/p/global/networks/default", Name: "default",
		Service: model.ServiceVPC, Location: "global", Status: model.StatusHealthy})
	lines := strings.Split(render(t, r, 60), "\n")
	if !regexp.MustCompile(`^│ 🌐 +default +│$`).MatchString(lines[1]) {
		t.Errorf("first line %q: want icon then name", lines[1])
	}
	if !regexp.MustCompile(`^│ {4}VPC Network +│$`).MatchString(lines[2]) {
		t.Errorf("second line %q: want service label under the name", lines[2])
	}
}

func TestViewCollectionShowsCountNotSyntheticIdentity(t *testing.T) {
	t.Parallel()
	owner := model.Resource{ID: "projects/demo", Name: "demo", ScopeID: "projects/demo", Service: model.ServiceProject}
	out := render(t, model.CollectionResource(owner, model.ServiceVPC, 3), 60)
	if n := strings.Count(out, "VPC Network"); n != 1 {
		t.Errorf("%q appears %d times, want once:\n%s", "VPC Network", n, out)
	}
	if !regexp.MustCompile(`Resources +3`).MatchString(out) {
		t.Errorf("count missing:\n%s", out)
	}
	for _, absent := range []string{"collections/", "Location", "Status"} {
		if strings.Contains(out, absent) {
			t.Errorf("unexpected %q:\n%s", absent, out)
		}
	}
}

func TestViewProductGroupShowsCountNotSyntheticIdentity(t *testing.T) {
	t.Parallel()
	owner := model.Resource{ID: "projects/demo", Name: "demo", ScopeID: "projects/demo", Service: model.ServiceProject}
	out := render(t, model.ProductGroupResource(owner, model.ServiceComputeEngine, 4), 60)
	if !strings.Contains(out, "Compute Engine") || !regexp.MustCompile(`Resources +4`).MatchString(out) {
		t.Errorf("name or count missing:\n%s", out)
	}
	for _, absent := range []string{"products/", "Location", "Status"} {
		if strings.Contains(out, absent) {
			t.Errorf("unexpected %q:\n%s", absent, out)
		}
	}
}

func TestWriteTableLongKeyKeepsColumn(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	writeTable(&b, []row{
		{key: "mode", value: "a"},
		{key: "autoCreateSubnetworks", value: "true"},
		{key: "mtu", value: "1460"},
	}, 40)
	got := ansi.Strip(b.String())
	want := "mode  a\nautoCreateSubnetworks  true\nmtu   1460\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestViewOmitsEmptyOptionalSections(t *testing.T) {
	t.Parallel()
	r := model.Normalize(model.Resource{ID: "b", Name: "b", Service: model.ServiceCloudStorage, Status: model.StatusHealthy})
	out := render(t, r, 60)
	for _, absent := range []string{"Labels", "Created", "Browse", " · "} {
		if strings.Contains(out, absent) {
			t.Errorf("unexpected %q:\n%s", absent, out)
		}
	}
}

func TestViewShowsStatusOnlyWhenReported(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		r      model.Resource
		want   string // the Status row; "" for none
		absent string
	}{
		{"no health", model.Resource{Name: "b", Service: model.ServiceCloudStorage}, "", "Unknown"},
		{"health and state", model.Resource{Name: "vm", Service: model.ServiceComputeEngine, Status: model.StatusHealthy, State: "RUNNING"}, "Healthy · RUNNING", ""},
		{"warning only", model.Resource{Name: "fw", Service: model.ServiceFirewall, Status: model.StatusWarning}, "Warning", ""},
		{"state only", model.Resource{Name: "role", Service: model.ServiceCustomRole, State: "GA"}, "GA", "Unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.r.ID = "projects/p/x/" + tc.r.Name
			out := render(t, model.Normalize(tc.r), 60)
			if tc.want == "" {
				if strings.Contains(out, "Status") {
					t.Errorf("unexpected Status row:\n%s", out)
				}
			} else if !strings.Contains(out, "Status") || !strings.Contains(out, tc.want) {
				t.Errorf("want Status %q:\n%s", tc.want, out)
			}
			if tc.absent != "" && strings.Contains(out, tc.absent) {
				t.Errorf("unexpected %q:\n%s", tc.absent, out)
			}
		})
	}
}

func TestWrap(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		in        string
		width     int
		wantLines int
		ellipsis  bool
	}{
		{"short", "a: b", 20, 1, false},
		{"wraps on spaces", "sourceRanges: 10.0.0.0/8 192.168.0.0/16", 20, 3, false},
		{"long word", strings.Repeat("x", 50), 20, 3, false},
		{"capped", strings.Repeat("word ", 40), 20, maxValueLines, true},
		{"id breaks at slash", "projects/demo/global/networks/default", 20, 3, false},
		{"list breaks at comma", "10.0.0.0/8,192.168.0.0/16", 20, 2, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := wrap(tc.in, tc.width)
			lines := strings.Split(got, "\n")
			if len(lines) != tc.wantLines {
				t.Fatalf("lines=%d want %d:\n%s", len(lines), tc.wantLines, got)
			}
			for _, l := range lines {
				if w := ansi.StringWidth(l); w > tc.width {
					t.Errorf("line %q width %d > %d", l, w, tc.width)
				}
			}
			if strings.HasSuffix(got, "…") != tc.ellipsis {
				t.Errorf("ellipsis=%v:\n%s", !tc.ellipsis, got)
			}
		})
	}
}

func TestViewKeepsFacetsAndActionsWhenFieldsOverflow(t *testing.T) {
	t.Parallel()
	var fields []model.Field
	for i := range 30 {
		fields = append(fields, model.Field{Key: fmt.Sprintf("key%02d", i), Value: "v"})
	}
	r := model.Normalize(model.Resource{
		ID: "b", Name: "b", Service: model.ServiceCloudStorage, Status: model.StatusHealthy,
		Fields: fields,
		Facets: []model.Facet{{ID: model.FacetObjects, Label: "Objects"}},
	})
	m := New(icons.TextRenderer{}, keys.Default())
	m.SetSize(50, 34)
	m.SetResource(&r)
	out := ansi.Strip(m.View())
	if got := strings.Count(out, "\n") + 1; got != 34 {
		t.Fatalf("height %d want 34:\n%s", got, out)
	}
	for _, want := range []string{"Objects", keys.Default().Open.Help().Desc, keys.Default().Copy.Help().Desc, "key00", "│ …"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "key29") {
		t.Errorf("last field should be clipped:\n%s", out)
	}
}

func TestViewFallsBackToMetadataForSyntheticRows(t *testing.T) {
	t.Parallel()
	r := model.Resource{ID: "folders/1", Name: "prod", Service: model.ServiceFolder,
		Metadata: map[string]string{model.MetaKind: "folder", "displayName": "prod"}}
	out := render(t, r, 60)
	if !regexp.MustCompile(`displayName +prod`).MatchString(out) {
		t.Errorf("metadata field missing:\n%s", out)
	}
	if regexp.MustCompile(`kind +folder`).MatchString(out) {
		t.Errorf("internal kind key shown:\n%s", out)
	}
}

func TestViewListsEveryMember(t *testing.T) {
	t.Parallel()
	members := []string{"user:a@example.com", "user:b@example.com", "group:c@example.com", "serviceAccount:d@p.iam.gserviceaccount.com", "domain:example.com", "allUsers"}
	r := model.Normalize(model.BindingResource("projects/demo", "projects/demo", "roles/viewer", members, ""))
	out := render(t, r, 60)
	if !strings.Contains(out, "Members (6)") {
		t.Errorf("members divider missing:\n%s", out)
	}
	for _, m := range members {
		if !strings.Contains(out, m) {
			t.Errorf("member %q missing:\n%s", m, out)
		}
	}
}

func TestViewPrincipalIsSynthetic(t *testing.T) {
	t.Parallel()
	b := model.BindingResource("projects/demo", "projects/demo", "roles/owner", []string{"user:a@x"}, "")
	p := model.PrincipalResource(model.Resource{ID: "projects/demo/c", ScopeID: "projects/demo"}, "user:a@x", []model.Resource{b})
	out := render(t, p, 60)
	if !strings.Contains(out, "Principal · demo") || !strings.Contains(out, "Roles (1)") || !strings.Contains(out, "owner") {
		t.Errorf("principal details:\n%s", out)
	}
	for _, absent := range []string{"principals/", "Location", "Status"} {
		if strings.Contains(out, absent) {
			t.Errorf("unexpected %q:\n%s", absent, out)
		}
	}
}

func TestTitleInBorder(t *testing.T) {
	t.Parallel()
	m := New(icons.TextRenderer{}, keys.Default())
	m.SetSize(40, 10)
	for _, r := range []*model.Resource{nil, {ID: "b", Name: "bucket", Service: model.ServiceCloudStorage}} {
		m.SetResource(r)
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		if !strings.HasPrefix(lines[0], "╭─ "+Title+" ─") || len(lines) != 10 {
			t.Fatalf("resource %v: top %q, %d lines", r, lines[0], len(lines))
		}
	}
}

func TestViewListsDenials(t *testing.T) {
	t.Parallel()
	m := New(icons.TextRenderer{}, keys.Default())
	m.SetSize(80, 40)
	r := model.Resource{ID: "projects/p", Name: "p", Service: model.ServiceProject}
	m.SetResource(&r)
	m.SetDenials([]model.Denial{{Service: model.ServiceGKE, Missing: []string{"container.clusters.list"}}})
	v := ansi.Strip(m.View())
	for _, want := range []string{"No access", "Kubernetes Engine", "missing container.clusters.list"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
	m.SetDenials(nil)
	if v := ansi.Strip(m.View()); strings.Contains(v, "No access") {
		t.Errorf("section shown without denials:\n%s", v)
	}
}
