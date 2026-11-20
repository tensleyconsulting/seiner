package kms

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	cloudkms "google.golang.org/api/cloudkms/v1"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
	"github.com/tensleyconsulting/seiner/internal/gcp/regions"
)

type liveAPI struct{ svc *cloudkms.Service }

// NewLazy defers client construction to the first call, so a slow or failing
// constructor cannot block startup; the failure surfaces as this service's
// ProviderError instead.
func NewLazy(opts ...option.ClientOption) Provider {
	l := lazy.New(func(ctx context.Context) (*cloudkms.Service, error) {
		return cloudkms.NewService(ctx, opts...)
	})
	locs := regions.Cached(func(ctx context.Context, project string) ([]string, error) {
		svc, err := l.Get(ctx)
		if err != nil {
			return nil, err
		}
		return liveAPI{svc: svc}.ListLocations(ctx, project)
	})
	return New(lazyAPI{l: l, locs: locs})
}

// lazyAPI builds the client on first use. Its location list is cached for
// the session, since it changes rarely and every key ring list starts with
// it.
type lazyAPI struct {
	l    *lazy.Value[*cloudkms.Service]
	locs regions.Lister
}

func (a lazyAPI) ListLocations(ctx context.Context, project string) ([]string, error) {
	return a.locs.Regions(ctx, project)
}

func (a lazyAPI) ListKeyRings(ctx context.Context, project, location string) ([]KeyRing, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListKeyRings(ctx, project, location)
}

func (a lazyAPI) ListKeys(ctx context.Context, project, location, ring string, limit int) ([]Key, bool, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveAPI{svc: svc}.ListKeys(ctx, project, location, ring, limit)
}

func (a lazyAPI) ListVersions(ctx context.Context, project, location, ring, key string, limit int) ([]KeyVersion, bool, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveAPI{svc: svc}.ListVersions(ctx, project, location, ring, key, limit)
}

func (a lazyAPI) GetAttestation(ctx context.Context, project, location, ring, key, version string) (Attestation, bool, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return Attestation{}, false, err
	}
	return liveAPI{svc: svc}.GetAttestation(ctx, project, location, ring, key, version)
}

func (a lazyAPI) ListImportJobs(ctx context.Context, project, location, ring string, limit int) ([]ImportJob, bool, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveAPI{svc: svc}.ListImportJobs(ctx, project, location, ring, limit)
}

func (a lazyAPI) GetIAMPolicy(ctx context.Context, project, location, ring, key string) ([]Binding, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.GetIAMPolicy(ctx, project, location, ring, key)
}

func (a lazyAPI) GetImportJobIAMPolicy(ctx context.Context, project, location, ring, job string) ([]Binding, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.GetImportJobIAMPolicy(ctx, project, location, ring, job)
}

var (
	_ API = lazyAPI{}
	_ API = liveAPI{}
)

// errStop ends paging once the key limit is reached.
var errStop = errors.New("stop")

func (a liveAPI) ListLocations(ctx context.Context, project string) ([]string, error) {
	var out []string
	err := a.svc.Projects.Locations.List("projects/"+project).Pages(ctx, func(resp *cloudkms.ListLocationsResponse) error {
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

func (a liveAPI) ListKeyRings(ctx context.Context, project, location string) ([]KeyRing, error) {
	var out []KeyRing
	err := a.svc.Projects.Locations.KeyRings.List("projects/"+project+"/locations/"+location).Pages(ctx, func(resp *cloudkms.ListKeyRingsResponse) error {
		for _, r := range resp.KeyRings {
			out = append(out, KeyRing{Name: lastSeg(r.Name), Location: location, Created: parseTime(r.CreateTime)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a liveAPI) ListKeys(ctx context.Context, project, location, ring string, limit int) ([]Key, bool, error) {
	var out []Key
	truncated := false
	parent := "projects/" + project + "/locations/" + location + "/keyRings/" + ring
	err := a.svc.Projects.Locations.KeyRings.CryptoKeys.List(parent).Pages(ctx, func(resp *cloudkms.ListCryptoKeysResponse) error {
		for _, k := range resp.CryptoKeys {
			if len(out) == limit {
				truncated = true
				return errStop
			}
			out = append(out, fromKey(k))
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return nil, false, err
	}
	return out, truncated, nil
}

func ringName(project, location, ring string) string {
	return "projects/" + project + "/locations/" + location + "/keyRings/" + ring
}

func (a liveAPI) ListVersions(ctx context.Context, project, location, ring, key string, limit int) ([]KeyVersion, bool, error) {
	var out []KeyVersion
	truncated := false
	parent := ringName(project, location, ring) + "/cryptoKeys/" + key
	err := a.svc.Projects.Locations.KeyRings.CryptoKeys.CryptoKeyVersions.List(parent).Pages(ctx, func(resp *cloudkms.ListCryptoKeyVersionsResponse) error {
		for _, v := range resp.CryptoKeyVersions {
			if len(out) == limit {
				truncated = true
				return errStop
			}
			out = append(out, fromVersion(v))
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return nil, false, err
	}
	return out, truncated, nil
}

// GetAttestation gets the version itself: a version list carries the
// attestation only in its FULL view, which would fetch it for every row.
func (a liveAPI) GetAttestation(ctx context.Context, project, location, ring, key, version string) (Attestation, bool, error) {
	name := ringName(project, location, ring) + "/cryptoKeys/" + key + "/cryptoKeyVersions/" + version
	v, err := a.svc.Projects.Locations.KeyRings.CryptoKeys.CryptoKeyVersions.Get(name).Context(ctx).Do()
	if err != nil {
		return Attestation{}, false, err
	}
	return fromAttestation(v.Attestation)
}

func (a liveAPI) ListImportJobs(ctx context.Context, project, location, ring string, limit int) ([]ImportJob, bool, error) {
	var out []ImportJob
	truncated := false
	err := a.svc.Projects.Locations.KeyRings.ImportJobs.List(ringName(project, location, ring)).Pages(ctx, func(resp *cloudkms.ListImportJobsResponse) error {
		for _, j := range resp.ImportJobs {
			if len(out) == limit {
				truncated = true
				return errStop
			}
			out = append(out, fromImportJob(j))
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return nil, false, err
	}
	return out, truncated, nil
}

func (a liveAPI) GetIAMPolicy(ctx context.Context, project, location, ring, key string) ([]Binding, error) {
	resource := ringName(project, location, ring)
	var (
		policy *cloudkms.Policy
		err    error
	)
	// Version 3 returns conditional bindings with their conditions.
	if key == "" {
		policy, err = a.svc.Projects.Locations.KeyRings.GetIamPolicy(resource).OptionsRequestedPolicyVersion(3).Context(ctx).Do()
	} else {
		policy, err = a.svc.Projects.Locations.KeyRings.CryptoKeys.GetIamPolicy(resource + "/cryptoKeys/" + key).OptionsRequestedPolicyVersion(3).Context(ctx).Do()
	}
	if err != nil {
		return nil, err
	}
	return fromBindings(policy.Bindings), nil
}

func (a liveAPI) GetImportJobIAMPolicy(ctx context.Context, project, location, ring, job string) ([]Binding, error) {
	name := ringName(project, location, ring) + "/importJobs/" + job
	policy, err := a.svc.Projects.Locations.KeyRings.ImportJobs.GetIamPolicy(name).OptionsRequestedPolicyVersion(3).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	return fromBindings(policy.Bindings), nil
}

func fromBindings(bindings []*cloudkms.Binding) []Binding {
	var out []Binding
	for _, b := range bindings {
		out = append(out, Binding{Role: b.Role, Members: append([]string(nil), b.Members...), Condition: conditionTitle(b.Condition)})
	}
	return out
}

// conditionTitle names a condition by its title, or by its expression when
// it has none.
func conditionTitle(e *cloudkms.Expr) string {
	if e == nil {
		return ""
	}
	if e.Title != "" {
		return e.Title
	}
	return e.Expression
}

func fromVersion(v *cloudkms.CryptoKeyVersion) KeyVersion {
	out := KeyVersion{
		Name:             lastSeg(v.Name),
		State:            v.State,
		Algorithm:        v.Algorithm,
		ProtectionLevel:  v.ProtectionLevel,
		Created:          parseTime(v.CreateTime),
		Generated:        parseTime(v.GenerateTime),
		Imported:         parseTime(v.ImportTime),
		DestroyScheduled: parseTime(v.DestroyTime),
		Destroyed:        parseTime(v.DestroyEventTime),
		ImportJob:        lastSeg(v.ImportJob),
		ReimportEligible: v.ReimportEligible,
	}
	for _, r := range []string{v.GenerationFailureReason, v.ImportFailureReason, v.ExternalDestructionFailureReason} {
		if r != "" {
			out.FailureReason = r
			break
		}
	}
	if o := v.ExternalProtectionLevelOptions; o != nil {
		out.ExternalKeyURI = o.ExternalKeyUri
		if out.ExternalKeyURI == "" {
			out.ExternalKeyURI = o.EkmConnectionKeyPath
		}
	}
	return out
}

func fromAttestation(at *cloudkms.KeyOperationAttestation) (Attestation, bool, error) {
	if at == nil || (at.Content == "" && at.CertChains == nil) {
		return Attestation{}, false, nil
	}
	content, err := decodeBytes(at.Content)
	if err != nil {
		return Attestation{}, false, fmt.Errorf("kms: attestation content: %w", err)
	}
	out := Attestation{Format: at.Format, Content: content}
	if c := at.CertChains; c != nil {
		out.Cavium = append([]string(nil), c.CaviumCerts...)
		out.GoogleCard = append([]string(nil), c.GoogleCardCerts...)
		out.GooglePartition = append([]string(nil), c.GooglePartitionCerts...)
	}
	return out, true, nil
}

// decodeBytes decodes a JSON bytes field. Google APIs send standard base64,
// but accept the URL alphabet too, so both are read.
func decodeBytes(s string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(s)
}

func fromImportJob(j *cloudkms.ImportJob) ImportJob {
	return ImportJob{
		Name:            lastSeg(j.Name),
		State:           j.State,
		Method:          j.ImportMethod,
		ProtectionLevel: j.ProtectionLevel,
		Backend:         lastSeg(j.CryptoKeyBackend),
		Created:         parseTime(j.CreateTime),
		Generated:       parseTime(j.GenerateTime),
		Expires:         parseTime(j.ExpireTime),
		Expired:         parseTime(j.ExpireEventTime),
	}
}

func fromKey(k *cloudkms.CryptoKey) Key {
	out := Key{
		Name:           lastSeg(k.Name),
		Purpose:        k.Purpose,
		RotationPeriod: k.RotationPeriod,
		NextRotation:   parseTime(k.NextRotationTime),
		DestroyDelay:   k.DestroyScheduledDuration,
		ImportOnly:     k.ImportOnly,
		Backend:        lastSeg(k.CryptoKeyBackend),
		Created:        parseTime(k.CreateTime),
		Labels:         k.Labels,
	}
	if t := k.VersionTemplate; t != nil {
		out.Algorithm = t.Algorithm
		out.ProtectionLevel = t.ProtectionLevel
		out.TemplateAlgorithm = t.Algorithm
		out.TemplateProtection = t.ProtectionLevel
	}
	if p := k.KeyAccessJustificationsPolicy; p != nil {
		out.AccessReasons = append([]string(nil), p.AllowedAccessReasons...)
	}
	if p := k.Primary; p != nil {
		out.Primary = lastSeg(p.Name)
		out.PrimaryState = p.State
		if p.Algorithm != "" {
			out.Algorithm = p.Algorithm
		}
		if p.ProtectionLevel != "" {
			out.ProtectionLevel = p.ProtectionLevel
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
