package gcs

import (
	"context"
	"errors"
	"maps"
	"slices"

	iampb "cloud.google.com/go/iam/apiv1/iampb"
	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
)

type liveAPI struct {
	client *storage.Client
}

// NewLazy returns a Provider whose Cloud Storage client is constructed on
// first use rather than at startup, and closed by g. A construction failure
// is returned from that first call instead of blocking Open or silently
// dropping the service.
func NewLazy(g *lazy.Group, opts ...option.ClientOption) Provider {
	l := lazy.NewIn(g, func(ctx context.Context) (*storage.Client, error) {
		return storage.NewClient(ctx, opts...)
	})
	return New(lazyAPI{l: l}, "")
}

type lazyAPI struct {
	l *lazy.Value[*storage.Client]
}

func (a lazyAPI) ListBuckets(ctx context.Context, project string) ([]Bucket, error) {
	c, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{client: c}.ListBuckets(ctx, project)
}

func (a lazyAPI) ListObjects(ctx context.Context, bucket string, limit int) ([]Object, bool, error) {
	c, err := a.l.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveAPI{client: c}.ListObjects(ctx, bucket, limit)
}

func (a lazyAPI) GetBucketPolicy(ctx context.Context, bucket string) ([]PolicyBinding, error) {
	c, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{client: c}.GetBucketPolicy(ctx, bucket)
}

func (a liveAPI) ListBuckets(ctx context.Context, project string) ([]Bucket, error) {
	it := a.client.Buckets(ctx, project)
	var out []Bucket
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		out = append(out, fromAttrs(attrs))
	}
}

func (a liveAPI) ListObjects(ctx context.Context, bucket string, limit int) ([]Object, bool, error) {
	if limit <= 0 {
		limit = maxObjects
	}
	q := &storage.Query{Projection: storage.ProjectionNoACL}
	if err := q.SetAttrSelection([]string{"Name", "Size", "ContentType", "StorageClass", "Generation", "Created", "Updated"}); err != nil {
		return nil, false, err
	}
	it := a.client.Bucket(bucket).Objects(ctx, q)
	// One page holds the limit and the peek past it, instead of the
	// default 1,000 objects.
	it.PageInfo().MaxSize = limit + 1
	var out []Object
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return out, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		out = append(out, Object{
			Name:         attrs.Name,
			Size:         attrs.Size,
			ContentType:  attrs.ContentType,
			StorageClass: attrs.StorageClass,
			Generation:   attrs.Generation,
			Created:      attrs.Created,
			Updated:      attrs.Updated,
		})
		if len(out) >= limit {
			// Peek so a bucket with exactly limit objects is not reported
			// as truncated. A failed peek keeps the page and assumes more.
			_, err := it.Next()
			return out, !errors.Is(err, iterator.Done), nil
		}
	}
}

func (a liveAPI) GetBucketPolicy(ctx context.Context, bucket string) ([]PolicyBinding, error) {
	// V3 reads conditional bindings; the v1 handle fails on a bucket that
	// has any.
	policy, err := a.client.Bucket(bucket).IAM().V3().Policy(ctx)
	if err != nil {
		return nil, err
	}
	return fromBindings(policy.Bindings), nil
}

func fromBindings(bindings []*iampb.Binding) []PolicyBinding {
	var out []PolicyBinding
	for _, b := range bindings {
		out = append(out, PolicyBinding{
			Role:      b.GetRole(),
			Members:   append([]string(nil), b.GetMembers()...),
			Condition: b.GetCondition().GetTitle(),
		})
	}
	return out
}

func fromAttrs(a *storage.BucketAttrs) Bucket {
	if a == nil {
		return Bucket{}
	}
	b := Bucket{
		Name:           a.Name,
		Location:       a.Location,
		LocationType:   a.LocationType,
		StorageClass:   a.StorageClass,
		Versioning:     a.VersioningEnabled,
		UniformAccess:  a.UniformBucketLevelAccess.Enabled,
		LifecycleRules: len(a.Lifecycle.Rules),
		RequesterPays:  a.RequesterPays,
		Created:        a.Created,
		Updated:        a.Updated,
		Labels:         maps.Clone(a.Labels),
	}
	switch a.PublicAccessPrevention {
	case storage.PublicAccessPreventionEnforced:
		b.PublicAccess = "enforced"
	case storage.PublicAccessPreventionInherited:
		b.PublicAccess = "inherited"
	}
	if r := a.RetentionPolicy; r != nil {
		b.Retention = r.RetentionPeriod
		b.RetentionLock = r.IsLocked
	}
	if sd := a.SoftDeletePolicy; sd != nil {
		b.SoftDelete = sd.RetentionDuration
	}
	if ac := a.Autoclass; ac != nil {
		b.Autoclass = ac.Enabled
	}
	if e := a.Encryption; e != nil {
		b.KMSKey = e.DefaultKMSKeyName
	}
	b.TurboReplication = a.RPO == storage.RPOAsyncTurbo
	if c := a.CustomPlacementConfig; c != nil {
		b.DataLocations = slices.Clone(c.DataLocations)
	}
	if l := a.Logging; l != nil {
		b.LogBucket = l.LogBucket
	}
	b.HNS = a.HierarchicalNamespace != nil && a.HierarchicalNamespace.Enabled
	b.ObjectRetention = a.ObjectRetentionMode
	b.EventBasedHold = a.DefaultEventBasedHold
	return b
}

var _ API = lazyAPI{}
