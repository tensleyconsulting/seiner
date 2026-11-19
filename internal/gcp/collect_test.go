package gcp

import (
	"context"
	"sync"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// listResult aggregates one load's provider results so tests can assert on
// a whole scope or facet at once.
type listResult struct {
	Resources []model.Resource
	Errors    []model.ProviderError
	Truncated bool // any provider stopped before its last item
}

func (c Catalog) List(ctx context.Context, scopeID string) listResult {
	return c.collect(func(fn func(model.ProviderResult)) { c.ListEach(ctx, scopeID, fn) })
}

func (c Catalog) ListRelated(ctx context.Context, parent model.Resource, facetID string) listResult {
	return c.collect(func(fn func(model.ProviderResult)) { c.ListRelatedEach(ctx, parent, facetID, fn) })
}

// collect runs each and aggregates results in provider order.
func (c Catalog) collect(each func(fn func(model.ProviderResult))) listResult {
	var mu sync.Mutex
	got := map[model.ServiceType][]model.ProviderResult{}
	each(func(r model.ProviderResult) {
		mu.Lock()
		defer mu.Unlock()
		got[r.Service] = append(got[r.Service], r)
	})
	var res listResult
	seen := map[model.ServiceType]bool{}
	for _, p := range c.providers {
		svc := p.Service()
		if seen[svc] {
			continue
		}
		seen[svc] = true
		for _, r := range got[svc] {
			res.Resources = append(res.Resources, r.Resources...)
			res.Truncated = res.Truncated || r.Truncated
			if r.Err != nil {
				res.Errors = append(res.Errors, providerError(svc, r.Err))
			}
		}
	}
	return res
}
