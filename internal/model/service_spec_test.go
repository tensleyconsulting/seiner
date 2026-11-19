package model

import (
	"strings"
	"testing"
)

// synthetic types are CRM or explorer nodes, not products with an API.
var synthetic = map[ServiceType]bool{
	ServiceUsage:        true,
	ServiceOrganization: true,
	ServiceFolder:       true,
	ServiceProject:      true,
}

// nodeOnly are nodes with no API of their own, and whose console link is
// the dashboard.
var nodeOnly = map[ServiceType]bool{ServiceCloudPlatform: true}

// ungated products list even when their API is off: IAM bindings through
// Resource Manager, and service accounts and custom roles without
// iam.googleapis.com enabled in the project.
var ungated = map[ServiceType]bool{
	ServiceIAM:               true,
	ServiceIAMBinding:        true,
	ServiceServiceAccount:    true,
	ServiceServiceAccountKey: true,
	ServiceCustomRole:        true,
	ServiceIAMPermission:     true,
}

func TestSpecsComplete(t *testing.T) {
	t.Parallel()
	seen := map[ServiceType]bool{}
	for _, spec := range Specs() {
		t.Run(string(spec.Type), func(t *testing.T) {
			if spec.Type == "" {
				t.Fatal("empty Type")
			}
			if seen[spec.Type] {
				t.Errorf("duplicate spec")
			}
			seen[spec.Type] = true
			if spec.Label == "" {
				t.Error("missing Label")
			}
			if spec.Abbrev == "" || spec.Abbrev == "?" {
				t.Errorf("Abbrev=%q", spec.Abbrev)
			}
			if spec.Glyph == "" {
				t.Error("missing Glyph")
			}
			if spec.Collection == "" {
				t.Error("missing Collection")
			}

			wantScope := synthetic[spec.Type] && spec.Type != ServiceUsage
			if spec.Scope != wantScope {
				t.Errorf("Scope=%v want %v", spec.Scope, wantScope)
			}
			if wantGrouped := !synthetic[spec.Type] && !spec.GroupOnly; spec.Grouped != wantGrouped {
				t.Errorf("Grouped=%v want %v", spec.Grouped, wantGrouped)
			}
			if spec.Group != "" {
				outer, ok := Spec(spec.Group)
				if !ok || synthetic[spec.Group] {
					t.Errorf("Group=%q is not a registered product", spec.Group)
				}
				if !spec.Grouped && !spec.GroupOnly {
					t.Error("Group set on an ungrouped spec")
				}
				if spec.GroupOnly && (!outer.GroupOnly || outer.Group != "") {
					t.Errorf("GroupOnly %s may nest only under a top-level GroupOnly product, not %s", spec.Type, spec.Group)
				}
				if outerProduct(spec.Group) != "" && outerProduct(outerProduct(spec.Group)) != "" {
					t.Errorf("Group=%q nests more than two levels deep", spec.Group)
				}
			}

			if synthetic[spec.Type] || nodeOnly[spec.Type] {
				if spec.API != "" || spec.Gate {
					t.Errorf("synthetic spec has API=%q Gate=%v", spec.API, spec.Gate)
				}
			} else {
				if !strings.HasSuffix(spec.API, ".googleapis.com") {
					t.Errorf("API=%q", spec.API)
				}
				if wantGate := !ungated[spec.Type]; spec.Gate != wantGate {
					t.Errorf("Gate=%v want %v", spec.Gate, wantGate)
				}
				// A client-based API is checked in the quota project, so
				// the listed project's enabled set predicts nothing.
				if spec.ClientBased && spec.Gate {
					t.Error("ClientBased spec must not be Gate")
				}
			}

			r := Resource{
				ID:       "projects/demo-project/things/thing-1",
				Name:     "thing-1",
				Service:  spec.Type,
				ScopeID:  "demo-project",
				Location: "us-central1",
			}
			dashboard := strings.Contains(r.ConsoleURL(), "/home/dashboard")
			switch {
			case spec.Console == nil && !synthetic[spec.Type]:
				t.Error("missing Console")
			case spec.Console != nil && dashboard && spec.Type != ServiceProject && !nodeOnly[spec.Type]:
				t.Errorf("ConsoleURL is the dashboard: %s", r.ConsoleURL())
			}
		})
	}
	for _, svc := range KnownServices() {
		if !seen[svc] {
			t.Errorf("%s has no spec", svc)
		}
	}
	held := map[ServiceType]bool{}
	for _, spec := range Specs() {
		held[spec.Group] = true
	}
	for _, spec := range Specs() {
		// Cloud Platform holds whatever an organization lists but Workspace.
		if spec.GroupOnly && !held[spec.Type] && !nodeOnly[spec.Type] {
			t.Errorf("%s is GroupOnly but no spec groups under it", spec.Type)
		}
	}
}

func TestSpecsReturnsCopy(t *testing.T) {
	t.Parallel()
	got := Specs()
	got[0].Label = "mutated"
	if spec, _ := Spec(got[0].Type); spec.Label == "mutated" {
		t.Fatal("Specs aliases the registry")
	}
}

func TestServiceForAPI(t *testing.T) {
	t.Parallel()
	cases := []struct {
		api  string
		want ServiceType
		ok   bool
	}{
		{"storage.googleapis.com", ServiceCloudStorage, true},
		{" storage.googleapis.com ", ServiceCloudStorage, true},
		{"compute.googleapis.com", ServiceComputeEngine, true},
		{"iam.googleapis.com", ServiceIAM, true},
		{"networksecurity.googleapis.com", ServiceNetworkSecurity, true},
		{"networkservices.googleapis.com", ServiceSecureWebProxy, true},
		{"accesscontextmanager.googleapis.com", ServiceACM, true},
		{"privateca.googleapis.com", ServicePrivateCA, true},
		{"networkconnectivity.googleapis.com", ServiceNetworkConnectivity, true},
		{"iap.googleapis.com", ServiceIAPApp, true},
		{"nope.googleapis.com", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := ServiceForAPI(tc.api)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ServiceForAPI(%q)=%q,%v want %q,%v", tc.api, got, ok, tc.want, tc.ok)
		}
	}
}

func TestUnknownServiceDefaults(t *testing.T) {
	t.Parallel()
	svc := ServiceType("not-registered")
	if svc.Label() != "not-registered" || svc.Abbrev() != "?" || svc.Glyph() != "❓" || svc.CollectionLabel() != "not-registered" {
		t.Errorf("defaults: %q %q %q %q", svc.Label(), svc.Abbrev(), svc.Glyph(), svc.CollectionLabel())
	}
	if !isGroupedService(svc) {
		t.Error("unknown services should be grouped")
	}
	r := Resource{Name: "x", Service: svc, ScopeID: "demo-project"}
	if r.IsScope() {
		t.Error("IsScope")
	}
	if got := r.ConsoleURL(); !strings.Contains(got, "/home/dashboard?project=demo-project") {
		t.Errorf("ConsoleURL=%q", got)
	}
}
