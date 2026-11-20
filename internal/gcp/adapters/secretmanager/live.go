package secretmanager

import (
	"context"
	"errors"
	"strings"
	"time"

	"google.golang.org/api/option"
	sm "google.golang.org/api/secretmanager/v1"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
)

type liveAPI struct{ svc *sm.Service }

// NewLazy defers client construction to the first call, so a slow or failing
// constructor cannot block startup; the failure surfaces as this service's
// ProviderError instead.
func NewLazy(opts ...option.ClientOption) Provider {
	l := lazy.New(func(ctx context.Context) (*sm.Service, error) {
		return sm.NewService(ctx, opts...)
	})
	return New(lazyAPI{l: l})
}

type lazyAPI struct{ l *lazy.Value[*sm.Service] }

func (a lazyAPI) ListSecrets(ctx context.Context, project string) ([]Secret, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListSecrets(ctx, project)
}

func (a lazyAPI) ListVersions(ctx context.Context, project, secret string, limit int) ([]Version, bool, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveAPI{svc: svc}.ListVersions(ctx, project, secret, limit)
}

func (a lazyAPI) GetIAMPolicy(ctx context.Context, project, secret string) ([]Binding, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.GetIAMPolicy(ctx, project, secret)
}

var (
	_ API = lazyAPI{}
	_ API = liveAPI{}
)

// errStop ends paging once the version limit is reached.
var errStop = errors.New("stop")

// ListSecrets lists the project's global secrets. Regional secrets
// (projects/p/locations/l/secrets) are a separate collection and not
// listed.
func (a liveAPI) ListSecrets(ctx context.Context, project string) ([]Secret, error) {
	var out []Secret
	err := a.svc.Projects.Secrets.List("projects/"+project).Pages(ctx, func(resp *sm.ListSecretsResponse) error {
		for _, s := range resp.Secrets {
			out = append(out, fromSecret(s))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a liveAPI) ListVersions(ctx context.Context, project, secret string, limit int) ([]Version, bool, error) {
	var out []Version
	truncated := false
	call := a.svc.Projects.Secrets.Versions.List("projects/" + project + "/secrets/" + secret).PageSize(int64(min(limit, 100)))
	err := call.Pages(ctx, func(resp *sm.ListSecretVersionsResponse) error {
		for _, v := range resp.Versions {
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

func (a liveAPI) GetIAMPolicy(ctx context.Context, project, secret string) ([]Binding, error) {
	name := "projects/" + project + "/secrets/" + secret
	policy, err := a.svc.Projects.Secrets.GetIamPolicy(name).OptionsRequestedPolicyVersion(3).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	var out []Binding
	for _, b := range policy.Bindings {
		out = append(out, Binding{Role: b.Role, Members: append([]string(nil), b.Members...), Condition: conditionTitle(b.Condition)})
	}
	return out, nil
}

func conditionTitle(e *sm.Expr) string {
	if e == nil {
		return ""
	}
	if e.Title != "" {
		return e.Title
	}
	return e.Expression
}

func fromSecret(s *sm.Secret) Secret {
	out := Secret{
		Name:        lastSeg(s.Name),
		Expires:     parseTime(s.ExpireTime),
		Aliases:     s.VersionAliases,
		DestroyTTL:  s.VersionDestroyTtl,
		Created:     parseTime(s.CreateTime),
		Labels:      s.Labels,
		Type:        s.SecretType,
		Annotations: s.Annotations,
	}
	if c := s.CustomerManagedEncryption; c != nil {
		out.KMSKey = c.KmsKeyName
	}
	if r := s.Replication; r != nil {
		switch {
		case r.Automatic != nil:
			out.Replication = "automatic"
			if c := r.Automatic.CustomerManagedEncryption; c != nil && out.KMSKey == "" {
				out.KMSKey = c.KmsKeyName
			}
		case r.UserManaged != nil:
			out.Replication = "user-managed"
			for _, rep := range r.UserManaged.Replicas {
				out.Locations = append(out.Locations, rep.Location)
				if c := rep.CustomerManagedEncryption; c != nil && out.KMSKey == "" {
					out.KMSKey = c.KmsKeyName
				}
			}
		}
	}
	if r := s.Rotation; r != nil {
		out.RotationPeriod = r.RotationPeriod
		out.NextRotation = parseTime(r.NextRotationTime)
	}
	for _, t := range s.Topics {
		out.Topics = append(out.Topics, lastSeg(t.Name))
	}
	return out
}

func fromVersion(v *sm.SecretVersion) Version {
	out := Version{
		Name:             lastSeg(v.Name),
		State:            v.State,
		Created:          parseTime(v.CreateTime),
		Destroyed:        parseTime(v.DestroyTime),
		ScheduledDestroy: parseTime(v.ScheduledDestroyTime),
	}
	add := func(c *sm.CustomerManagedEncryptionStatus) {
		if c != nil && c.KmsKeyVersionName != "" {
			out.KMSKeyVersions = append(out.KMSKeyVersions, c.KmsKeyVersionName)
		}
	}
	add(v.CustomerManagedEncryption)
	if r := v.ReplicationStatus; r != nil {
		if r.Automatic != nil {
			add(r.Automatic.CustomerManagedEncryption)
		}
		if r.UserManaged != nil {
			for _, rep := range r.UserManaged.Replicas {
				add(rep.CustomerManagedEncryption)
			}
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
