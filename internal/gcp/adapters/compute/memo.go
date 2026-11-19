package compute

import (
	"context"
	"sync"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// memoTTL is how long a successful project list is reused. One project load
// runs every compute provider at once, and several read the same lists
// (forwarding rules, backend services, target proxies), then load balancer
// facet expansions assemble everything again. 30s covers that burst. It is
// kept well under the catalog's 60s list cache so the memo does not add much
// staleness on top of it; a manual refresh drops it anyway (Invalidate).
const memoTTL = 30 * time.Second

// Memo is an API that shares project list calls: concurrent callers for the
// same method and project wait on one call, and a successful result is
// reused for memoTTL. It is a model.Invalidator so a manual refresh can drop
// a project's results. Callers must not modify the slices it returns.
type Memo struct {
	API
	ttl time.Duration
	now func() time.Time

	mu    sync.Mutex
	calls map[memoKey]*memoCall
}

type memoKey struct{ method, project string }

// memoCall is one shared list call. val and err are written before done is
// closed and read only after; the other fields are guarded by Memo.mu.
type memoCall struct {
	done    chan struct{}
	val     any
	err     error
	cancel  context.CancelFunc
	waiters int
	exp     time.Time // zero while the call runs
}

// NewMemo wraps api. ListManagedInstances and GetRouter pass through.
func NewMemo(api API) *Memo {
	return &Memo{API: api, ttl: memoTTL, now: time.Now, calls: map[memoKey]*memoCall{}}
}

// Invalidate drops scopeID's project results, cached or in flight, so the
// next call lists again. A call still running finishes for its waiters but
// is not kept. Other scopes have nothing to drop.
func (m *Memo) Invalidate(scopeID string) {
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.calls {
		if k.project == project {
			delete(m.calls, k)
		}
	}
}

// memo runs fn once for concurrent callers of (method, project) and keeps a
// successful result for m.ttl. An error, a model.Partial one included, is
// returned to the callers already waiting but not kept, so the next call
// retries. fn runs on a context no single caller cancels; it is canceled
// only when every caller has given up.
func memo[T any](ctx context.Context, m *Memo, method, project string, fn func(context.Context, string) ([]T, error)) ([]T, error) {
	k := memoKey{method, project}
	m.mu.Lock()
	now := m.now()
	for key, c := range m.calls {
		if !c.exp.IsZero() && now.After(c.exp) {
			delete(m.calls, key)
		}
	}
	c, ok := m.calls[k]
	if !ok {
		cctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		c = &memoCall{done: make(chan struct{}), cancel: cancel}
		m.calls[k] = c
		go m.run(cctx, k, c, func(ctx context.Context) (any, error) { return fn(ctx, project) })
	}
	c.waiters++
	m.mu.Unlock()

	select {
	case <-c.done:
		m.mu.Lock()
		c.waiters--
		m.mu.Unlock()
		v, _ := c.val.([]T)
		return v, c.err
	case <-ctx.Done():
		m.mu.Lock()
		c.waiters--
		if c.waiters == 0 && c.exp.IsZero() {
			c.cancel()
			if m.calls[k] == c {
				delete(m.calls, k)
			}
		}
		m.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (m *Memo) run(ctx context.Context, k memoKey, c *memoCall, fn func(context.Context) (any, error)) {
	defer c.cancel()
	v, err := fn(ctx)
	c.val, c.err = v, err
	m.mu.Lock()
	if m.calls[k] == c {
		if err == nil {
			c.exp = m.now().Add(m.ttl)
		} else {
			delete(m.calls, k)
		}
	}
	m.mu.Unlock()
	close(c.done)
}

func (m *Memo) ListNetworks(ctx context.Context, project string) ([]Network, error) {
	return memo(ctx, m, "networks", project, m.API.ListNetworks)
}

func (m *Memo) ListSubnets(ctx context.Context, project string) ([]Subnet, error) {
	return memo(ctx, m, "subnets", project, m.API.ListSubnets)
}

func (m *Memo) ListFirewalls(ctx context.Context, project string) ([]Firewall, error) {
	return memo(ctx, m, "firewalls", project, m.API.ListFirewalls)
}

func (m *Memo) ListInstances(ctx context.Context, project string) ([]Instance, error) {
	return memo(ctx, m, "instances", project, m.API.ListInstances)
}

func (m *Memo) ListAddresses(ctx context.Context, project string) ([]Address, error) {
	return memo(ctx, m, "addresses", project, m.API.ListAddresses)
}

func (m *Memo) ListForwardingRules(ctx context.Context, project string) ([]ForwardingRule, error) {
	return memo(ctx, m, "forwardingRules", project, m.API.ListForwardingRules)
}

func (m *Memo) ListServiceAttachments(ctx context.Context, project string) ([]ServiceAttachment, error) {
	return memo(ctx, m, "serviceAttachments", project, m.API.ListServiceAttachments)
}

func (m *Memo) ListURLMaps(ctx context.Context, project string) ([]URLMap, error) {
	return memo(ctx, m, "urlMaps", project, m.API.ListURLMaps)
}

func (m *Memo) ListBackendServices(ctx context.Context, project string) ([]BackendService, error) {
	return memo(ctx, m, "backendServices", project, m.API.ListBackendServices)
}

func (m *Memo) ListTargetProxies(ctx context.Context, project string) ([]TargetProxy, error) {
	return memo(ctx, m, "targetProxies", project, m.API.ListTargetProxies)
}

func (m *Memo) ListHealthChecks(ctx context.Context, project string) ([]HealthCheck, error) {
	return memo(ctx, m, "healthChecks", project, m.API.ListHealthChecks)
}

func (m *Memo) ListSecurityPolicies(ctx context.Context, project string) ([]SecurityPolicy, error) {
	return memo(ctx, m, "securityPolicies", project, m.API.ListSecurityPolicies)
}

func (m *Memo) ListSSLPolicies(ctx context.Context, project string) ([]SSLPolicy, error) {
	return memo(ctx, m, "sslPolicies", project, m.API.ListSSLPolicies)
}

func (m *Memo) ListDisks(ctx context.Context, project string) ([]Disk, error) {
	return memo(ctx, m, "disks", project, m.API.ListDisks)
}

func (m *Memo) ListInstanceTemplates(ctx context.Context, project string) ([]InstanceTemplate, error) {
	return memo(ctx, m, "instanceTemplates", project, m.API.ListInstanceTemplates)
}

func (m *Memo) ListInstanceGroups(ctx context.Context, project string) ([]InstanceGroup, error) {
	return memo(ctx, m, "instanceGroups", project, m.API.ListInstanceGroups)
}

func (m *Memo) ListRoutes(ctx context.Context, project string) ([]Route, error) {
	return memo(ctx, m, "routes", project, m.API.ListRoutes)
}

func (m *Memo) ListRouters(ctx context.Context, project string) ([]Router, error) {
	return memo(ctx, m, "routers", project, m.API.ListRouters)
}

var (
	_ API               = (*Memo)(nil)
	_ model.Invalidator = (*Memo)(nil)
)
