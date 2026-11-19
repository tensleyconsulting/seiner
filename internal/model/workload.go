package model

import (
	"fmt"
	"slices"
	"strconv"
	"time"
)

// ListDisallowedServices is the AssuredWorkload list of services that are
// compliant but blocked by the workload's service-usage restriction.
const ListDisallowedServices = "disallowed-services"

// AssuredWorkload is the Assured Workloads record behind a CRM folder. It is
// Google-free; internal/gcp/assuredworkloads fills it in.
type AssuredWorkload struct {
	ID       string // organizations/1/locations/us/workloads/w
	Name     string // display name
	Location string
	// Regime is the complianceRegime enum word, the workload's control
	// package or data boundary (EU_DATA_BOUNDARY_AND_SUPPORT).
	Regime            string
	Partner           string // "" when unspecified
	SovereignControls bool
	KAJEnrollment     string // Key Access Justifications state; "" when unspecified
	// ActiveViolations and AcknowledgedViolations are -1 when the API did
	// not report a compliance status.
	ActiveViolations       int64
	AcknowledgedViolations int64
	KeysProject            string // projects/{number} of the CMEK keys project; "" if none
	DisallowedServices     []string
	Created                time.Time
}

// annotate adds w's fields, badge, and status to a folder resource.
func (w AssuredWorkload) annotate(r *Resource) {
	put := func(k, v string) {
		if v != "" {
			r.Metadata[k] = v
		}
	}
	put("workload", w.Name)
	put("workloadId", w.ID)
	put("complianceRegime", w.Regime)
	put("workloadLocation", w.Location)
	put("partner", w.Partner)
	put("sovereignControls", strconv.FormatBool(w.SovereignControls))
	put("kajEnrollment", w.KAJEnrollment)
	put("keysProject", w.KeysProject)
	if w.ActiveViolations >= 0 {
		put("violations", fmt.Sprintf("%d active, %d acknowledged", w.ActiveViolations, w.AcknowledgedViolations))
	}
	if !w.Created.IsZero() {
		put("workloadCreated", w.Created.UTC().Format(time.RFC3339))
	}
	r.Badge = "AW"
	switch {
	case w.ActiveViolations > 0:
		r.Status = StatusWarning
	case w.ActiveViolations == 0:
		r.Status = StatusHealthy
	}
	if len(w.DisallowedServices) > 0 {
		r.Lists = append(r.Lists, List{
			ID:    ListDisallowedServices,
			Label: "Compliant but disallowed services",
			Items: slices.Clone(w.DisallowedServices),
		})
	}
}

// workloadFields is the details-pane order of the keys annotate sets.
var workloadFields = []string{
	"workload", "complianceRegime", "workloadLocation", "partner",
	"sovereignControls", "kajEnrollment", "violations", "keysProject",
	"workloadId", "workloadCreated",
}
