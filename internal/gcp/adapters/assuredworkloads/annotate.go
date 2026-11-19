package assuredworkloads

import (
	"context"
	"log/slog"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// maxConcurrentLists bounds how many ListWorkloads calls run at once.
const maxConcurrentLists = 8

// Annotate lists the workloads of every organization in root at each of
// locations and attaches each one to the folder it owns. A failed list is
// logged and leaves those folders plain: Assured Workloads never fails the
// tree. root is not modified.
func Annotate(ctx context.Context, api API, root model.HierarchyNode, locations []string) model.HierarchyNode {
	if api == nil || len(locations) == 0 {
		return root
	}
	type call struct{ org, location string }
	var calls []call
	for _, org := range orgs(root) {
		for _, loc := range locations {
			calls = append(calls, call{org, loc})
		}
	}
	found := make([][]Workload, len(calls))
	var g errgroup.Group
	g.SetLimit(maxConcurrentLists)
	for i, c := range calls {
		g.Go(func() error {
			ws, err := api.ListWorkloads(ctx, c.org, c.location)
			if err != nil {
				slog.Warn("assured workloads list failed", "org", c.org, "location", c.location, "err", err)
				return nil
			}
			found[i] = ws
			return nil
		})
	}
	_ = g.Wait()

	byFolder := map[string]*model.AssuredWorkload{}
	for i, ws := range found {
		for _, w := range ws {
			aw := workload(w, calls[i].location)
			for _, r := range w.Resources {
				if r.Type == TypeConsumerFolder && r.ID != "" {
					byFolder["folders/"+r.ID] = &aw
				}
			}
		}
	}
	if len(byFolder) == 0 {
		return root
	}
	return attach(root, byFolder)
}

// orgs returns the bare ids of the organizations in root.
func orgs(root model.HierarchyNode) []string {
	var out []string
	var walk func(model.HierarchyNode)
	walk = func(n model.HierarchyNode) {
		if n.Kind == model.KindOrg {
			if id, ok := strings.CutPrefix(n.ID, "organizations/"); ok && id != "" {
				out = append(out, id)
			}
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(root)
	return out
}

// attach returns a copy of n with each folder in byFolder given its workload.
func attach(n model.HierarchyNode, byFolder map[string]*model.AssuredWorkload) model.HierarchyNode {
	if n.Kind == model.KindFolder {
		if w, ok := byFolder[n.ID]; ok {
			n.Workload = w
		}
	}
	if len(n.Children) > 0 {
		kids := make([]model.HierarchyNode, len(n.Children))
		for i, ch := range n.Children {
			kids[i] = attach(ch, byFolder)
		}
		n.Children = kids
	}
	return n
}

// workload maps w onto the model, dropping the API's unspecified enum words.
func workload(w Workload, location string) model.AssuredWorkload {
	out := model.AssuredWorkload{
		ID:                     w.Name,
		Name:                   w.DisplayName,
		Location:               location,
		Regime:                 w.ComplianceRegime,
		Partner:                specified(w.Partner, "PARTNER_UNSPECIFIED"),
		SovereignControls:      w.SovereignControls,
		KAJEnrollment:          strings.TrimPrefix(specified(w.KAJEnrollment, "KAJ_ENROLLMENT_STATE_UNSPECIFIED"), "KAJ_ENROLLMENT_STATE_"),
		ActiveViolations:       -1,
		AcknowledgedViolations: -1,
		DisallowedServices:     w.DisallowedServices,
		Created:                w.Created,
	}
	if w.HasStatus {
		out.ActiveViolations, out.AcknowledgedViolations = w.ActiveViolations, w.AcknowledgedViolations
	}
	for _, r := range w.Resources {
		if r.Type == TypeEncryptionKeysProject && r.ID != "" {
			out.KeysProject = "projects/" + r.ID
			break
		}
	}
	return out
}

func specified(v, unspecified string) string {
	if v == unspecified {
		return ""
	}
	return v
}
