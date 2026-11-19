package serviceusage

import (
	"time"

	"github.com/tensleyconsulting/seiner/internal/gcp/cache"
)

// enabledTTL is how long one project's enabled set is reused.
const enabledTTL = 5 * time.Minute

// newEnabledCache memoizes ListEnabled per project for enabledTTL.
// Concurrent callers for the same project share one in-flight call. Errors
// are not cached.
func newEnabledCache() *cache.Flight[[]APIService] {
	return cache.NewFlight[[]APIService](enabledTTL)
}
