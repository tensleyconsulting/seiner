package logging

import (
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	lg "google.golang.org/api/logging/v2"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
)

type liveAPI struct{ svc *lg.Service }

// NewLazy returns an API whose Logging client is constructed on first use
// rather than at startup, so a slow or failing constructor cannot block
// startup; the failure surfaces as each provider's ProviderError instead.
// Build it once and pass it to NewSinks and NewBuckets.
func NewLazy(opts ...option.ClientOption) API {
	l := lazy.New(func(ctx context.Context) (*lg.Service, error) {
		return lg.NewService(ctx, opts...)
	})
	return lazyAPI{l: l}
}

type lazyAPI struct{ l *lazy.Value[*lg.Service] }

func (a lazyAPI) ListSinks(ctx context.Context, parent string) ([]Sink, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListSinks(ctx, parent)
}

func (a lazyAPI) ListBuckets(ctx context.Context, project string) ([]Bucket, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListBuckets(ctx, project)
}

func (a lazyAPI) ListMetrics(ctx context.Context, project string) ([]Metric, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListMetrics(ctx, project)
}

func (a lazyAPI) ListViews(ctx context.Context, project, location, bucket string) ([]View, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListViews(ctx, project, location, bucket)
}

func (a lazyAPI) ListLinks(ctx context.Context, project, location, bucket string) ([]Link, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListLinks(ctx, project, location, bucket)
}

var (
	_ API = lazyAPI{}
	_ API = liveAPI{}
)

// ListSinks uses the parent-agnostic sinks collection, which accepts
// organizations/o, folders/f, and projects/p alike.
func (a liveAPI) ListSinks(ctx context.Context, parent string) ([]Sink, error) {
	var out []Sink
	err := a.svc.Sinks.List(parent).Pages(ctx, func(resp *lg.ListSinksResponse) error {
		for _, s := range resp.Sinks {
			out = append(out, fromSink(s))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListBuckets uses the "-" location wildcard, which the API documents as
// returning buckets in every location.
func (a liveAPI) ListBuckets(ctx context.Context, project string) ([]Bucket, error) {
	var out []Bucket
	err := a.svc.Projects.Locations.Buckets.List("projects/"+project+"/locations/-").Pages(ctx, func(resp *lg.ListBucketsResponse) error {
		for _, b := range resp.Buckets {
			out = append(out, fromBucket(b))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func fromSink(s *lg.LogSink) Sink {
	out := Sink{
		Name:              s.Name,
		Description:       s.Description,
		Destination:       s.Destination,
		Filter:            s.Filter,
		Disabled:          s.Disabled,
		IncludeChildren:   s.IncludeChildren,
		InterceptChildren: s.InterceptChildren,
		WriterIdentity:    s.WriterIdentity,
		PartitionedTables: s.BigqueryOptions != nil && s.BigqueryOptions.UsePartitionedTables,
		Created:           parseTime(s.CreateTime),
		Updated:           parseTime(s.UpdateTime),
	}
	for _, e := range s.Exclusions {
		out.Exclusions = append(out.Exclusions, Exclusion{Name: e.Name, Filter: e.Filter, Disabled: e.Disabled})
	}
	return out
}

// fromBucket maps projects/p/locations/l/buckets/b.
func fromBucket(b *lg.LogBucket) Bucket {
	out := Bucket{
		Name:          lastSeg(b.Name),
		Location:      segAfter(b.Name, "locations"),
		Description:   b.Description,
		RetentionDays: b.RetentionDays,
		Locked:        b.Locked,
		State:         b.LifecycleState,
		Analytics:     b.AnalyticsEnabled,
		Restricted:    slices.Clone(b.RestrictedFields),
		Created:       parseTime(b.CreateTime),
		Updated:       parseTime(b.UpdateTime),
	}
	if c := b.CmekSettings; c != nil {
		out.KMSKey = c.KmsKeyName
		out.KMSKeyVersion = c.KmsKeyVersionName
	}
	for _, ix := range b.IndexConfigs {
		out.Indexes = append(out.Indexes, ix.FieldPath+" "+ix.Type)
	}
	return out
}

func (a liveAPI) ListMetrics(ctx context.Context, project string) ([]Metric, error) {
	var out []Metric
	err := a.svc.Projects.Metrics.List("projects/"+project).Pages(ctx, func(resp *lg.ListLogMetricsResponse) error {
		for _, m := range resp.Metrics {
			out = append(out, fromMetric(m))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func bucketName(project, location, bucket string) string {
	return "projects/" + project + "/locations/" + location + "/buckets/" + bucket
}

func (a liveAPI) ListViews(ctx context.Context, project, location, bucket string) ([]View, error) {
	var out []View
	err := a.svc.Projects.Locations.Buckets.Views.List(bucketName(project, location, bucket)).Pages(ctx, func(resp *lg.ListViewsResponse) error {
		for _, v := range resp.Views {
			out = append(out, View{Name: lastSeg(v.Name), Description: v.Description, Filter: v.Filter,
				Created: parseTime(v.CreateTime), Updated: parseTime(v.UpdateTime)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a liveAPI) ListLinks(ctx context.Context, project, location, bucket string) ([]Link, error) {
	var out []Link
	err := a.svc.Projects.Locations.Buckets.Links.List(bucketName(project, location, bucket)).Pages(ctx, func(resp *lg.ListLinksResponse) error {
		for _, l := range resp.Links {
			out = append(out, fromLink(l))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func fromMetric(m *lg.LogMetric) Metric {
	out := Metric{
		Name:           m.Name,
		Description:    m.Description,
		Filter:         m.Filter,
		Disabled:       m.Disabled,
		Bucket:         m.BucketName,
		ValueExtractor: m.ValueExtractor,
		Created:        parseTime(m.CreateTime),
		Updated:        parseTime(m.UpdateTime),
	}
	if d := m.MetricDescriptor; d != nil {
		out.Kind = d.MetricKind
		out.ValueType = d.ValueType
		out.Unit = d.Unit
	}
	for _, k := range slices.Sorted(maps.Keys(m.LabelExtractors)) {
		out.Labels = append(out.Labels, k+"  "+m.LabelExtractors[k])
	}
	return out
}

func fromLink(l *lg.Link) Link {
	out := Link{Name: lastSeg(l.Name), Description: l.Description, State: l.LifecycleState, Created: parseTime(l.CreateTime)}
	if d := l.BigqueryDataset; d != nil {
		out.Dataset = d.DatasetId
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

// segAfter returns the path segment following key in name, or "".
func segAfter(name, key string) string {
	segs := strings.Split(name, "/")
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] == key {
			return segs[i+1]
		}
	}
	return ""
}
