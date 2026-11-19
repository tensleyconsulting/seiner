package cache

import (
	"context"
	"sync"
	"time"
)

// fetchTimeout caps one shared fetch. It matches the catalog's per-provider
// timeout, since the fetch no longer inherits any caller's deadline.
const fetchTimeout = 20 * time.Second

// Flight memoizes one fetch per key for a TTL. Concurrent callers for a key
// share one in-flight fetch, and errors are not cached. The fetch runs on a
// context detached from its callers' cancellation, so one caller giving up
// does not fail the others; each caller still returns its own ctx.Err() as
// soon as its context ends. The zero value is not usable; use NewFlight.
type Flight[V any] struct {
	ttl     time.Duration
	timeout time.Duration
	now     func() time.Time

	mu      sync.Mutex
	entries map[string]*flight[V]
}

type flight[V any] struct {
	ready chan struct{} // closed when val/err/at are set
	val   V
	err   error
	at    time.Time
}

// NewFlight returns a Flight whose successful results live for ttl.
func NewFlight[V any](ttl time.Duration) *Flight[V] {
	return &Flight[V]{ttl: ttl, timeout: fetchTimeout, now: time.Now, entries: map[string]*flight[V]{}}
}

// Do returns key's cached value, or joins or starts a fetch for it. fetch
// receives a context that keeps ctx's values but not its cancellation.
func (f *Flight[V]) Do(ctx context.Context, key string, fetch func(context.Context) (V, error)) (V, error) {
	f.mu.Lock()
	e := f.entries[key]
	if e != nil {
		select {
		case <-e.ready:
			if f.now().Sub(e.at) < f.ttl {
				f.mu.Unlock()
				return e.val, e.err
			}
			e = nil
		default:
		}
	}
	if e == nil {
		e = &flight[V]{ready: make(chan struct{})}
		f.entries[key] = e
		go f.run(context.WithoutCancel(ctx), key, e, fetch)
	}
	f.mu.Unlock()

	select {
	case <-e.ready:
		return e.val, e.err
	case <-ctx.Done():
		var zero V
		return zero, ctx.Err()
	}
}

func (f *Flight[V]) run(ctx context.Context, key string, e *flight[V], fetch func(context.Context) (V, error)) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	val, err := fetch(ctx)

	f.mu.Lock()
	defer f.mu.Unlock()
	e.val, e.err, e.at = val, err, f.now()
	close(e.ready)
	if err != nil && f.entries[key] == e {
		delete(f.entries, key)
	}
}

// Forget drops key, so the next Do fetches again. Callers already waiting
// on an in-flight fetch still receive its result, but it is not kept.
func (f *Flight[V]) Forget(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.entries, key)
}
