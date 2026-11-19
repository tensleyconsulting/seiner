package acm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"

	acmapi "google.golang.org/api/accesscontextmanager/v1"
	ci "google.golang.org/api/cloudidentity/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/vpcsc"
	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// saFilter asks the binding list for service account bindings; without a
// filter it returns group bindings only.
const saFilter = "principal:service_account OR principal:service_account_project_number"

type liveAPI struct {
	acm    *acmapi.Service
	ci     *ci.Service
	levels vpcsc.API
}

// NewLazy defers client construction to the first call, so a slow or failing
// constructor cannot block startup; the failure surfaces as this service's
// ProviderError instead. Access levels are read through levels, the VPC
// Service Controls API, which is shared so its client builds once.
func NewLazy(levels vpcsc.API, opts ...option.ClientOption) Bindings {
	return NewBindings(&lazyAPI{
		acm:    lazy.New(func(ctx context.Context) (*acmapi.Service, error) { return acmapi.NewService(ctx, opts...) }),
		ci:     lazy.New(func(ctx context.Context) (*ci.Service, error) { return ci.NewService(ctx, opts...) }),
		levels: levels,
	})
}

// lazyAPI builds each client on first use. A group keeps its id when its
// email changes, but renames are rare enough that each group is looked up
// once per session.
type lazyAPI struct {
	acm    *lazy.Value[*acmapi.Service]
	ci     *lazy.Value[*ci.Service]
	levels vpcsc.API

	mu     sync.Mutex
	groups map[string]Group
}

func (a *lazyAPI) ListBindings(ctx context.Context, org string) ([]Binding, error) {
	c, err := a.acm.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{acm: c}.ListBindings(ctx, org)
}

func (a *lazyAPI) ListServiceAccountBindings(ctx context.Context, org string) ([]Binding, error) {
	c, err := a.acm.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{acm: c}.ListServiceAccountBindings(ctx, org)
}

func (a *lazyAPI) GetBinding(ctx context.Context, org, name string) (Binding, error) {
	c, err := a.acm.Get(ctx)
	if err != nil {
		return Binding{}, err
	}
	return liveAPI{acm: c}.GetBinding(ctx, org, name)
}

func (a *lazyAPI) ListLevels(ctx context.Context, policy string) ([]vpcsc.Level, error) {
	return a.levels.ListLevels(ctx, policy)
}

func (a *lazyAPI) Group(ctx context.Context, key string) (Group, error) {
	a.mu.Lock()
	g, ok := a.groups[key]
	a.mu.Unlock()
	if ok {
		return g, nil
	}
	c, err := a.ci.Get(ctx)
	if err != nil {
		return Group{}, err
	}
	g, err = liveAPI{ci: c}.Group(ctx, key)
	if err != nil {
		return Group{}, err
	}
	a.mu.Lock()
	if a.groups == nil {
		a.groups = map[string]Group{}
	}
	a.groups[key] = g
	a.mu.Unlock()
	return g, nil
}

var (
	_ API = (*lazyAPI)(nil)
	_ API = liveAPI{}
)

func (a liveAPI) ListBindings(ctx context.Context, org string) ([]Binding, error) {
	return a.list(ctx, org, "")
}

func (a liveAPI) ListServiceAccountBindings(ctx context.Context, org string) ([]Binding, error) {
	return a.list(ctx, org, saFilter)
}

func (a liveAPI) GetBinding(ctx context.Context, org, name string) (Binding, error) {
	b, err := a.acm.Organizations.GcpUserAccessBindings.Get(org + "/gcpUserAccessBindings/" + name).Context(ctx).Do()
	if err != nil {
		var gerr *googleapi.Error
		if errors.As(err, &gerr) && gerr.Code == http.StatusNotFound {
			return Binding{}, fmt.Errorf("%w: %w", model.ErrNotFound, err)
		}
		return Binding{}, err
	}
	return fromBinding(b), nil
}

func (a liveAPI) ListLevels(ctx context.Context, policy string) ([]vpcsc.Level, error) {
	return a.levels.ListLevels(ctx, policy)
}

func (a liveAPI) list(ctx context.Context, org, filter string) ([]Binding, error) {
	call := a.acm.Organizations.GcpUserAccessBindings.List(org)
	if filter != "" {
		call = call.Filter(filter)
	}
	var out []Binding
	err := call.Context(ctx).Pages(ctx, func(page *acmapi.ListGcpUserAccessBindingsResponse) error {
		for _, b := range page.GcpUserAccessBindings {
			out = append(out, fromBinding(b))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Group reads groups/KEY, assuming Cloud Identity names a group by the
// same Directory id the binding's groupKey holds. Google does not document
// that they match; when the read fails, the binding lists by its id.
func (a liveAPI) Group(ctx context.Context, key string) (Group, error) {
	g, err := a.ci.Groups.Get("groups/" + key).Context(ctx).Do()
	if err != nil {
		return Group{}, err
	}
	out := Group{DisplayName: g.DisplayName}
	if g.GroupKey != nil {
		out.Email = g.GroupKey.Id
	}
	return out, nil
}

func fromBinding(b *acmapi.GcpUserAccessBinding) Binding {
	out := Binding{
		Name:         lastSeg(b.Name),
		GroupKey:     b.GroupKey,
		Levels:       shortLevels(b.AccessLevels),
		DryRunLevels: shortLevels(b.DryRunAccessLevels),
		Session:      fromSession(b.SessionSettings),
	}
	if p := b.Principal; p != nil {
		out.ServiceAccount = p.ServiceAccount
		out.SAProject = p.ServiceAccountProjectNumber
		out.Federated = p.FederatedPrincipal
	}
	refs := slices.Concat(b.AccessLevels, b.DryRunAccessLevels)
	for _, s := range b.ScopedAccessSettings {
		out.Scoped = append(out.Scoped, fromScoped(s))
		if a := s.ActiveSettings; a != nil {
			refs = append(refs, a.AccessLevels...)
		}
		if d := s.DryRunSettings; d != nil {
			refs = append(refs, d.AccessLevels...)
		}
	}
	out.Policy = policyOf(refs)
	return out
}

// policyOf is the policy number of the first access level name
// (accessPolicies/N/accessLevels/L) in refs. A binding's levels all come
// from the organization's access policy.
func policyOf(refs []string) string {
	for _, r := range refs {
		if rest, ok := strings.CutPrefix(r, "accessPolicies/"); ok {
			if n, _, ok := strings.Cut(rest, "/"); ok {
				return n
			}
		}
	}
	return ""
}

func fromScoped(s *acmapi.ScopedAccessSettings) Scoped {
	var out Scoped
	if s.Scope != nil && s.Scope.ClientScope != nil {
		cs := s.Scope.ClientScope
		if app := cs.RestrictedClientApplication; app != nil {
			out.App = app.Name
			if out.App == "" {
				out.App = app.ClientId
			}
		}
		if cs.RestrictedProject != nil {
			out.Project = cs.RestrictedProject.Name
		}
	}
	if a := s.ActiveSettings; a != nil {
		out.Levels = shortLevels(a.AccessLevels)
		out.Session = fromSession(a.SessionSettings)
	}
	if d := s.DryRunSettings; d != nil {
		out.DryRunLevels = shortLevels(d.AccessLevels)
		out.DryRunSession = fromSession(d.SessionSettings)
	}
	return out
}

func fromSession(s *acmapi.SessionSettings) *Session {
	if s == nil {
		return nil
	}
	return &Session{
		Length:        s.SessionLength,
		LengthEnabled: s.SessionLengthEnabled,
		MaxInactivity: s.MaxInactivity,
		ReauthMethod:  s.SessionReauthMethod,
		UseOIDCMaxAge: s.UseOidcMaxAge,
	}
}

func shortLevels(names []string) []string {
	var out []string
	for _, n := range names {
		out = append(out, lastSeg(n))
	}
	return out
}

func lastSeg(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}
