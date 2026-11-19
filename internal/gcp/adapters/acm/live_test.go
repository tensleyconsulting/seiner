package acm

import (
	"slices"
	"testing"

	acmapi "google.golang.org/api/accesscontextmanager/v1"
)

func TestFromBinding(t *testing.T) {
	t.Parallel()
	got := fromBinding(&acmapi.GcpUserAccessBinding{
		Name:               "organizations/9/gcpUserAccessBindings/b1",
		GroupKey:           "01abc",
		AccessLevels:       []string{"accessPolicies/100/accessLevels/corp"},
		DryRunAccessLevels: []string{"accessPolicies/100/accessLevels/devices"},
		SessionSettings:    &acmapi.SessionSettings{SessionLength: "3600s", SessionLengthEnabled: true, SessionReauthMethod: "LOGIN", MaxInactivity: "600s", UseOidcMaxAge: true},
		ScopedAccessSettings: []*acmapi.ScopedAccessSettings{
			{
				Scope:          &acmapi.AccessScope{ClientScope: &acmapi.ClientScope{RestrictedClientApplication: &acmapi.Application{ClientId: "123.apps.googleusercontent.com"}}},
				ActiveSettings: &acmapi.AccessSettings{AccessLevels: []string{"accessPolicies/100/accessLevels/corp"}},
				DryRunSettings: &acmapi.AccessSettings{SessionSettings: &acmapi.SessionSettings{MaxInactivity: "60s"}},
			},
			{
				Scope:          &acmapi.AccessScope{ClientScope: &acmapi.ClientScope{RestrictedProject: &acmapi.Project{Name: "projects/42"}, RestrictedClientApplication: &acmapi.Application{Name: "Cloud Console", ClientId: "x"}}},
				ActiveSettings: &acmapi.AccessSettings{SessionSettings: &acmapi.SessionSettings{SessionLength: "60s"}},
			},
		},
	})
	if got.Name != "b1" || got.GroupKey != "01abc" || got.Policy != "100" || !slices.Equal(got.Levels, []string{"corp"}) || !slices.Equal(got.DryRunLevels, []string{"devices"}) {
		t.Fatalf("fromBinding = %+v", got)
	}
	if s := got.Session; s == nil || s.Length != "3600s" || !s.LengthEnabled || s.ReauthMethod != "LOGIN" || s.MaxInactivity != "600s" || !s.UseOIDCMaxAge {
		t.Fatalf("session %+v", got.Session)
	}
	if len(got.Scoped) != 2 {
		t.Fatalf("scoped %+v", got.Scoped)
	}
	app, proj := got.Scoped[0], got.Scoped[1]
	if app.App != "123.apps.googleusercontent.com" || !slices.Equal(app.Levels, []string{"corp"}) || app.DryRunSession == nil || app.Session != nil {
		t.Fatalf("app scope %+v", app)
	}
	// The application's name wins over its client id.
	if proj.App != "Cloud Console" || proj.Project != "projects/42" || proj.Session == nil {
		t.Fatalf("project scope %+v", proj)
	}

	sa := fromBinding(&acmapi.GcpUserAccessBinding{Name: "organizations/9/gcpUserAccessBindings/s1", Principal: &acmapi.Principal{ServiceAccount: "tf@seed.iam.gserviceaccount.com"}})
	if sa.ServiceAccount != "tf@seed.iam.gserviceaccount.com" || sa.GroupKey != "" || sa.Session != nil || sa.Scoped != nil {
		t.Fatalf("sa = %+v", sa)
	}
	fed := fromBinding(&acmapi.GcpUserAccessBinding{Name: "f", Principal: &acmapi.Principal{FederatedPrincipal: "principal://x", ServiceAccountProjectNumber: ""}})
	if fed.Federated != "principal://x" {
		t.Fatalf("federated = %+v", fed)
	}
	empty := fromScoped(&acmapi.ScopedAccessSettings{})
	if empty.App != "" || empty.Project != "" || empty.Levels != nil {
		t.Fatalf("empty scope = %+v", empty)
	}
}

func TestPolicyOf(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		refs []string
		want string
	}{
		{nil, ""},
		{[]string{"corp"}, ""},
		{[]string{"accessPolicies/100/accessLevels/corp"}, "100"},
		{[]string{"x", "accessPolicies/7/accessLevels/a", "accessPolicies/8/accessLevels/b"}, "7"},
	} {
		if got := policyOf(tc.refs); got != tc.want {
			t.Errorf("policyOf(%v) = %q, want %q", tc.refs, got, tc.want)
		}
	}
	// A binding whose only level is in a scoped setting still knows its policy.
	b := fromBinding(&acmapi.GcpUserAccessBinding{Name: "b", ScopedAccessSettings: []*acmapi.ScopedAccessSettings{
		{DryRunSettings: &acmapi.AccessSettings{AccessLevels: []string{"accessPolicies/5/accessLevels/x"}}},
	}})
	if b.Policy != "5" {
		t.Fatalf("scoped-only policy %q", b.Policy)
	}
}
