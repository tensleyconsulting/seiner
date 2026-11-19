package lazy

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestValueBuildsOnce(t *testing.T) {
	t.Parallel()
	var calls int32
	v := New(func(context.Context) (int, error) {
		atomic.AddInt32(&calls, 1)
		return 7, nil
	})
	for range 3 {
		got, err := v.Get(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got != 7 {
			t.Fatalf("got %d", got)
		}
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestValueCachesError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("boom")
	var calls int32
	v := New(func(context.Context) (int, error) {
		atomic.AddInt32(&calls, 1)
		return 0, wantErr
	})
	for range 3 {
		_, err := v.Get(context.Background())
		if !errors.Is(err, wantErr) {
			t.Fatalf("err = %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestValueConcurrentGetBuildsOnce(t *testing.T) {
	t.Parallel()
	var calls int32
	v := New(func(context.Context) (int, error) {
		atomic.AddInt32(&calls, 1)
		return 1, nil
	})
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := v.Get(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

type fakeClient struct {
	closes atomic.Int32
	err    error
}

func (c *fakeClient) Close() error {
	c.closes.Add(1)
	return c.err
}

func TestGroupClose(t *testing.T) {
	t.Parallel()
	closeErr := errors.New("close failed")
	tests := []struct {
		name string
		// get says, per client, whether Get runs (twice) before Close.
		get      []bool
		failMk   bool
		closeErr error
		// wantCloses is each client's Close count after two Group.Close
		// calls.
		wantCloses []int32
		wantBuilds int32
		wantErr    error
	}{
		{name: "none built", get: []bool{false, false}, wantCloses: []int32{0, 0}},
		{name: "built ones only", get: []bool{true, false, true}, wantCloses: []int32{1, 0, 1}, wantBuilds: 2},
		{name: "failed build", get: []bool{true}, failMk: true, wantCloses: []int32{0}, wantBuilds: 1},
		{name: "close errors joined", get: []bool{true, true}, closeErr: closeErr, wantCloses: []int32{1, 1}, wantBuilds: 2, wantErr: closeErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var g Group
			var builds atomic.Int32
			clients := make([]*fakeClient, len(tt.get))
			for i, get := range tt.get {
				clients[i] = &fakeClient{err: tt.closeErr}
				v := NewIn(&g, func(context.Context) (*fakeClient, error) {
					builds.Add(1)
					if tt.failMk {
						return nil, errors.New("boom")
					}
					return clients[i], nil
				})
				if get {
					for range 2 {
						_, _ = v.Get(context.Background())
					}
				}
			}
			err := g.Close()
			if tt.wantErr == nil && err != nil || !errors.Is(err, tt.wantErr) {
				t.Fatalf("Close = %v, want %v", err, tt.wantErr)
			}
			if err := g.Close(); err != nil {
				t.Fatalf("second Close = %v", err)
			}
			for i, c := range clients {
				if got := c.closes.Load(); got != tt.wantCloses[i] {
					t.Errorf("client %d closed %d times, want %d", i, got, tt.wantCloses[i])
				}
			}
			if got := builds.Load(); got != tt.wantBuilds {
				t.Errorf("builds = %d, want %d", got, tt.wantBuilds)
			}
		})
	}
}

// A client that finishes building after Close is closed at once, and Get
// reports it unusable.
func TestGroupBuildAfterClose(t *testing.T) {
	t.Parallel()
	var g Group
	c := &fakeClient{}
	v := NewIn(&g, func(context.Context) (*fakeClient, error) { return c, nil })
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get(context.Background()); !errors.Is(err, errClosed) {
		t.Fatalf("Get = %v, want errClosed", err)
	}
	if n := c.closes.Load(); n != 1 {
		t.Fatalf("closed %d times, want 1", n)
	}
}

func TestNilGroupClosesNothing(t *testing.T) {
	t.Parallel()
	var g *Group
	c := &fakeClient{}
	v := NewIn(g, func(context.Context) (*fakeClient, error) { return c, nil })
	if _, err := v.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil || c.closes.Load() != 0 {
		t.Fatalf("Close = %v, closes = %d", err, c.closes.Load())
	}
}
