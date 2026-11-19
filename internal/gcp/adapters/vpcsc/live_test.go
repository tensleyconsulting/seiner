package vpcsc

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"

	acm "google.golang.org/api/accesscontextmanager/v1"
	"google.golang.org/api/googleapi"
)

func TestFromPerimeter(t *testing.T) {
	t.Parallel()
	got := fromPerimeter(&acm.ServicePerimeter{
		Name:          "accessPolicies/100/servicePerimeters/prod",
		Title:         "Production",
		PerimeterType: "PERIMETER_TYPE_REGULAR",
		Status: &acm.ServicePerimeterConfig{
			Resources:             []string{"projects/42"},
			RestrictedServices:    []string{"storage.googleapis.com"},
			AccessLevels:          []string{"accessPolicies/100/accessLevels/corp"},
			IngressPolicies:       []*acm.IngressPolicy{{}, {}},
			EgressPolicies:        []*acm.EgressPolicy{{}},
			VpcAccessibleServices: &acm.VpcAccessibleServices{EnableRestriction: true, AllowedServices: []string{"RESTRICTED-SERVICES"}},
		},
		Spec:                  &acm.ServicePerimeterConfig{Resources: []string{"projects/42", "projects/43"}},
		UseExplicitDryRunSpec: true,
	})
	if got.Name != "prod" || got.Title != "Production" || got.Status == nil || got.Spec == nil {
		t.Fatalf("fromPerimeter = %+v", got)
	}
	s := got.Status
	if !slices.Equal(s.AccessLevels, []string{"corp"}) || len(s.Ingress) != 2 || len(s.Egress) != 1 || !s.VPCRestricted ||
		!slices.Equal(s.VPCAllowed, []string{"RESTRICTED-SERVICES"}) || len(got.Spec.Resources) != 2 {
		t.Fatalf("status %+v spec %+v", s, got.Spec)
	}
	// Without an explicit dry-run spec, a leftover spec is not in force.
	implicit := fromPerimeter(&acm.ServicePerimeter{Name: "x", Spec: &acm.ServicePerimeterConfig{Resources: []string{"projects/1"}}})
	if implicit.Spec != nil || implicit.Status != nil {
		t.Fatalf("implicit = %+v", implicit)
	}
	// An explicit spec that is empty is an empty config, not "no spec".
	empty := fromPerimeter(&acm.ServicePerimeter{Name: "x", UseExplicitDryRunSpec: true})
	if empty.Spec == nil {
		t.Fatal("explicit empty spec dropped")
	}
	// Restriction turned off lists no allowed services.
	off := fromConfig(&acm.ServicePerimeterConfig{VpcAccessibleServices: &acm.VpcAccessibleServices{AllowedServices: []string{"x"}}})
	if off.VPCRestricted || off.VPCAllowed != nil {
		t.Fatalf("off = %+v", off)
	}
}

func TestFromLevel(t *testing.T) {
	t.Parallel()
	basic := fromLevel(&acm.AccessLevel{
		Name:  "accessPolicies/100/accessLevels/devices",
		Title: "Devices",
		Basic: &acm.BasicLevel{CombiningFunction: "OR", Conditions: []*acm.Condition{
			{
				IpSubnetworks:        []string{"203.0.113.0/24"},
				RequiredAccessLevels: []string{"accessPolicies/100/accessLevels/corp"},
				DevicePolicy:         &acm.DevicePolicy{RequireScreenlock: true},
				VpcNetworkSources:    []*acm.VpcNetworkSource{{VpcSubnetwork: &acm.VpcSubNetwork{Network: "//compute.googleapis.com/projects/p/global/networks/default"}}},
				Negate:               true,
			},
		}},
	})
	if basic.Name != "devices" || basic.Combining != "OR" || len(basic.Conditions) != 1 {
		t.Fatalf("basic = %+v", basic)
	}
	c := basic.Conditions[0]
	if c.Device == nil || !c.Device.Screenlock || !c.Negate || !slices.Equal(c.RequiredLevels, []string{"corp"}) || len(c.VPCNetworks) != 1 {
		t.Fatalf("condition = %+v", c)
	}
	custom := fromLevel(&acm.AccessLevel{Name: "accessPolicies/100/accessLevels/admins", Custom: &acm.CustomLevel{Expr: &acm.Expr{Expression: "true"}}})
	if custom.Expression != "true" || custom.Conditions != nil {
		t.Fatalf("custom = %+v", custom)
	}
}

func TestFromPolicy(t *testing.T) {
	t.Parallel()
	got := fromPolicy(&acm.AccessPolicy{Name: "accessPolicies/100", Title: "default", Scopes: []string{"folders/7"}})
	if got.Name != "100" || got.Title != "default" || !slices.Equal(got.Scopes, []string{"folders/7"}) {
		t.Fatalf("fromPolicy = %+v", got)
	}
}

func TestDenied(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"forbidden", &googleapi.Error{Code: http.StatusForbidden, Message: "Permission 'accesscontextmanager.policies.list' denied"}, true},
		{"api disabled by reason", &googleapi.Error{Code: http.StatusForbidden, Errors: []googleapi.ErrorItem{{Reason: "accessNotConfigured"}}}, false},
		{"api disabled by message", &googleapi.Error{Code: http.StatusForbidden, Message: "Access Context Manager API has not been used in project 1"}, false},
		{"not found", &googleapi.Error{Code: http.StatusNotFound}, false},
		{"other", errors.New("boom"), false},
	}
	for _, tc := range cases {
		got := denied(tc.err)
		if errors.Is(got, ErrDenied) != tc.want {
			t.Errorf("%s: denied = %v, want ErrDenied %v", tc.name, got, tc.want)
		}
		// The Google error stays in the chain for Classify.
		if !errors.Is(got, tc.err) {
			t.Errorf("%s: lost the original error", tc.name)
		}
	}
	wrapped := denied(fmt.Errorf("list: %w", &googleapi.Error{Code: http.StatusForbidden}))
	if !errors.Is(wrapped, ErrDenied) {
		t.Errorf("wrapped forbidden not denied: %v", wrapped)
	}
}

func TestFromRules(t *testing.T) {
	t.Parallel()
	in := fromIngress(&acm.IngressPolicy{
		Title: "CI deploys",
		IngressFrom: &acm.IngressFrom{
			IdentityType: "IDENTITY_TYPE_UNSPECIFIED",
			Identities:   []string{"serviceAccount:ci@build.iam.gserviceaccount.com"},
			Sources: []*acm.IngressSource{
				{AccessLevel: "accessPolicies/100/accessLevels/corp"},
				{AccessLevel: "*"},
				{Resource: "projects/50"},
				{PscEndpoint: &acm.PrivateServiceConnectEndpoint{ForwardingRule: "projects/p/regions/r/forwardingRules/f"}},
			},
		},
		IngressTo: &acm.IngressTo{
			Operations: []*acm.ApiOperation{{ServiceName: "storage.googleapis.com", MethodSelectors: []*acm.MethodSelector{{Method: "google.storage.objects.get"}, {Permission: "storage.objects.list"}, {}}}},
			Resources:  []string{"*"},
			Roles:      []string{"roles/viewer"},
		},
	})
	if in.Title != "CI deploys" || in.IdentityType != "" || len(in.Identities) != 1 ||
		!slices.Equal(in.Sources, []string{"corp", "*", "projects/50", "psc projects/p/regions/r/forwardingRules/f"}) {
		t.Fatalf("ingress from = %+v", in)
	}
	if len(in.Operations) != 1 || !slices.Equal(in.Operations[0].Methods, []string{"google.storage.objects.get", "permission storage.objects.list"}) ||
		!slices.Equal(in.Resources, []string{"*"}) || !slices.Equal(in.Roles, []string{"roles/viewer"}) {
		t.Fatalf("ingress to = %+v", in)
	}
	eg := fromEgress(&acm.EgressPolicy{
		EgressFrom: &acm.EgressFrom{IdentityType: "ANY_IDENTITY", SourceRestriction: "SOURCE_RESTRICTION_ENABLED", Sources: []*acm.EgressSource{{Resource: "projects/42"}}},
		EgressTo:   &acm.EgressTo{ExternalResources: []string{"s3://exports"}, Operations: []*acm.ApiOperation{{ServiceName: "*"}}},
	})
	if eg.IdentityType != "ANY_IDENTITY" || eg.SourceRestriction != "SOURCE_RESTRICTION_ENABLED" || !slices.Equal(eg.Sources, []string{"projects/42"}) ||
		!slices.Equal(eg.ExternalResources, []string{"s3://exports"}) || len(eg.Operations) != 1 || eg.Operations[0].Service != "*" {
		t.Fatalf("egress = %+v", eg)
	}
	unspecified := fromEgress(&acm.EgressPolicy{EgressFrom: &acm.EgressFrom{SourceRestriction: "SOURCE_RESTRICTION_UNSPECIFIED"}})
	if unspecified.SourceRestriction != "" {
		t.Fatalf("unspecified restriction = %q", unspecified.SourceRestriction)
	}
	if empty := fromIngress(&acm.IngressPolicy{}); empty.Sources != nil || empty.Operations != nil {
		t.Fatalf("empty = %+v", empty)
	}
}

func TestFromDevice(t *testing.T) {
	t.Parallel()
	if fromDevice(nil) != nil {
		t.Fatal("nil device policy mapped")
	}
	d := fromDevice(&acm.DevicePolicy{
		AllowedEncryptionStatuses:     []string{"ENCRYPTED"},
		AllowedDeviceManagementLevels: []string{"COMPLETE"},
		OsConstraints:                 []*acm.OsConstraint{{OsType: "DESKTOP_CHROME_OS", MinimumVersion: "120", RequireVerifiedChromeOs: true}},
		RequireCorpOwned:              true,
		RequireAdminApproval:          true,
	})
	if !slices.Equal(d.Encryption, []string{"ENCRYPTED"}) || !slices.Equal(d.Management, []string{"COMPLETE"}) ||
		len(d.OS) != 1 || d.OS[0] != (OS{Type: "DESKTOP_CHROME_OS", MinVersion: "120", Verified: true}) ||
		!d.CorpOwned || !d.AdminApproval || d.Screenlock {
		t.Fatalf("device = %+v", d)
	}
}
