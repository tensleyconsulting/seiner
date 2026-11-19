package regions

import (
	"context"
	"time"

	"github.com/tensleyconsulting/seiner/internal/gcp/cache"
)

// liveTTL is how long one project's region list is reused. Regions change
// rarely, so this is much longer than the 60s list caches elsewhere.
const liveTTL = 24 * time.Hour

// newCache memoizes region lists per project for liveTTL. Concurrent callers
// for the same project share one in-flight call. Errors are not cached.
func newCache() *cache.Flight[[]string] {
	return cache.NewFlight[[]string](liveTTL)
}

// Cached returns a Lister that calls fetch at most once per project per
// liveTTL. Concurrent callers for a project share one in-flight call, and
// errors are not cached. Adapters with their own location list (Cloud KMS,
// Artifact Registry) wrap it in this so each expand does not repeat it.
func Cached(fetch func(ctx context.Context, project string) ([]string, error)) Lister {
	return cached{fetch: fetch, cache: newCache()}
}

type cached struct {
	fetch func(ctx context.Context, project string) ([]string, error)
	cache *cache.Flight[[]string]
}

func (c cached) Regions(ctx context.Context, project string) ([]string, error) {
	return c.cache.Do(ctx, project, func(ctx context.Context) ([]string, error) {
		return c.fetch(ctx, project)
	})
}

var _ Lister = cached{}
