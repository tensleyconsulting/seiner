// Package regions reports the Google Cloud regions available to a project,
// so adapters that must call a regional API once per region (fanout.Regions)
// know which locations to fan out across.
package regions

import "context"

// Lister reports the regions enabled for a project, e.g. "us-central1".
type Lister interface {
	Regions(ctx context.Context, project string) ([]string, error)
}

// Static always returns the same fixed list, ignoring project. Use it as a
// fallback when Compute Engine's API is disabled, so a regional adapter still
// has a location list to fan out across.
type Static []string

func (s Static) Regions(ctx context.Context, _ string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]string(nil), s...), nil
}

// DefaultRegions is a conservative fallback list of long-standing Google
// Cloud regions, for use with Static when Compute Engine is unavailable.
var DefaultRegions = Static{
	"us-central1", "us-east1", "us-east4", "us-west1",
	"europe-west1", "europe-west4",
	"asia-east1", "asia-southeast1",
}

// Fake is a test double returning a canned list or error.
type Fake struct {
	Names []string
	Err   error
}

func (f Fake) Regions(ctx context.Context, _ string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.Err != nil {
		return nil, f.Err
	}
	return append([]string(nil), f.Names...), nil
}

var (
	_ Lister = Static(nil)
	_ Lister = Fake{}
)
