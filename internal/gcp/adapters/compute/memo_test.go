package compute

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// countingAPI counts the list calls that reach the wrapped API. A non-nil
// gate holds every call until it is closed.
type countingAPI struct {
	API
	gate chan struct{}

	mu sync.Mutex
	n  map[string]int
}

func newCounting(api API) *countingAPI { return &countingAPI{API: api, n: map[string]int{}} }

func (c *countingAPI) hit(ctx context.Context, method string) error {
	c.mu.Lock()
	c.n[method]++
	c.mu.Unlock()
	if c.gate == nil {
		return nil
	}
	select {
	case <-c.gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *countingAPI) counts() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]int{}
	for k, v := range c.n {
		out[k] = v
	}
	return out
}

func counted[T any](ctx context.Context, c *countingAPI, method, project string, fn func(context.Context, string) ([]T, error)) ([]T, error) {
	if err := c.hit(ctx, method); err != nil {
		return nil, err
	}
	return fn(ctx, project)
}

func (c *countingAPI) ListNetworks(ctx context.Context, p string) ([]Network, error) {
	return counted(ctx, c, "networks", p, c.API.ListNetworks)
}
func (c *countingAPI) ListSubnets(ctx context.Context, p string) ([]Subnet, error) {
	return counted(ctx, c, "subnets", p, c.API.ListSubnets)
}
func (c *countingAPI) ListFirewalls(ctx context.Context, p string) ([]Firewall, error) {
	return counted(ctx, c, "firewalls", p, c.API.ListFirewalls)
}
func (c *countingAPI) ListInstances(ctx context.Context, p string) ([]Instance, error) {
	return counted(ctx, c, "instances", p, c.API.ListInstances)
}
func (c *countingAPI) ListAddresses(ctx context.Context, p string) ([]Address, error) {
	return counted(ctx, c, "addresses", p, c.API.ListAddresses)
}
func (c *countingAPI) ListForwardingRules(ctx context.Context, p string) ([]ForwardingRule, error) {
	return counted(ctx, c, "forwardingRules", p, c.API.ListForwardingRules)
}
func (c *countingAPI) ListServiceAttachments(ctx context.Context, p string) ([]ServiceAttachment, error) {
	return counted(ctx, c, "serviceAttachments", p, c.API.ListServiceAttachments)
}
func (c *countingAPI) ListURLMaps(ctx context.Context, p string) ([]URLMap, error) {
	return counted(ctx, c, "urlMaps", p, c.API.ListURLMaps)
}
func (c *countingAPI) ListBackendServices(ctx context.Context, p string) ([]BackendService, error) {
	return counted(ctx, c, "backendServices", p, c.API.ListBackendServices)
}
func (c *countingAPI) ListTargetProxies(ctx context.Context, p string) ([]TargetProxy, error) {
	return counted(ctx, c, "targetProxies", p, c.API.ListTargetProxies)
}
func (c *countingAPI) ListHealthChecks(ctx context.Context, p string) ([]HealthCheck, error) {
	return counted(ctx, c, "healthChecks", p, c.API.ListHealthChecks)
}
func (c *countingAPI) ListSecurityPolicies(ctx context.Context, p string) ([]SecurityPolicy, error) {
	return counted(ctx, c, "securityPolicies", p, c.API.ListSecurityPolicies)
}
func (c *countingAPI) ListSSLPolicies(ctx context.Context, p string) ([]SSLPolicy, error) {
	return counted(ctx, c, "sslPolicies", p, c.API.ListSSLPolicies)
}

// loadProject runs the load balancer providers and the forwarding rule
// providers concurrently, as one project load does, then expands every
// facet of every load balancer.
func loadProject(t *testing.T, api API) {
	t.Helper()
	ctx := context.Background()
	const scope = "projects/demo-project"
	providers := []model.ResourceProvider{
		NewLoadBalancers(api), NewForwardingRules(api), NewPSCEndpoints(api),
		NewHealthChecks(api), NewSecurityPolicies(api), NewSSLPolicies(api),
	}
	var wg sync.WaitGroup
	var lbs []model.Resource
	for _, p := range providers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := p.List(ctx, scope)
			if err != nil && !model.IsPartial(err) {
				t.Errorf("%s: %v", p.Service(), err)
			}
			if p.Service() == model.ServiceLoadBalancer {
				lbs = got
			}
		}()
	}
	wg.Wait()
	if len(lbs) == 0 {
		t.Fatal("no load balancers")
	}
	lb := NewLoadBalancers(api)
	for _, r := range lbs {
		for _, facet := range []string{model.FacetFrontends, model.FacetBackends, model.FacetHealthChecks} {
			if _, _, err := lb.ListRelated(ctx, r, facet); err != nil {
				t.Errorf("%s %s: %v", r.Name, facet, err)
			}
		}
	}
}

func TestMemoOneProjectLoad(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		memo bool
		want map[string]int
	}{
		{
			name: "memoized",
			memo: true,
			want: map[string]int{
				"urlMaps": 1, "backendServices": 1, "targetProxies": 1, "forwardingRules": 1,
				"healthChecks": 1, "securityPolicies": 1, "sslPolicies": 1,
			},
		},
		{
			// Five load balancers, three facets each: fifteen more
			// assemblies, and four health check facets with refs.
			name: "direct",
			want: map[string]int{
				"urlMaps": 16, "backendServices": 16, "targetProxies": 16, "forwardingRules": 18,
				"healthChecks": 5, "securityPolicies": 1, "sslPolicies": 1,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := newCounting(lbSample())
			var api API = c
			if tt.memo {
				api = NewMemo(c)
			}
			loadProject(t, api)
			got := c.counts()
			for k, n := range tt.want {
				if got[k] != n {
					t.Errorf("%s calls = %d, want %d (all %v)", k, got[k], n, got)
				}
			}
		})
	}
}

func TestMemoDoesNotKeepErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	tests := []struct {
		name string
		api  fakeAPI
		call func(context.Context, API) error
	}{
		{
			name: "error",
			api:  fakeAPI{backErr: boom},
			call: func(ctx context.Context, a API) error { _, err := a.ListBackendServices(ctx, "p"); return err },
		},
		{
			name: "partial",
			api:  fakeAPI{proxies: []TargetProxy{{Name: "px", Kind: "targetHttpProxies"}}, proxyErr: model.Partial(boom)},
			call: func(ctx context.Context, a API) error { _, err := a.ListTargetProxies(ctx, "p"); return err },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := newCounting(tt.api)
			m := NewMemo(c)
			for range 2 {
				if err := tt.call(context.Background(), m); err == nil {
					t.Fatal("want an error")
				}
			}
			total := 0
			for _, n := range c.counts() {
				total += n
			}
			if total != 2 {
				t.Fatalf("calls = %v, want 2", c.counts())
			}
		})
	}
}

func TestMemoPartialKeepsItems(t *testing.T) {
	t.Parallel()
	m := NewMemo(fakeAPI{proxies: []TargetProxy{{Name: "px"}}, proxyErr: model.Partial(errors.New("ssl"))})
	got, err := m.ListTargetProxies(context.Background(), "p")
	if !model.IsPartial(err) || len(got) != 1 {
		t.Fatalf("got %v err=%v, want one proxy and a partial error", got, err)
	}
}

func TestMemoExpiryAndInvalidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		after func(m *Memo, clock *time.Time)
		want  int
	}{
		{name: "within ttl", after: func(*Memo, *time.Time) {}, want: 1},
		{name: "expired", after: func(_ *Memo, clock *time.Time) { *clock = clock.Add(memoTTL + time.Second) }, want: 2},
		{name: "invalidated", after: func(m *Memo, _ *time.Time) { m.Invalidate("projects/p") }, want: 2},
		{name: "other project invalidated", after: func(m *Memo, _ *time.Time) { m.Invalidate("projects/p2") }, want: 1},
		{name: "folder invalidated", after: func(m *Memo, _ *time.Time) { m.Invalidate("folders/1") }, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := newCounting(fakeAPI{rules: []ForwardingRule{{Name: "fr"}}})
			m := NewMemo(c)
			clock := time.Unix(1000, 0)
			m.now = func() time.Time { return clock }
			ctx := context.Background()
			if _, err := m.ListForwardingRules(ctx, "p"); err != nil {
				t.Fatal(err)
			}
			tt.after(m, &clock)
			if _, err := m.ListForwardingRules(ctx, "p"); err != nil {
				t.Fatal(err)
			}
			if got := c.counts()["forwardingRules"]; got != tt.want {
				t.Fatalf("calls = %d, want %d", got, tt.want)
			}
		})
	}
}

// waitCalls waits until method has reached the API n times.
func waitCalls(t *testing.T, c *countingAPI, method string, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for c.counts()[method] < n {
		if time.Now().After(deadline) {
			t.Fatalf("%s calls = %d, want %d", method, c.counts()[method], n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestMemoConcurrentCallersShare(t *testing.T) {
	t.Parallel()
	c := newCounting(fakeAPI{rules: []ForwardingRule{{Name: "fr"}}})
	c.gate = make(chan struct{})
	m := NewMemo(c)
	const callers = 8
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := m.ListForwardingRules(context.Background(), "p")
			if err == nil && len(got) != 1 {
				err = errors.New("wrong rules")
			}
			errs <- err
		}()
	}
	waitCalls(t, c, "forwardingRules", 1)
	close(c.gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := c.counts()["forwardingRules"]; got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestMemoCanceledCallerDoesNotPoison(t *testing.T) {
	t.Parallel()
	c := newCounting(fakeAPI{rules: []ForwardingRule{{Name: "fr"}}})
	c.gate = make(chan struct{})
	m := NewMemo(c)

	first, cancel := context.WithCancel(context.Background())
	firstErr := make(chan error, 1)
	go func() { _, err := m.ListForwardingRules(first, "p"); firstErr <- err }()
	waitCalls(t, c, "forwardingRules", 1)

	second := make(chan error, 1)
	go func() {
		got, err := m.ListForwardingRules(context.Background(), "p")
		if err == nil && len(got) != 1 {
			err = errors.New("wrong rules")
		}
		second <- err
	}()
	// Let the second caller join before the first gives up.
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-firstErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("first err = %v, want canceled", err)
	}
	close(c.gate)
	if err := <-second; err != nil {
		t.Fatalf("second err = %v", err)
	}
	if got := c.counts()["forwardingRules"]; got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestMemoLastCallerGivingUpCancelsTheCall(t *testing.T) {
	t.Parallel()
	c := newCounting(fakeAPI{rules: []ForwardingRule{{Name: "fr"}}})
	c.gate = make(chan struct{})
	m := NewMemo(c)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := m.ListForwardingRules(ctx, "p"); done <- err }()
	waitCalls(t, c, "forwardingRules", 1)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want canceled", err)
	}
	// The abandoned call is dropped, so the next caller lists again.
	close(c.gate)
	if _, err := m.ListForwardingRules(context.Background(), "p"); err != nil {
		t.Fatal(err)
	}
	if got := c.counts()["forwardingRules"]; got != 2 {
		t.Fatalf("calls = %d, want 2", got)
	}
}
