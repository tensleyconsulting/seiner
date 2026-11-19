package regions

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
)

type countingFetch struct {
	mu    sync.Mutex
	calls map[string]int
	names []string
	err   error
}

func (c *countingFetch) fetch(project string) func(context.Context) ([]string, error) {
	return func(context.Context) ([]string, error) {
		c.mu.Lock()
		if c.calls == nil {
			c.calls = map[string]int{}
		}
		c.calls[project]++
		c.mu.Unlock()
		if c.err != nil {
			return nil, c.err
		}
		return c.names, nil
	}
}

func (c *countingFetch) count(project string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[project]
}

func TestCachedListerCallsOncePerProject(t *testing.T) {
	t.Parallel()
	f := &countingFetch{names: []string{"global", "us-central1"}}
	l := Cached(func(ctx context.Context, project string) ([]string, error) {
		return f.fetch(project)(ctx)
	})
	for range 3 {
		got, err := l.Regions(context.Background(), "p")
		if err != nil || !slices.Equal(got, f.names) {
			t.Fatalf("Regions = %v, %v", got, err)
		}
	}
	if _, err := l.Regions(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	if f.count("p") != 1 || f.count("q") != 1 {
		t.Fatalf("calls p=%d q=%d, want 1 each", f.count("p"), f.count("q"))
	}

	failing := &countingFetch{err: errors.New("boom")}
	fl := Cached(func(ctx context.Context, project string) ([]string, error) {
		return failing.fetch(project)(ctx)
	})
	for range 2 {
		if _, err := fl.Regions(context.Background(), "p"); err == nil {
			t.Fatal("want error")
		}
	}
	if failing.count("p") != 2 {
		t.Fatalf("errors must not be cached: calls=%d", failing.count("p"))
	}
}
