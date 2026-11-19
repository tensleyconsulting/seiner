package gcp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/crm"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/iam"
	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
)

type countingCloser struct{ n atomic.Int32 }

func (c *countingCloser) Close() error {
	c.n.Add(1)
	return nil
}

func TestCatalogClose(t *testing.T) {
	t.Parallel()
	closer := &countingCloser{}
	tests := []struct {
		name string
		cat  Catalog
		want int32
	}{
		{name: "zero", cat: Catalog{}},
		{name: "fixtures", cat: FakeCatalog()},
		// Value copies made after withClients share its closer.
		{name: "live", cat: NewCatalog().withClients(closer).WithQuotaProject("q"), want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cat.Close(); err != nil {
				t.Fatal(err)
			}
			if got := closer.n.Load(); got != tt.want {
				t.Fatalf("closer called %d times, want %d", got, tt.want)
			}
		})
	}
}

// The Resource Manager clients are built with options that need no network
// or credentials (a gRPC client does not connect until its first call), so
// the test can see Close reach real clients: closing one twice fails.
func TestLiveClientsCloseOnce(t *testing.T) {
	t.Parallel()
	opts := []option.ClientOption{option.WithoutAuthentication(), option.WithEndpoint("127.0.0.1:1")}
	g := &lazy.Group{}
	rm := crm.NewLazyClients(g, opts...)
	unbuilt := crm.NewLazyClients(g, opts...)
	_, _ = liveProviders(g, rm, opts) // builds nothing by itself
	c, err := rm.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	if err := c.Projects.Close(); err == nil {
		t.Fatal("projects client was not closed")
	}
	if err := g.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
	// A set never built is not built just to be closed; building it now
	// that the group is closed fails.
	if _, err := unbuilt.Get(context.Background()); err == nil {
		t.Fatal("client built after Close is usable")
	}
}

// crm (tree and permission checks) and iam (policies and node names) share
// one Resource Manager client set: it builds once for both.
func TestResourceManagerClientsShared(t *testing.T) {
	t.Parallel()
	errBuild := errors.New("no clients")
	var builds atomic.Int32
	rm := lazy.New(func(context.Context) (crm.Clients, error) {
		builds.Add(1)
		return crm.Clients{}, errBuild
	})
	ctx := context.Background()
	tree, policies := crm.NewLazy(rm), iam.NewLazyAPI(rm)
	calls := []struct {
		name string
		call func() error
	}{
		{"crm GetOrg", func() error { _, err := tree.GetOrg(ctx, "organizations/1"); return err }},
		{"crm TestPermissions", func() error { _, err := tree.TestPermissions(ctx, "projects/p", nil); return err }},
		{"iam GetPolicy", func() error { _, err := policies.GetPolicy(ctx, "projects/p"); return err }},
		{"iam GetNode", func() error { _, err := policies.GetNode(ctx, "folders/1"); return err }},
	}
	for _, c := range calls {
		if err := c.call(); !errors.Is(err, errBuild) {
			t.Errorf("%s: err = %v, want the shared build error", c.name, err)
		}
	}
	if n := builds.Load(); n != 1 {
		t.Fatalf("client set built %d times, want 1", n)
	}
}
