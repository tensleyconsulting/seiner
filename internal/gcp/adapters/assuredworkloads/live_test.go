package assuredworkloads

import (
	"reflect"
	"testing"
	"time"

	aw "google.golang.org/api/assuredworkloads/v1"
)

func TestFromWorkload(t *testing.T) {
	t.Parallel()
	got := fromWorkload(&aw.GoogleCloudAssuredworkloadsV1Workload{
		Name:                           "organizations/1/locations/us/workloads/w",
		DisplayName:                    "fedramp",
		ComplianceRegime:               "DATA_BOUNDARY_FOR_FEDRAMP_MODERATE",
		Partner:                        "PARTNER_UNSPECIFIED",
		EnableSovereignControls:        true,
		KajEnrollmentState:             "KAJ_ENROLLMENT_STATE_PENDING",
		ComplianceStatus:               &aw.GoogleCloudAssuredworkloadsV1WorkloadComplianceStatus{ActiveViolationCount: 3, AcknowledgedViolationCount: 1},
		CompliantButDisallowedServices: []string{"a.googleapis.com"},
		Resources: []*aw.GoogleCloudAssuredworkloadsV1WorkloadResourceInfo{
			{ResourceId: 42, ResourceType: "CONSUMER_FOLDER"},
			nil,
			{ResourceType: "KEYRING"},
		},
		CreateTime: "2024-02-12T09:30:00Z",
	})
	want := Workload{
		Name:                   "organizations/1/locations/us/workloads/w",
		DisplayName:            "fedramp",
		ComplianceRegime:       "DATA_BOUNDARY_FOR_FEDRAMP_MODERATE",
		Partner:                "PARTNER_UNSPECIFIED",
		SovereignControls:      true,
		KAJEnrollment:          "KAJ_ENROLLMENT_STATE_PENDING",
		HasStatus:              true,
		ActiveViolations:       3,
		AcknowledgedViolations: 1,
		Resources:              []Resource{{ID: "42", Type: TypeConsumerFolder}},
		DisallowedServices:     []string{"a.googleapis.com"},
		Created:                time.Date(2024, 2, 12, 9, 30, 0, 0, time.UTC),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fromWorkload = %+v\nwant %+v", got, want)
	}
	if bare := fromWorkload(&aw.GoogleCloudAssuredworkloadsV1Workload{Name: "w"}); bare.HasStatus || !bare.Created.IsZero() {
		t.Fatalf("bare = %+v", bare)
	}
}
