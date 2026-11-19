package fanout

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegionsOrdersResultsAndReportsFailures(t *testing.T) {
	t.Parallel()
	locations := []string{"us-central1", "us-east1", "europe-west1"}
	boom := errors.New("boom")

	items, errs := Regions(context.Background(), locations, 0,
		func(_ context.Context, loc string) ([]string, error) {
			if loc == "us-east1" {
				return nil, boom
			}
			return []string{loc + "-a", loc + "-b"}, nil
		})

	want := []string{"us-central1-a", "us-central1-b", "europe-west1-a", "europe-west1-b"}
	if len(items) != len(want) {
		t.Fatalf("items = %v, want %v", items, want)
	}
	for i, v := range want {
		if items[i] != v {
			t.Fatalf("items = %v, want %v", items, want)
		}
	}

	if len(errs) != 1 {
		t.Fatalf("errs = %v, want 1 entry", errs)
	}
	if errs[0].Location != "us-east1" || !errors.Is(errs[0].Err, boom) {
		t.Fatalf("errs[0] = %+v", errs[0])
	}
}

func TestRegionsEmptyLocations(t *testing.T) {
	t.Parallel()
	items, errs := Regions(context.Background(), nil, 4,
		func(context.Context, string) ([]int, error) { return []int{1}, nil })
	if items != nil || errs != nil {
		t.Fatalf("items = %v, errs = %v, want both nil", items, errs)
	}
}

func TestRegionsRespectsLimit(t *testing.T) {
	t.Parallel()
	locations := make([]string, 20)
	for i := range locations {
		locations[i] = "loc"
	}

	var current, max int32
	const limit = 3
	block := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(block)
	}()

	_, _ = Regions(context.Background(), locations, limit,
		func(context.Context, string) ([]struct{}, error) {
			n := atomic.AddInt32(&current, 1)
			for {
				old := atomic.LoadInt32(&max)
				if n <= old || atomic.CompareAndSwapInt32(&max, old, n) {
					break
				}
			}
			<-block
			atomic.AddInt32(&current, -1)
			return nil, nil
		})

	if max > limit {
		t.Fatalf("max concurrent = %d, want <= %d", max, limit)
	}
	if max != limit {
		t.Fatalf("max concurrent = %d, want exactly %d (limit never reached)", max, limit)
	}
}

func TestErr(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	tests := []struct {
		name string
		errs []LocationError
		want string
	}{
		{"none", nil, ""},
		{"one", []LocationError{{Location: "us-east1", Err: boom}}, "locations failed: us-east1: boom"},
		{"several", []LocationError{{Location: "us-east1", Err: boom}, {Location: "europe-west1", Err: errors.New("other")}},
			"locations failed: us-east1, europe-west1: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := Err(tt.errs)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Err = %v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != tt.want {
				t.Fatalf("Err = %v, want %q", err, tt.want)
			}
			if !errors.Is(err, boom) {
				t.Fatalf("Err = %v, does not wrap the first location's error", err)
			}
		})
	}
}
