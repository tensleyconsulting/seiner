package orgpolicy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	op "google.golang.org/api/orgpolicy/v2"
)

// TestListPoliciesUsesTheScopesCollection checks each CRM kind lists its own
// policies collection, the call that returns only direct policies.
func TestListPoliciesUsesTheScopesCollection(t *testing.T) {
	t.Parallel()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"policies":[{"name":"x/policies/compute.requireOsLogin","spec":{"rules":[{"enforce":true}]}}]}`))
	}))
	defer srv.Close()
	p := NewLazy(option.WithEndpoint(srv.URL), option.WithoutAuthentication(), option.WithHTTPClient(srv.Client()))
	for _, scope := range []string{"organizations/1", "folders/2", "projects/p"} {
		got, err := p.List(context.Background(), scope)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Name != "compute.requireOsLogin" || got[0].Metadata["enforcement"] != "enforced" {
			t.Fatalf("%s: %+v", scope, got)
		}
	}
	want := []string{"/v2/organizations/1/policies", "/v2/folders/2/policies", "/v2/projects/p/policies"}
	if !slices.Equal(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

func TestFromPolicy(t *testing.T) {
	t.Parallel()
	got := fromPolicy(&op.GoogleCloudOrgpolicyV2Policy{
		Name: "projects/123/policies/gcp.resourceLocations",
		Spec: &op.GoogleCloudOrgpolicyV2PolicySpec{
			InheritFromParent: true,
			UpdateTime:        "2025-02-11T16:45:00Z",
			Rules: []*op.GoogleCloudOrgpolicyV2PolicySpecPolicyRule{
				{Values: &op.GoogleCloudOrgpolicyV2PolicySpecPolicyRuleStringValues{AllowedValues: []string{"in:us-locations"}}},
				{DenyAll: true, Condition: &op.GoogleTypeExpr{Expression: "resource.matchTag('env', 'dev')"}},
			},
		},
		DryRunSpec: &op.GoogleCloudOrgpolicyV2PolicySpec{},
	})
	if got.Constraint != "gcp.resourceLocations" || !got.InheritFromParent || !got.DryRun || got.Updated.IsZero() ||
		len(got.Rules) != 2 || got.Rules[0].Allowed[0] != "in:us-locations" ||
		!got.Rules[1].DenyAll || got.Rules[1].Condition != "resource.matchTag('env', 'dev')" {
		t.Fatalf("fromPolicy = %+v", got)
	}
}

func TestFromPolicyDryRunSpecAndParameters(t *testing.T) {
	t.Parallel()
	got := fromPolicy(&op.GoogleCloudOrgpolicyV2Policy{
		Name: "projects/p/policies/compute.managed.restrictProtocolForwardingCreationForTypes",
		Spec: &op.GoogleCloudOrgpolicyV2PolicySpec{Rules: []*op.GoogleCloudOrgpolicyV2PolicySpecPolicyRule{
			{Enforce: true, Parameters: googleapi.RawMessage(`{ "allowedSchemes": [ "INTERNAL" ] }`)},
		}},
		DryRunSpec: &op.GoogleCloudOrgpolicyV2PolicySpec{
			InheritFromParent: true,
			UpdateTime:        "2026-10-01T00:00:00Z",
			Rules:             []*op.GoogleCloudOrgpolicyV2PolicySpecPolicyRule{{DenyAll: true}},
		},
	})
	if !got.DryRun || !got.DryRunInherit || len(got.DryRunRules) != 1 || !got.DryRunRules[0].DenyAll ||
		got.Rules[0].Parameters != `{"allowedSchemes":["INTERNAL"]}` {
		t.Fatalf("fromPolicy = %+v", got)
	}
}
