package regions

import (
	"context"

	gce "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/cache"
	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
)

// live lists regions through the Compute Engine API, built lazily on first
// use, and caches the result per project for liveTTL.
type live struct {
	l     *lazy.Value[*gce.Service]
	cache *cache.Flight[[]string]
}

// NewLive returns a Lister backed by the Compute Engine Regions API. The
// client is constructed on first use rather than at startup.
func NewLive(opts ...option.ClientOption) Lister {
	return &live{
		l:     lazy.New(func(ctx context.Context) (*gce.Service, error) { return gce.NewService(ctx, opts...) }),
		cache: newCache(),
	}
}

func (r *live) Regions(ctx context.Context, project string) ([]string, error) {
	return r.cache.Do(ctx, project, func(ctx context.Context) ([]string, error) {
		svc, err := r.l.Get(ctx)
		if err != nil {
			return nil, err
		}
		var names []string
		err = svc.Regions.List(project).Context(ctx).Pages(ctx, func(page *gce.RegionList) error {
			for _, reg := range page.Items {
				if reg.Deprecated != nil {
					continue
				}
				names = append(names, reg.Name)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return names, nil
	})
}

var _ Lister = (*live)(nil)
