package vpcsc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	acm "google.golang.org/api/accesscontextmanager/v1"
	crm "google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
	"github.com/tensleyconsulting/seiner/internal/model"
)

type liveAPI struct {
	acm *acm.Service
	crm *crm.Service
}

// NewLazy returns an API that builds each client on first use, so a slow or
// failing constructor cannot block startup; the failure surfaces as the
// calling provider's ProviderError instead. Policies and Perimeters share
// it.
func NewLazy(opts ...option.ClientOption) API {
	return &lazyAPI{
		acm: lazy.New(func(ctx context.Context) (*acm.Service, error) { return acm.NewService(ctx, opts...) }),
		crm: lazy.New(func(ctx context.Context) (*crm.Service, error) { return crm.NewService(ctx, opts...) }),
	}
}

// lazyAPI builds each client on first use. A project's number and
// organization never change, so each is looked up once per session.
type lazyAPI struct {
	acm *lazy.Value[*acm.Service]
	crm *lazy.Value[*crm.Service]

	mu       sync.Mutex
	projects map[string]Project
}

func (a *lazyAPI) ListPolicies(ctx context.Context, org string) ([]Policy, error) {
	c, err := a.acm.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{acm: c}.ListPolicies(ctx, org)
}

func (a *lazyAPI) ListPerimeters(ctx context.Context, policy string) ([]Perimeter, error) {
	c, err := a.acm.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{acm: c}.ListPerimeters(ctx, policy)
}

func (a *lazyAPI) GetPerimeter(ctx context.Context, policy, name string) (Perimeter, error) {
	c, err := a.acm.Get(ctx)
	if err != nil {
		return Perimeter{}, err
	}
	return liveAPI{acm: c}.GetPerimeter(ctx, policy, name)
}

func (a *lazyAPI) ListLevels(ctx context.Context, policy string) ([]Level, error) {
	c, err := a.acm.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{acm: c}.ListLevels(ctx, policy)
}

func (a *lazyAPI) Project(ctx context.Context, project string) (Project, error) {
	a.mu.Lock()
	p, ok := a.projects[project]
	a.mu.Unlock()
	if ok {
		return p, nil
	}
	c, err := a.crm.Get(ctx)
	if err != nil {
		return Project{}, err
	}
	p, err = liveAPI{crm: c}.Project(ctx, project)
	if err != nil {
		return Project{}, err
	}
	a.mu.Lock()
	if a.projects == nil {
		a.projects = map[string]Project{}
	}
	a.projects[project] = p
	a.mu.Unlock()
	return p, nil
}

var (
	_ API = (*lazyAPI)(nil)
	_ API = liveAPI{}
)

func (a liveAPI) ListPolicies(ctx context.Context, org string) ([]Policy, error) {
	var out []Policy
	err := a.acm.AccessPolicies.List().Parent(org).Context(ctx).Pages(ctx, func(page *acm.ListAccessPoliciesResponse) error {
		for _, p := range page.AccessPolicies {
			out = append(out, fromPolicy(p))
		}
		return nil
	})
	if err != nil {
		return nil, denied(err)
	}
	return out, nil
}

func (a liveAPI) ListPerimeters(ctx context.Context, policy string) ([]Perimeter, error) {
	var out []Perimeter
	err := a.acm.AccessPolicies.ServicePerimeters.List("accessPolicies/"+policy).Context(ctx).Pages(ctx, func(page *acm.ListServicePerimetersResponse) error {
		for _, p := range page.ServicePerimeters {
			out = append(out, fromPerimeter(p))
		}
		return nil
	})
	if err != nil {
		return nil, denied(err)
	}
	return out, nil
}

func (a liveAPI) GetPerimeter(ctx context.Context, policy, name string) (Perimeter, error) {
	p, err := a.acm.AccessPolicies.ServicePerimeters.Get("accessPolicies/" + policy + "/servicePerimeters/" + name).Context(ctx).Do()
	if err != nil {
		var gerr *googleapi.Error
		if errors.As(err, &gerr) && gerr.Code == http.StatusNotFound {
			return Perimeter{}, fmt.Errorf("%w: %w", model.ErrNotFound, err)
		}
		return Perimeter{}, denied(err)
	}
	return fromPerimeter(p), nil
}

func (a liveAPI) ListLevels(ctx context.Context, policy string) ([]Level, error) {
	var out []Level
	// AS_DEFINED keeps basic levels as conditions instead of rewriting
	// them as CEL.
	call := a.acm.AccessPolicies.AccessLevels.List("accessPolicies/" + policy).AccessLevelFormat("AS_DEFINED")
	err := call.Context(ctx).Pages(ctx, func(page *acm.ListAccessLevelsResponse) error {
		for _, l := range page.AccessLevels {
			out = append(out, fromLevel(l))
		}
		return nil
	})
	if err != nil {
		return nil, denied(err)
	}
	return out, nil
}

// Project reads the project's number, and its organization from its
// ancestry, which walks folders without needing access to them.
func (a liveAPI) Project(ctx context.Context, project string) (Project, error) {
	p, err := a.crm.Projects.Get(project).Context(ctx).Do()
	if err != nil {
		return Project{}, err
	}
	anc, err := a.crm.Projects.GetAncestry(project, &crm.GetAncestryRequest{}).Context(ctx).Do()
	if err != nil {
		return Project{}, err
	}
	out := Project{Number: strconv.FormatInt(p.ProjectNumber, 10)}
	for _, an := range anc.Ancestor {
		if an.ResourceId != nil && an.ResourceId.Type == "organization" {
			out.Org = "organizations/" + an.ResourceId.Id
		}
	}
	return out, nil
}

// denied wraps a permission-denied error with ErrDenied, keeping the Google
// error in the chain. A 403 for a disabled API is not a denial.
func denied(err error) error {
	var gerr *googleapi.Error
	if !errors.As(err, &gerr) || gerr.Code != http.StatusForbidden {
		return err
	}
	for _, e := range gerr.Errors {
		if e.Reason == "accessNotConfigured" || e.Reason == "SERVICE_DISABLED" {
			return err
		}
	}
	if strings.Contains(gerr.Message, "has not been used") || strings.Contains(gerr.Message, "is disabled") {
		return err
	}
	return fmt.Errorf("%w: %w", ErrDenied, err)
}

func fromPolicy(p *acm.AccessPolicy) Policy {
	return Policy{
		Name:   strings.TrimPrefix(p.Name, "accessPolicies/"),
		Title:  p.Title,
		Scopes: p.Scopes,
	}
}

func fromPerimeter(p *acm.ServicePerimeter) Perimeter {
	out := Perimeter{
		Name:        lastSeg(p.Name),
		Title:       p.Title,
		Description: p.Description,
		Type:        p.PerimeterType,
		Status:      fromConfig(p.Status),
	}
	if p.UseExplicitDryRunSpec {
		out.Spec = fromConfig(p.Spec)
		if out.Spec == nil {
			out.Spec = &Config{}
		}
	}
	return out
}

func fromConfig(c *acm.ServicePerimeterConfig) *Config {
	if c == nil {
		return nil
	}
	out := &Config{
		Resources:          c.Resources,
		RestrictedServices: c.RestrictedServices,
	}
	for _, p := range c.IngressPolicies {
		out.Ingress = append(out.Ingress, fromIngress(p))
	}
	for _, p := range c.EgressPolicies {
		out.Egress = append(out.Egress, fromEgress(p))
	}
	for _, l := range c.AccessLevels {
		out.AccessLevels = append(out.AccessLevels, lastSeg(l))
	}
	if v := c.VpcAccessibleServices; v != nil && v.EnableRestriction {
		out.VPCRestricted = true
		out.VPCAllowed = v.AllowedServices
	}
	return out
}

func fromLevel(l *acm.AccessLevel) Level {
	out := Level{
		Name:        lastSeg(l.Name),
		Title:       l.Title,
		Description: l.Description,
	}
	if l.Custom != nil && l.Custom.Expr != nil {
		out.Expression = l.Custom.Expr.Expression
	}
	if b := l.Basic; b != nil {
		out.Combining = b.CombiningFunction
		for _, c := range b.Conditions {
			out.Conditions = append(out.Conditions, fromCondition(c))
		}
	}
	return out
}

func fromCondition(c *acm.Condition) Condition {
	out := Condition{
		IPSubnetworks: c.IpSubnetworks,
		Regions:       c.Regions,
		Members:       c.Members,
		Device:        fromDevice(c.DevicePolicy),
		Negate:        c.Negate,
	}
	for _, l := range c.RequiredAccessLevels {
		out.RequiredLevels = append(out.RequiredLevels, lastSeg(l))
	}
	for _, v := range c.VpcNetworkSources {
		if v.VpcSubnetwork != nil {
			out.VPCNetworks = append(out.VPCNetworks, v.VpcSubnetwork.Network)
		}
	}
	return out
}

func fromIngress(p *acm.IngressPolicy) Rule {
	out := Rule{Title: p.Title}
	if f := p.IngressFrom; f != nil {
		out.Identities = f.Identities
		out.IdentityType = identityType(f.IdentityType)
		for _, src := range f.Sources {
			out.Sources = append(out.Sources, source(src.AccessLevel, src.Resource, src.PscEndpoint))
		}
	}
	if t := p.IngressTo; t != nil {
		out.Operations = operations(t.Operations)
		out.Resources = t.Resources
		out.Roles = t.Roles
	}
	return out
}

func fromEgress(p *acm.EgressPolicy) Rule {
	out := Rule{Title: p.Title}
	if f := p.EgressFrom; f != nil {
		out.Identities = f.Identities
		out.IdentityType = identityType(f.IdentityType)
		if f.SourceRestriction != "SOURCE_RESTRICTION_UNSPECIFIED" {
			out.SourceRestriction = f.SourceRestriction
		}
		for _, src := range f.Sources {
			out.Sources = append(out.Sources, source(src.AccessLevel, src.Resource, src.PscEndpoint))
		}
	}
	if t := p.EgressTo; t != nil {
		out.Operations = operations(t.Operations)
		out.Resources = t.Resources
		out.ExternalResources = t.ExternalResources
		out.Roles = t.Roles
	}
	return out
}

func identityType(t string) string {
	if t == "IDENTITY_TYPE_UNSPECIFIED" {
		return ""
	}
	return t
}

// source reads one rule source: an access level's short name ("*" stays
// "*"), a resource, or a PSC endpoint.
func source(level, resource string, psc *acm.PrivateServiceConnectEndpoint) string {
	switch {
	case level != "":
		return lastSeg(level)
	case resource != "":
		return resource
	case psc != nil && psc.ForwardingRule != "":
		return "psc " + psc.ForwardingRule
	}
	return ""
}

func operations(ops []*acm.ApiOperation) []Operation {
	var out []Operation
	for _, op := range ops {
		o := Operation{Service: op.ServiceName}
		for _, m := range op.MethodSelectors {
			switch {
			case m.Method != "":
				o.Methods = append(o.Methods, m.Method)
			case m.Permission != "":
				o.Methods = append(o.Methods, "permission "+m.Permission)
			}
		}
		out = append(out, o)
	}
	return out
}

func fromDevice(d *acm.DevicePolicy) *Device {
	if d == nil {
		return nil
	}
	out := &Device{
		Encryption:    d.AllowedEncryptionStatuses,
		Management:    d.AllowedDeviceManagementLevels,
		Screenlock:    d.RequireScreenlock,
		CorpOwned:     d.RequireCorpOwned,
		AdminApproval: d.RequireAdminApproval,
	}
	for _, o := range d.OsConstraints {
		out.OS = append(out.OS, OS{Type: o.OsType, MinVersion: o.MinimumVersion, Verified: o.RequireVerifiedChromeOs})
	}
	return out
}

func lastSeg(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}
