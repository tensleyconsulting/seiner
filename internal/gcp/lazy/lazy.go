// Package lazy builds a client once, on first use, so adapters can defer the
// cost (and the risk of failure) of constructing a Google client past
// startup. Both the live adapters under internal/gcp and their test code
// import this package; it stays Google-free so it carries no transitive
// client dependency of its own.
package lazy

import (
	"context"
	"errors"
	"io"
	"sync"
)

// Value builds a V once, on the first Get, and caches the result or error for
// every later call.
type Value[V any] struct {
	once sync.Once
	v    V
	err  error
	mk   func(ctx context.Context) (V, error)
}

// New returns a Value that builds its client with mk on first Get.
func New[V any](mk func(ctx context.Context) (V, error)) *Value[V] {
	return &Value[V]{mk: mk}
}

// Get returns the built value, constructing it on the first call. Building
// uses context.WithoutCancel so the client outlives the context of whichever
// caller happens to trigger construction; later calls, successful or not,
// return the cached result without rebuilding.
func (l *Value[V]) Get(ctx context.Context) (V, error) {
	l.once.Do(func() {
		l.v, l.err = l.mk(context.WithoutCancel(ctx))
	})
	return l.v, l.err
}

// errClosed is returned by a Value built after its Group closed.
var errClosed = errors.New("lazy: clients closed")

// Group closes the clients built through it. The zero Group is ready to
// use, and a nil *Group closes nothing.
type Group struct {
	mu      sync.Mutex
	closers []io.Closer
	closed  bool
}

// NewIn is New for a client that must be closed: once built, g.Close
// closes it. Nothing is built just to be closed. A client that finishes
// building after g closed is closed at once, and its Get fails.
func NewIn[V io.Closer](g *Group, mk func(ctx context.Context) (V, error)) *Value[V] {
	if g == nil {
		return New(mk)
	}
	return New(func(ctx context.Context) (V, error) {
		v, err := mk(ctx)
		if err != nil {
			return v, err
		}
		if err := g.add(v); err != nil {
			_ = v.Close()
			var zero V
			return zero, err
		}
		return v, nil
	})
}

func (g *Group) add(c io.Closer) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return errClosed
	}
	g.closers = append(g.closers, c)
	return nil
}

// Close closes every client built so far, each once, and joins their
// errors. Later calls close nothing.
func (g *Group) Close() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	closers := g.closers
	g.closers, g.closed = nil, true
	g.mu.Unlock()
	var errs []error
	for _, c := range closers {
		errs = append(errs, c.Close())
	}
	return errors.Join(errs...)
}
