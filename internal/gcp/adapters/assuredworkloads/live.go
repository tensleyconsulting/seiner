package assuredworkloads

import (
	"context"
	"strconv"
	"time"

	aw "google.golang.org/api/assuredworkloads/v1"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
)

type liveAPI struct{ svc *aw.Service }

// NewLazy defers client construction to the first call, so a slow or failing
// constructor cannot block startup.
func NewLazy(opts ...option.ClientOption) API {
	l := lazy.New(func(ctx context.Context) (*aw.Service, error) {
		return aw.NewService(ctx, opts...)
	})
	return lazyAPI{l: l}
}

type lazyAPI struct{ l *lazy.Value[*aw.Service] }

func (a lazyAPI) ListWorkloads(ctx context.Context, org, location string) ([]Workload, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListWorkloads(ctx, org, location)
}

func (a liveAPI) ListWorkloads(ctx context.Context, org, location string) ([]Workload, error) {
	parent := "organizations/" + org + "/locations/" + location
	var out []Workload
	err := a.svc.Organizations.Locations.Workloads.List(parent).Context(ctx).Pages(ctx, func(page *aw.GoogleCloudAssuredworkloadsV1ListWorkloadsResponse) error {
		for _, w := range page.Workloads {
			out = append(out, fromWorkload(w))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func fromWorkload(w *aw.GoogleCloudAssuredworkloadsV1Workload) Workload {
	out := Workload{
		Name:               w.Name,
		DisplayName:        w.DisplayName,
		ComplianceRegime:   w.ComplianceRegime,
		Partner:            w.Partner,
		SovereignControls:  w.EnableSovereignControls,
		KAJEnrollment:      w.KajEnrollmentState,
		DisallowedServices: append([]string(nil), w.CompliantButDisallowedServices...),
	}
	if s := w.ComplianceStatus; s != nil {
		out.HasStatus = true
		out.ActiveViolations = s.ActiveViolationCount
		out.AcknowledgedViolations = s.AcknowledgedViolationCount
	}
	for _, r := range w.Resources {
		if r == nil || r.ResourceId == 0 {
			continue
		}
		out.Resources = append(out.Resources, Resource{ID: strconv.FormatInt(r.ResourceId, 10), Type: r.ResourceType})
	}
	if t, err := time.Parse(time.RFC3339, w.CreateTime); err == nil {
		out.Created = t
	}
	return out
}

var _ API = lazyAPI{}
