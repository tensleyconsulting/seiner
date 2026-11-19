package model

import (
	"reflect"
	"testing"
	"time"
)

func TestNodeResourceWorkload(t *testing.T) {
	t.Parallel()
	w := &AssuredWorkload{
		ID:                     "organizations/1/locations/us/workloads/w",
		Name:                   "fedramp",
		Location:               "us",
		Regime:                 "DATA_BOUNDARY_FOR_FEDRAMP_MODERATE",
		KAJEnrollment:          "COMPLETE",
		ActiveViolations:       2,
		AcknowledgedViolations: 1,
		KeysProject:            "projects/555",
		DisallowedServices:     []string{"a.googleapis.com"},
		Created:                time.Date(2024, 2, 12, 9, 30, 0, 0, time.UTC),
	}
	cases := []struct {
		name       string
		n          HierarchyNode
		wantFields []Field
		wantBadge  string
		wantStatus ResourceStatus
		wantLists  []List
	}{
		{"workload folder", HierarchyNode{ID: "folders/10", Name: "fedramp", Kind: KindFolder, ParentID: "organizations/1", Workload: w},
			[]Field{
				{"parent", "organizations/1"},
				{"workload", "fedramp"},
				{"complianceRegime", "DATA_BOUNDARY_FOR_FEDRAMP_MODERATE"},
				{"workloadLocation", "us"},
				{"sovereignControls", "false"},
				{"kajEnrollment", "COMPLETE"},
				{"violations", "2 active, 1 acknowledged"},
				{"keysProject", "projects/555"},
				{"workloadId", "organizations/1/locations/us/workloads/w"},
				{"workloadCreated", "2024-02-12T09:30:00Z"},
			},
			"AW", StatusWarning,
			[]List{{ID: ListDisallowedServices, Label: "Compliant but disallowed services", Items: []string{"a.googleapis.com"}}}},
		{"no violations reported", HierarchyNode{ID: "folders/10", Name: "eu", Kind: KindFolder, Workload: &AssuredWorkload{Regime: "EU_DATA_BOUNDARY_AND_SUPPORT", ActiveViolations: -1, AcknowledgedViolations: -1}},
			[]Field{{"complianceRegime", "EU_DATA_BOUNDARY_AND_SUPPORT"}, {"sovereignControls", "false"}},
			"AW", StatusUnknown, nil},
		{"no active violations", HierarchyNode{ID: "folders/10", Name: "eu", Kind: KindFolder, Workload: &AssuredWorkload{Regime: "EU_DATA_BOUNDARY_AND_SUPPORT", AcknowledgedViolations: 3}},
			[]Field{{"complianceRegime", "EU_DATA_BOUNDARY_AND_SUPPORT"}, {"sovereignControls", "false"}, {"violations", "0 active, 3 acknowledged"}},
			"AW", StatusHealthy, nil},
		{"plain folder", HierarchyNode{ID: "folders/10", Name: "plain", Kind: KindFolder},
			nil, "", StatusUnknown, nil},
		{"workload ignored off a folder", HierarchyNode{ID: "projects/p", Name: "p", Kind: KindProject, Workload: w},
			nil, "", StatusUnknown, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := tc.n.Resource()
			if !reflect.DeepEqual(r.Fields, tc.wantFields) {
				t.Errorf("fields %v\nwant %v", r.Fields, tc.wantFields)
			}
			if r.Badge != tc.wantBadge || r.Status != tc.wantStatus {
				t.Errorf("badge %q status %q, want %q %q", r.Badge, r.Status, tc.wantBadge, tc.wantStatus)
			}
			if !reflect.DeepEqual(r.Lists, tc.wantLists) {
				t.Errorf("lists %v, want %v", r.Lists, tc.wantLists)
			}
		})
	}
}

// The resource's list must not alias the workload's slice.
func TestNodeResourceWorkloadListIsCopy(t *testing.T) {
	t.Parallel()
	w := &AssuredWorkload{ActiveViolations: -1, DisallowedServices: []string{"a"}}
	r := HierarchyNode{ID: "folders/1", Kind: KindFolder, Workload: w}.Resource()
	r.Lists[0].Items[0] = "changed"
	if w.DisallowedServices[0] != "a" {
		t.Fatal("list aliases the workload")
	}
}
