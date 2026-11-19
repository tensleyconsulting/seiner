package assuredworkloads

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	mu        sync.Mutex
	workloads map[string][]Workload // org/location → workloads
	errs      map[string]error      // org/location → error
	calls     []string
}

func (f *fakeAPI) ListWorkloads(_ context.Context, org, location string) ([]Workload, error) {
	key := org + "/" + location
	f.mu.Lock()
	f.calls = append(f.calls, key)
	f.mu.Unlock()
	if err := f.errs[key]; err != nil {
		return nil, err
	}
	return f.workloads[key], nil
}

func tree() model.HierarchyNode {
	return model.HierarchyNode{ID: "organizations/1", Name: "example", Kind: model.KindOrg, Children: []model.HierarchyNode{
		{ID: "folders/10", Name: "eu", Kind: model.KindFolder, ParentID: "organizations/1", Children: []model.HierarchyNode{
			{ID: "projects/p", Name: "p", Kind: model.KindProject, ParentID: "folders/10"},
		}},
		{ID: "folders/20", Name: "us", Kind: model.KindFolder, ParentID: "organizations/1"},
		{ID: "folders/30", Name: "plain", Kind: model.KindFolder, ParentID: "organizations/1"},
	}}
}

func TestAnnotate(t *testing.T) {
	t.Parallel()
	created := time.Date(2024, 2, 12, 9, 30, 0, 0, time.UTC)
	api := &fakeAPI{
		workloads: map[string][]Workload{
			"1/europe-west3": {{
				Name:              "organizations/1/locations/europe-west3/workloads/eu",
				DisplayName:       "eu-workload",
				ComplianceRegime:  "EU_DATA_BOUNDARY_AND_SUPPORT",
				Partner:           "PARTNER_UNSPECIFIED",
				SovereignControls: true,
				KAJEnrollment:     "KAJ_ENROLLMENT_STATE_COMPLETE",
				HasStatus:         true,
				ActiveViolations:  2, AcknowledgedViolations: 1,
				Resources: []Resource{
					{ID: "10", Type: TypeConsumerFolder},
					{ID: "555", Type: TypeEncryptionKeysProject},
				},
				DisallowedServices: []string{"svc.googleapis.com"},
				Created:            created,
			}},
		},
		errs: map[string]error{"1/us": errors.New("permission denied")},
	}
	in := tree()
	out := Annotate(context.Background(), api, in, []string{"europe-west3", "us"})

	if n, _ := in.Find("folders/10"); n.Workload != nil {
		t.Fatal("input tree was modified")
	}
	eu, _ := out.Find("folders/10")
	want := &model.AssuredWorkload{
		ID:                     "organizations/1/locations/europe-west3/workloads/eu",
		Name:                   "eu-workload",
		Location:               "europe-west3",
		Regime:                 "EU_DATA_BOUNDARY_AND_SUPPORT",
		SovereignControls:      true,
		KAJEnrollment:          "COMPLETE",
		ActiveViolations:       2,
		AcknowledgedViolations: 1,
		KeysProject:            "projects/555",
		DisallowedServices:     []string{"svc.googleapis.com"},
		Created:                created,
	}
	if !reflect.DeepEqual(eu.Workload, want) {
		t.Errorf("workload %+v\nwant %+v", eu.Workload, want)
	}
	for _, id := range []string{"folders/20", "folders/30", "projects/p", "organizations/1"} {
		if n, _ := out.Find(id); n.Workload != nil {
			t.Errorf("%s has a workload", id)
		}
	}
	if len(api.calls) != 2 {
		t.Errorf("calls %v, want one per location", api.calls)
	}
}

func TestAnnotateNoStatus(t *testing.T) {
	t.Parallel()
	api := &fakeAPI{workloads: map[string][]Workload{
		"1/us": {{Name: "w", ComplianceRegime: "FEDRAMP_MODERATE", Resources: []Resource{{ID: "20", Type: TypeConsumerFolder}}}},
	}}
	out := Annotate(context.Background(), api, tree(), []string{"us"})
	n, _ := out.Find("folders/20")
	if n.Workload == nil || n.Workload.ActiveViolations != -1 || n.Workload.AcknowledgedViolations != -1 {
		t.Fatalf("workload %+v, want unknown violation counts", n.Workload)
	}
}

func TestAnnotateSkips(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		api       API
		root      model.HierarchyNode
		locations []string
	}{
		{"no api", nil, tree(), []string{"us"}},
		{"no locations", &fakeAPI{}, tree(), nil},
		{"no org", &fakeAPI{}, model.HierarchyNode{ID: "projects/p", Kind: model.KindProject}, []string{"us"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := Annotate(context.Background(), tc.api, tc.root, tc.locations)
			if !reflect.DeepEqual(out, tc.root) {
				t.Errorf("tree changed: %+v", out)
			}
			if f, ok := tc.api.(*fakeAPI); ok && len(f.calls) != 0 {
				t.Errorf("calls %v", f.calls)
			}
		})
	}
}

// Every org in a multi-org tree is listed.
func TestAnnotateEveryOrg(t *testing.T) {
	t.Parallel()
	root := model.HierarchyNode{ID: model.RootID, Kind: model.KindRoot, Children: []model.HierarchyNode{
		{ID: "organizations/1", Kind: model.KindOrg},
		{ID: "organizations/2", Kind: model.KindOrg},
	}}
	api := &fakeAPI{}
	Annotate(context.Background(), api, root, []string{"us"})
	if len(api.calls) != 2 {
		t.Errorf("calls %v", api.calls)
	}
}
