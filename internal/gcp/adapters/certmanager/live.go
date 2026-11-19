package certmanager

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	cm "google.golang.org/api/certificatemanager/v1"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
	"github.com/tensleyconsulting/seiner/internal/gcp/regions"
)

type liveAPI struct{ svc *cm.Service }

// NewLazy defers client construction to the first call, so a slow or failing
// constructor cannot block startup; the failure surfaces as that service's
// ProviderError instead. Share the result across this package's providers so
// the client and location list are built once.
func NewLazy(opts ...option.ClientOption) API {
	svc := lazy.New(func(ctx context.Context) (*cm.Service, error) {
		return cm.NewService(ctx, opts...)
	})
	return lazyAPI{
		svc: svc,
		locs: regions.Cached(func(ctx context.Context, project string) ([]string, error) {
			c, err := svc.Get(ctx)
			if err != nil {
				return nil, err
			}
			return liveAPI{svc: c}.Locations(ctx, project)
		}),
	}
}

// lazyAPI builds the client on first use. The location list is cached for
// the session, since it changes rarely and every list starts with it.
type lazyAPI struct {
	svc  *lazy.Value[*cm.Service]
	locs regions.Lister
}

func (a lazyAPI) Locations(ctx context.Context, project string) ([]string, error) {
	return a.locs.Regions(ctx, project)
}

func (a lazyAPI) ListCertificates(ctx context.Context, project, location string) ([]Certificate, error) {
	c, err := a.svc.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: c}.ListCertificates(ctx, project, location)
}

func (a lazyAPI) ListMaps(ctx context.Context, project, location string) ([]Map, error) {
	c, err := a.svc.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: c}.ListMaps(ctx, project, location)
}

func (a lazyAPI) ListMapEntries(ctx context.Context, project, location, certMap string, limit int) ([]MapEntry, bool, error) {
	c, err := a.svc.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveAPI{svc: c}.ListMapEntries(ctx, project, location, certMap, limit)
}

func (a lazyAPI) ListDNSAuthorizations(ctx context.Context, project, location string) ([]DNSAuthorization, error) {
	c, err := a.svc.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: c}.ListDNSAuthorizations(ctx, project, location)
}

func (a lazyAPI) ListIssuanceConfigs(ctx context.Context, project, location string) ([]IssuanceConfig, error) {
	c, err := a.svc.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: c}.ListIssuanceConfigs(ctx, project, location)
}

func (a lazyAPI) ListTrustConfigs(ctx context.Context, project, location string) ([]TrustConfig, error) {
	c, err := a.svc.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: c}.ListTrustConfigs(ctx, project, location)
}

var (
	_ API = lazyAPI{}
	_ API = liveAPI{}
)

// errStop ends paging once the entry limit is reached.
var errStop = errors.New("stop")

func (a liveAPI) Locations(ctx context.Context, project string) ([]string, error) {
	var out []string
	err := a.svc.Projects.Locations.List("projects/"+project).Pages(ctx, func(resp *cm.ListLocationsResponse) error {
		for _, l := range resp.Locations {
			out = append(out, l.LocationId)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a liveAPI) ListCertificates(ctx context.Context, project, location string) ([]Certificate, error) {
	var out []Certificate
	err := a.svc.Projects.Locations.Certificates.List(parent(project, location)).Pages(ctx, func(resp *cm.ListCertificatesResponse) error {
		for _, c := range resp.Certificates {
			out = append(out, fromCertificate(location, c))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a liveAPI) ListMaps(ctx context.Context, project, location string) ([]Map, error) {
	var out []Map
	err := a.svc.Projects.Locations.CertificateMaps.List(parent(project, location)).Pages(ctx, func(resp *cm.ListCertificateMapsResponse) error {
		for _, m := range resp.CertificateMaps {
			out = append(out, fromMap(location, m))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a liveAPI) ListMapEntries(ctx context.Context, project, location, certMap string, limit int) ([]MapEntry, bool, error) {
	var out []MapEntry
	truncated := false
	name := parent(project, location) + "/certificateMaps/" + certMap
	err := a.svc.Projects.Locations.CertificateMaps.CertificateMapEntries.List(name).Pages(ctx, func(resp *cm.ListCertificateMapEntriesResponse) error {
		for _, e := range resp.CertificateMapEntries {
			if len(out) == limit {
				truncated = true
				return errStop
			}
			out = append(out, fromEntry(e))
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return nil, false, err
	}
	return out, truncated, nil
}

func (a liveAPI) ListDNSAuthorizations(ctx context.Context, project, location string) ([]DNSAuthorization, error) {
	var out []DNSAuthorization
	err := a.svc.Projects.Locations.DnsAuthorizations.List(parent(project, location)).Pages(ctx, func(resp *cm.ListDnsAuthorizationsResponse) error {
		for _, d := range resp.DnsAuthorizations {
			out = append(out, fromDNSAuth(location, d))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a liveAPI) ListIssuanceConfigs(ctx context.Context, project, location string) ([]IssuanceConfig, error) {
	var out []IssuanceConfig
	err := a.svc.Projects.Locations.CertificateIssuanceConfigs.List(parent(project, location)).Pages(ctx, func(resp *cm.ListCertificateIssuanceConfigsResponse) error {
		for _, c := range resp.CertificateIssuanceConfigs {
			out = append(out, fromIssuance(location, c))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a liveAPI) ListTrustConfigs(ctx context.Context, project, location string) ([]TrustConfig, error) {
	var out []TrustConfig
	err := a.svc.Projects.Locations.TrustConfigs.List(parent(project, location)).Pages(ctx, func(resp *cm.ListTrustConfigsResponse) error {
		for _, t := range resp.TrustConfigs {
			out = append(out, fromTrust(location, t))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// fromCertificate copies what certificate rows show. The PEM body and
// private key are never copied.
func fromCertificate(location string, c *cm.Certificate) Certificate {
	out := Certificate{
		Name:        lastSeg(c.Name),
		Location:    location,
		Description: c.Description,
		SANs:        c.SanDnsnames,
		Expires:     parseTime(c.ExpireTime),
		Created:     parseTime(c.CreateTime),
		Updated:     parseTime(c.UpdateTime),
		Labels:      c.Labels,
	}
	if c.Scope != "DEFAULT" {
		out.Scope = c.Scope
	}
	for _, u := range c.UsedBy {
		out.UsedBy = append(out.UsedBy, u.Name)
	}
	switch {
	case c.Managed != nil:
		m := c.Managed
		out.Type = "MANAGED"
		out.State = m.State
		out.Issue = issue(m.ProvisioningIssue)
		out.Domains = m.Domains
		out.DNSAuthorizations = m.DnsAuthorizations
		out.IssuanceConfig = m.IssuanceConfig
		for _, a := range m.AuthorizationAttemptInfo {
			au := Authorization{Domain: a.Domain, State: a.State, Details: a.Details}
			if a.FailureReason != "FAILURE_REASON_UNSPECIFIED" {
				au.Reason = a.FailureReason
			}
			out.Authorizations = append(out.Authorizations, au)
		}
	case c.ManagedIdentity != nil:
		out.Type = "MANAGED_IDENTITY"
		out.State = c.ManagedIdentity.State
		out.Issue = issue(c.ManagedIdentity.ProvisioningIssue)
		out.Identity = c.ManagedIdentity.Identity
	case c.SelfManaged != nil:
		out.Type = "SELF_MANAGED"
	}
	if out.State == "STATE_UNSPECIFIED" {
		out.State = ""
	}
	return out
}

// issue reads a provisioning issue as "REASON: details".
func issue(p *cm.ProvisioningIssue) string {
	if p == nil {
		return ""
	}
	reason := p.Reason
	if reason == "REASON_UNSPECIFIED" {
		reason = ""
	}
	switch {
	case reason != "" && p.Details != "":
		return reason + ": " + p.Details
	case reason != "":
		return reason
	}
	return p.Details
}

func fromMap(location string, m *cm.CertificateMap) Map {
	out := Map{
		Name:        lastSeg(m.Name),
		Location:    location,
		Description: m.Description,
		Created:     parseTime(m.CreateTime),
		Updated:     parseTime(m.UpdateTime),
		Labels:      m.Labels,
	}
	for _, t := range m.GclbTargets {
		switch {
		case t.TargetHttpsProxy != "":
			out.Targets = append(out.Targets, t.TargetHttpsProxy)
		case t.TargetSslProxy != "":
			out.Targets = append(out.Targets, t.TargetSslProxy)
		}
		for _, ip := range t.IpConfigs {
			ports := make([]string, 0, len(ip.Ports))
			for _, p := range ip.Ports {
				ports = append(ports, strconv.FormatInt(p, 10))
			}
			addr := ip.IpAddress
			if len(ports) > 0 {
				addr += ":" + strings.Join(ports, ",")
			}
			out.Addresses = append(out.Addresses, addr)
		}
	}
	return out
}

func fromEntry(e *cm.CertificateMapEntry) MapEntry {
	out := MapEntry{
		Name:         lastSeg(e.Name),
		Description:  e.Description,
		Hostname:     e.Hostname,
		Matcher:      e.Matcher,
		State:        e.State,
		Certificates: e.Certificates,
		Created:      parseTime(e.CreateTime),
		Updated:      parseTime(e.UpdateTime),
		Labels:       e.Labels,
	}
	if out.Matcher == "MATCHER_UNSPECIFIED" {
		out.Matcher = ""
	}
	if out.State == "SERVING_STATE_UNSPECIFIED" {
		out.State = ""
	}
	return out
}

func fromDNSAuth(location string, d *cm.DnsAuthorization) DNSAuthorization {
	out := DNSAuthorization{
		Name:        lastSeg(d.Name),
		Location:    location,
		Description: d.Description,
		Domain:      d.Domain,
		Type:        d.Type,
		Created:     parseTime(d.CreateTime),
		Updated:     parseTime(d.UpdateTime),
		Labels:      d.Labels,
	}
	if out.Type == "TYPE_UNSPECIFIED" {
		out.Type = ""
	}
	if r := d.DnsResourceRecord; r != nil {
		out.RecordName, out.RecordType, out.RecordData = r.Name, r.Type, r.Data
	}
	return out
}

func fromIssuance(location string, c *cm.CertificateIssuanceConfig) IssuanceConfig {
	out := IssuanceConfig{
		Name:           lastSeg(c.Name),
		Location:       location,
		Description:    c.Description,
		KeyAlgorithm:   c.KeyAlgorithm,
		Lifetime:       c.Lifetime,
		RotationWindow: c.RotationWindowPercentage,
		Created:        parseTime(c.CreateTime),
		Updated:        parseTime(c.UpdateTime),
		Labels:         c.Labels,
	}
	if out.KeyAlgorithm == "KEY_ALGORITHM_UNSPECIFIED" {
		out.KeyAlgorithm = ""
	}
	if ca := c.CertificateAuthorityConfig; ca != nil && ca.CertificateAuthorityServiceConfig != nil {
		out.CAPool = ca.CertificateAuthorityServiceConfig.CaPool
	}
	return out
}

// fromTrust counts a trust config's certificates; the PEM bodies are not
// copied.
func fromTrust(location string, t *cm.TrustConfig) TrustConfig {
	out := TrustConfig{
		Name:        lastSeg(t.Name),
		Location:    location,
		Description: t.Description,
		Allowlisted: len(t.AllowlistedCertificates),
		Created:     parseTime(t.CreateTime),
		Updated:     parseTime(t.UpdateTime),
		Labels:      t.Labels,
	}
	for _, s := range t.TrustStores {
		out.TrustAnchors += len(s.TrustAnchors)
		out.Intermediates += len(s.IntermediateCas)
	}
	for domain, s := range t.SpiffeTrustStores {
		out.SPIFFEDomains = append(out.SPIFFEDomains, domain)
		out.TrustAnchors += len(s.TrustAnchors)
		out.Intermediates += len(s.IntermediateCas)
	}
	slices.Sort(out.SPIFFEDomains)
	return out
}

func parent(project, location string) string {
	return "projects/" + project + "/locations/" + location
}

func lastSeg(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
