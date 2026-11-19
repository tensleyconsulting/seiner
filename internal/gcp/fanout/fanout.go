// Package fanout runs one call per location with bounded concurrency,
// keeping results ordered and partial failures visible per location. Adapters
// for APIs that require a call per region (Cloud Run, Cloud Functions, GKE,
// Memorystore, Dataproc, ...) use it instead of each rolling its own
// goroutine pool; Compute's aggregated lists cover all zones in one call and
// do not need it.
package fanout

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"
)

// LocationError is one location's failed call.
type LocationError struct {
	Location string
	Err      error
}

func (e LocationError) Error() string {
	return fmt.Sprintf("%s: %v", e.Location, e.Err)
}

func (e LocationError) Unwrap() error { return e.Err }

// Err is one error for the failed locations, nil when there are none. It
// names them and wraps the first one's error, so the error can still be
// classified: "locations failed: us-east1, europe-west1: <err>".
func Err(errs []LocationError) error {
	if len(errs) == 0 {
		return nil
	}
	names := make([]string, len(errs))
	for i, e := range errs {
		names[i] = e.Location
	}
	return fmt.Errorf("locations failed: %s: %w", strings.Join(names, ", "), errs[0].Err)
}

// Regions runs fn once per location with at most limit in flight and returns
// the concatenated results, ordered by location, plus one error per failed
// location. A failed location contributes no items but does not stop the
// others. limit <= 0 means unbounded.
func Regions[T any](ctx context.Context, locations []string, limit int, fn func(ctx context.Context, location string) ([]T, error)) ([]T, []LocationError) {
	items := make([][]T, len(locations))
	errs := make([]error, len(locations))

	var g errgroup.Group
	if limit > 0 {
		g.SetLimit(limit)
	}
	for i, loc := range locations {
		g.Go(func() error {
			out, err := fn(ctx, loc)
			if err != nil {
				errs[i] = err
				return nil
			}
			items[i] = out
			return nil
		})
	}
	_ = g.Wait()

	var out []T
	var locErrs []LocationError
	for i, loc := range locations {
		if errs[i] != nil {
			locErrs = append(locErrs, LocationError{Location: loc, Err: errs[i]})
			continue
		}
		out = append(out, items[i]...)
	}
	return out, locErrs
}
