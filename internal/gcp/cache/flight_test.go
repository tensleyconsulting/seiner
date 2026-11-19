package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// counter is a fetch that counts its calls and, when block is set, waits for
// it to close before returning.
type counter struct {
	calls atomic.Int32
	block chan struct{}
	val   string
	err   error
}

func (c *counter) fetch(ctx context.Context) (string, error) {
	c.calls.Add(1)
	if c.block != nil {
		<-c.block
	}
	// A fetch whose context was canceled fails, as a real API call would.
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return c.val, c.err
}

// waitCalls spins until fetch has been called n times.
func (c *counter) waitCalls(t *testing.T, n int32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for c.calls.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("calls=%d, want %d", c.calls.Load(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestFlightExpires(t *testing.T) {
	t.Parallel()
	f := NewFlight[string](time.Minute)
	now := time.Unix(1000, 0)
	f.now = func() time.Time { return now }
	c := &counter{val: "v"}

	steps := []struct {
		advance time.Duration
		want    int32
	}{
		{0, 1},
		{time.Minute - time.Second, 1},
		{time.Second, 2},
		{time.Second, 2},
	}
	for i, s := range steps {
		now = now.Add(s.advance)
		got, err := f.Do(context.Background(), "k", c.fetch)
		if err != nil || got != "v" {
			t.Fatalf("step %d: Do = %q, %v", i, got, err)
		}
		if n := c.calls.Load(); n != s.want {
			t.Fatalf("step %d: calls=%d want %d", i, n, s.want)
		}
	}
}

func TestFlightKeysAreIndependent(t *testing.T) {
	t.Parallel()
	f := NewFlight[string](time.Minute)
	c := &counter{val: "v"}
	for _, key := range []string{"a", "b", "a", "b"} {
		if _, err := f.Do(context.Background(), key, c.fetch); err != nil {
			t.Fatal(err)
		}
	}
	if n := c.calls.Load(); n != 2 {
		t.Fatalf("calls=%d want 2", n)
	}
}

func TestFlightDoesNotCacheErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	f := NewFlight[string](time.Minute)
	c := &counter{err: boom}
	for range 2 {
		if _, err := f.Do(context.Background(), "k", c.fetch); !errors.Is(err, boom) {
			t.Fatalf("err=%v want %v", err, boom)
		}
	}
	if n := c.calls.Load(); n != 2 {
		t.Fatalf("calls=%d want 2 (errors not cached)", n)
	}
}

func TestFlightSharesInFlightFetch(t *testing.T) {
	t.Parallel()
	f := NewFlight[string](time.Minute)
	c := &counter{val: "v", block: make(chan struct{})}

	const callers = 4
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := f.Do(context.Background(), "k", c.fetch)
			if err == nil && got != "v" {
				err = errors.New("wrong value " + got)
			}
			errs <- err
		}()
	}
	c.waitCalls(t, 1)
	// Give the other callers time to join the fetch, then release it.
	time.Sleep(20 * time.Millisecond)
	close(c.block)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := c.calls.Load(); n != 1 {
		t.Fatalf("calls=%d want 1", n)
	}
}

// One caller canceling, whether it started the fetch or joined it, returns
// Canceled to that caller at once and leaves the fetch and the other
// callers alone.
func TestFlightCanceledCallerDoesNotCancelOthers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		cancelFirst bool // cancel the caller that started the fetch, else the one that joined it
	}{
		{name: "first caller cancels", cancelFirst: true},
		{name: "second caller cancels", cancelFirst: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := NewFlight[string](time.Minute)
			c := &counter{val: "v", block: make(chan struct{})}

			type result struct {
				val string
				err error
			}
			start := func(ctx context.Context) <-chan result {
				ch := make(chan result, 1)
				go func() {
					v, err := f.Do(ctx, "k", c.fetch)
					ch <- result{v, err}
				}()
				return ch
			}

			ctx1, cancel1 := context.WithCancel(context.Background())
			defer cancel1()
			first := start(ctx1)
			c.waitCalls(t, 1)
			ctx2, cancel2 := context.WithCancel(context.Background())
			defer cancel2()
			second := start(ctx2)
			time.Sleep(20 * time.Millisecond)

			canceled, other := first, second
			if tt.cancelFirst {
				cancel1()
			} else {
				cancel2()
				canceled, other = second, first
			}
			select {
			case r := <-canceled:
				if !errors.Is(r.err, context.Canceled) {
					t.Fatalf("canceled caller err=%v want Canceled", r.err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("canceled caller did not return")
			}

			close(c.block)
			if r := <-other; r.err != nil || r.val != "v" {
				t.Fatalf("other caller = %q, %v; want v, nil", r.val, r.err)
			}
			// The shared result was cached despite the cancel.
			if v, err := f.Do(context.Background(), "k", c.fetch); err != nil || v != "v" {
				t.Fatalf("Do after = %q, %v", v, err)
			}
			if n := c.calls.Load(); n != 1 {
				t.Fatalf("calls=%d want 1", n)
			}
		})
	}
}

func TestFlightFetchHasDeadline(t *testing.T) {
	t.Parallel()
	f := NewFlight[bool](time.Minute)
	got, err := f.Do(context.Background(), "k", func(ctx context.Context) (bool, error) {
		_, ok := ctx.Deadline()
		return ok, nil
	})
	if err != nil || !got {
		t.Fatalf("fetch context deadline set=%v err=%v; want a deadline", got, err)
	}
}

func TestFlightForget(t *testing.T) {
	t.Parallel()
	f := NewFlight[string](time.Minute)
	c := &counter{val: "v"}
	steps := []struct {
		forget bool
		want   int32
	}{
		{false, 1},
		{false, 1},
		{true, 2},
		{false, 2},
	}
	for i, s := range steps {
		if s.forget {
			f.Forget("k")
		}
		if _, err := f.Do(context.Background(), "k", c.fetch); err != nil {
			t.Fatal(err)
		}
		if n := c.calls.Load(); n != s.want {
			t.Fatalf("step %d: calls=%d want %d", i, n, s.want)
		}
	}
}
