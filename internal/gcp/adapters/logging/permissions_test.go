package logging

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func TestLogViewPermissions(t *testing.T) {
	t.Parallel()
	api := fakeAPI{
		buckets: []Bucket{{Name: "_Default", Location: "global", State: "ACTIVE"}},
		views:   map[string][]View{"global/_Default": {{Name: "_AllLogs"}}},
		viewIAM: map[string][]Binding{
			"global/_Default/_AllLogs": {
				{Role: "roles/logging.viewAccessor", Members: []string{"group:sre@example.com"}},
			},
		},
	}
	p := NewBuckets(api)
	buckets, err := p.List(context.Background(), "projects/p")
	if err != nil || len(buckets) == 0 {
		t.Fatalf("buckets=%+v err=%v", buckets, err)
	}
	views, _, err := p.ListRelated(context.Background(), buckets[0], model.FacetViews)
	if err != nil || len(views) == 0 {
		t.Fatalf("views=%+v err=%v", views, err)
	}
	v := views[0]
	if len(v.Facets) != 1 || v.Facets[0].ID != model.FacetPermissions {
		t.Fatalf("view facets = %+v", v.Facets)
	}
	got, truncated, err := p.ListRelated(context.Background(), v, model.FacetPermissions)
	if err != nil || truncated || len(got) != 1 {
		t.Fatalf("got=%+v truncated=%v err=%v", got, truncated, err)
	}
	if got[0].Service != model.ServiceIAMBinding || got[0].ParentID != v.ID ||
		got[0].FacetID != model.FacetPermissions || got[0].ScopeID != "projects/p" ||
		got[0].Name != "logging.viewAccessor" ||
		!slices.Equal(got[0].List(model.ListMembers), []string{"group:sre@example.com"}) {
		t.Fatalf("binding[0] = %+v", got[0])
	}

	boom := errors.New("boom")
	if _, _, err := NewBuckets(fakeAPI{err: boom}).ListRelated(context.Background(), v, model.FacetPermissions); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}
