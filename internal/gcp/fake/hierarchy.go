package fake

import (
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func hnode(id, name string, kind model.NodeKind, kids ...model.HierarchyNode) model.HierarchyNode {
	n := model.HierarchyNode{ID: id, Name: name, Kind: kind, Children: kids}
	for i := range n.Children {
		n.Children[i].ParentID = id
	}
	return n
}

// project sets the project-only fields. created is RFC 3339.
func project(n model.HierarchyNode, number, created string, labels map[string]string) model.HierarchyNode {
	t, err := time.Parse(time.RFC3339, created)
	if err != nil {
		panic(err)
	}
	n.Number, n.Created, n.Labels = number, t, labels
	return n
}

// Hierarchy is the fixture CRM tree. Node ids are full CRM names; Name is the
// short name. demo-project sits five nodes deep:
// example-org → platform → prod → prod-apps → demo-project.
func Hierarchy() model.HierarchyNode {
	return hnode("organizations/example-org", "example-org", model.KindOrg,
		hnode("folders/platform", "platform", model.KindFolder,
			hnode("folders/prod", "prod", model.KindFolder,
				hnode("folders/prod-apps", "prod-apps", model.KindFolder,
					project(hnode("projects/demo-project", "demo-project", model.KindProject), "123456789012", "2023-06-30T11:58:00Z", map[string]string{"env": "prod", "cost-center": "web"}),
					hnode("projects/checkout-project", "checkout-project", model.KindProject),
				),
				hnode("folders/prod-data", "prod-data", model.KindFolder,
					hnode("projects/analytics-project", "analytics-project", model.KindProject),
				),
			),
			hnode("folders/staging", "staging", model.KindFolder,
				hnode("folders/staging-apps", "staging-apps", model.KindFolder,
					hnode("projects/staging-project", "staging-project", model.KindProject),
				),
			),
		),
		hnode("folders/security", "security", model.KindFolder,
			hnode("folders/sec-core", "sec-core", model.KindFolder,
				hnode("projects/security-project", "security-project", model.KindProject),
			),
		),
		hnode("folders/sandbox", "sandbox", model.KindFolder,
			hnode("projects/playground-project", "playground-project", model.KindProject),
		),
		workload(hnode("folders/eu-regulated", "eu-regulated", model.KindFolder,
			hnode("projects/eu-analytics-project", "eu-analytics-project", model.KindProject),
		)),
	)
}

// workload makes n an EU data boundary Assured Workload folder with one
// active violation.
func workload(n model.HierarchyNode) model.HierarchyNode {
	n.Workload = &model.AssuredWorkload{
		ID:                     "organizations/example-org/locations/europe-west3/workloads/eu-regulated",
		Name:                   "eu-regulated",
		Location:               "europe-west3",
		Regime:                 "EU_DATA_BOUNDARY_AND_SUPPORT",
		SovereignControls:      true,
		KAJEnrollment:          "COMPLETE",
		ActiveViolations:       1,
		AcknowledgedViolations: 0,
		KeysProject:            "projects/987654321098",
		DisallowedServices:     []string{"bigquerydatatransfer.googleapis.com"},
		Created:                time.Date(2024, 2, 12, 9, 30, 0, 0, time.UTC),
	}
	return n
}
