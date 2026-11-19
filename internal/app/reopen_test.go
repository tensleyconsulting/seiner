package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tensleyconsulting/seiner/internal/config"
	"github.com/tensleyconsulting/seiner/internal/gcp"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// closingCatalog is the fixture catalog with a counted Close.
type closingCatalog struct {
	model.Catalog
	closed *atomic.Int32
}

func (c closingCatalog) Close() error {
	c.closed.Add(1)
	return nil
}

func refreshKey() tea.KeyPressMsg { return keyText("r") }

func TestRefreshReopens(t *testing.T) {
	t.Parallel()
	signedIn := model.Identity{Account: "dev@example.com", Source: model.IdentityADC}
	noCreds := model.Identity{Source: model.IdentityNone, Err: errors.New("could not find default credentials")}
	rejected := model.ProviderError{Kind: model.ErrKindUnauthenticated, Message: "sign-in expired"}

	tests := []struct {
		name        string
		start       model.Catalog // nil: no catalog at startup
		startIdent  model.Identity
		rejected    bool // Google rejected the live credentials mid-session
		reopenSrc   bool // the reopener finds a catalog
		wantCalls   int32
		wantAccount string
		wantBanners int
		wantClosed  int32
	}{
		{name: "no credentials, then signed in", startIdent: noCreds, reopenSrc: true,
			wantCalls: 1, wantAccount: "dev@example.com", wantBanners: 0},
		{name: "no credentials, still none", startIdent: noCreds,
			wantCalls: 1, wantAccount: "not signed in", wantBanners: 1},
		{name: "rejected, then signed in", start: gcp.FakeCatalog(), startIdent: testIdent(), rejected: true, reopenSrc: true,
			wantCalls: 1, wantAccount: "dev@example.com", wantBanners: 0, wantClosed: 1},
		{name: "working credentials just reload", start: gcp.FakeCatalog(), startIdent: testIdent(),
			wantCalls: 0, wantAccount: testIdent().Account, wantBanners: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var calls, closed atomic.Int32
			var start model.Catalog
			if tt.start != nil {
				start = closingCatalog{Catalog: tt.start, closed: &closed}
			}
			m := New(config.Config{Project: "demo-project", IconMode: config.IconText}, "test", start, tt.startIdent)
			m = m.WithReopen(func(context.Context) (model.Catalog, model.Identity, string) {
				calls.Add(1)
				if !tt.reopenSrc {
					return nil, noCreds, ""
				}
				return gcp.FakeCatalog(), signedIn, "demo-project"
			})
			m = sized(load(m))
			if tt.rejected {
				m.noteAuth(rejected)
			}

			next, cmd := m.Update(refreshKey())
			if tt.wantCalls > 0 && !next.(Model).reopening {
				t.Fatal("reopening was not set on the returned Model before running cmd")
			}
			m = drain(next.(Model), cmd)

			if got := calls.Load(); got != tt.wantCalls {
				t.Fatalf("reopen calls = %d, want %d", got, tt.wantCalls)
			}
			if got := m.status.Account; got != tt.wantAccount {
				t.Errorf("account = %q, want %q", got, tt.wantAccount)
			}
			if got := len(m.banners()); got != tt.wantBanners {
				t.Errorf("banners = %q, want %d", m.banners(), tt.wantBanners)
			}
			if got := closed.Load(); got != tt.wantClosed {
				t.Errorf("old catalog closed %d times, want %d", got, tt.wantClosed)
			}
			if tt.reopenSrc && len(m.catalog) == 0 {
				t.Error("reopened catalog loaded nothing")
			}
		})
	}
}

// The demo has no reopener, so refresh never reads credentials.
func TestRefreshWithoutReopenerReloads(t *testing.T) {
	t.Parallel()
	m := sized(newTestModel())
	m.noteAuth(model.ProviderError{Kind: model.ErrKindUnauthenticated})
	if m.needsReopen() {
		t.Fatal("no reopener, yet refresh would reopen")
	}
}
