package iap

import (
	"context"
	"strings"
	"sync"
	"time"

	crm "google.golang.org/api/cloudresourcemanager/v3"
	gce "google.golang.org/api/compute/v1"
	iapapi "google.golang.org/api/iap/v1"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/iam"
	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
)

type liveAPI struct {
	gce *gce.Service
	iap *iapapi.Service
	crm *crm.Service
}

// NewLazy defers client construction to the first call, so a slow or failing
// constructor cannot block startup; the failure surfaces as this service's
// ProviderError instead. policies is shared with the IAM node.
func NewLazy(policies iam.API, opts ...option.ClientOption) Apps {
	return NewApps(&lazyAPI{
		gce: lazy.New(func(ctx context.Context) (*gce.Service, error) { return gce.NewService(ctx, opts...) }),
		iap: lazy.New(func(ctx context.Context) (*iapapi.Service, error) { return iapapi.NewService(ctx, opts...) }),
		crm: lazy.New(func(ctx context.Context) (*crm.Service, error) { return crm.NewService(ctx, opts...) }),
	}, policies)
}

// lazyAPI builds each client on first use. Project numbers never change, so
// each is looked up once per session.
type lazyAPI struct {
	gce *lazy.Value[*gce.Service]
	iap *lazy.Value[*iapapi.Service]
	crm *lazy.Value[*crm.Service]

	mu      sync.Mutex
	numbers map[string]string
}

func (a *lazyAPI) ProjectNumber(ctx context.Context, project string) (string, error) {
	a.mu.Lock()
	n, ok := a.numbers[project]
	a.mu.Unlock()
	if ok {
		return n, nil
	}
	c, err := a.crm.Get(ctx)
	if err != nil {
		return "", err
	}
	n, err = liveAPI{crm: c}.ProjectNumber(ctx, project)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	if a.numbers == nil {
		a.numbers = map[string]string{}
	}
	a.numbers[project] = n
	a.mu.Unlock()
	return n, nil
}

func (a *lazyAPI) ListBackends(ctx context.Context, project string) ([]Backend, error) {
	c, err := a.gce.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{gce: c}.ListBackends(ctx, project)
}

func (a *lazyAPI) GetSettings(ctx context.Context, resource string) (Settings, error) {
	c, err := a.iap.Get(ctx)
	if err != nil {
		return Settings{}, err
	}
	return liveAPI{iap: c}.GetSettings(ctx, resource)
}

func (a *lazyAPI) GetPolicy(ctx context.Context, resource string) ([]Binding, error) {
	c, err := a.iap.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{iap: c}.GetPolicy(ctx, resource)
}

var (
	_ API = (*lazyAPI)(nil)
	_ API = liveAPI{}
)

func (a liveAPI) ProjectNumber(ctx context.Context, project string) (string, error) {
	p, err := a.crm.Projects.Get("projects/" + project).Context(ctx).Do()
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(p.Name, "projects/"), nil
}

// ListBackends reads every backend service, global and regional, and keeps
// those with IAP on.
func (a liveAPI) ListBackends(ctx context.Context, project string) ([]Backend, error) {
	var out []Backend
	err := a.gce.BackendServices.AggregatedList(project).Context(ctx).Pages(ctx, func(page *gce.BackendServiceAggregatedList) error {
		for _, scoped := range page.Items {
			for _, b := range scoped.BackendServices {
				if b.Iap != nil && b.Iap.Enabled {
					out = append(out, fromBackend(b))
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a liveAPI) GetSettings(ctx context.Context, resource string) (Settings, error) {
	s, err := a.iap.V1.GetIapSettings(resource).Context(ctx).Do()
	if err != nil {
		return Settings{}, err
	}
	return fromSettings(s), nil
}

func (a liveAPI) GetPolicy(ctx context.Context, resource string) ([]Binding, error) {
	// Version 3 returns conditional bindings with their conditions.
	req := &iapapi.GetIamPolicyRequest{Options: &iapapi.GetPolicyOptions{RequestedPolicyVersion: 3}}
	p, err := a.iap.V1.GetIamPolicy(resource, req).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	out := make([]Binding, 0, len(p.Bindings))
	for _, b := range p.Bindings {
		bd := Binding{Role: b.Role, Members: b.Members}
		if b.Condition != nil {
			bd.Condition = b.Condition.Title
			if bd.Condition == "" {
				bd.Condition = b.Condition.Expression
			}
		}
		out = append(out, bd)
	}
	return out, nil
}

// fromBackend copies the fields IAP rows show. The OAuth client secret is
// input-only in the API and is not copied in any case.
func fromBackend(b *gce.BackendService) Backend {
	out := Backend{
		Name:        b.Name,
		Region:      lastSeg(b.Region),
		Description: b.Description,
		Scheme:      b.LoadBalancingScheme,
		Protocol:    b.Protocol,
		Created:     parseTime(b.CreationTimestamp),
	}
	if b.Iap != nil {
		out.ClientID = b.Iap.Oauth2ClientId
	}
	return out
}

// fromSettings copies the settings IAP rows show. OAuth client secrets are
// not copied.
func fromSettings(s *iapapi.IapSettings) Settings {
	var out Settings
	if as := s.AccessSettings; as != nil {
		if r := as.ReauthSettings; r != nil {
			out.ReauthMethod = r.Method
			out.ReauthMaxAge = r.MaxAge
			out.ReauthPolicy = r.PolicyType
		}
		if d := as.AllowedDomainsSettings; d != nil && d.Enable {
			out.AllowedDomains = d.Domains
		}
		if c := as.CorsSettings; c != nil {
			out.CORSPreflight = c.AllowHttpOptions
		}
		out.IdentitySources = as.IdentitySources
		if g := as.GcipSettings; g != nil {
			out.GCIPTenants = g.TenantIds
		}
		if o := as.OauthSettings; o != nil {
			out.LoginHint = o.LoginHint
			out.ProgrammaticClients = o.ProgrammaticClients
		}
	}
	if ap := s.ApplicationSettings; ap != nil {
		out.CookieDomain = ap.CookieDomain
		if p := ap.AccessDeniedPageSettings; p != nil {
			out.AccessDeniedPage = p.AccessDeniedPageUri
		}
	}
	return out
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func lastSeg(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}
