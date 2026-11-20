package logging

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/tensleyconsulting/seiner/internal/gcp/display"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// Sinks implements model.ResourceProvider for log sinks on organizations,
// folders, and projects.
type Sinks struct{ api API }

func NewSinks(api API) Sinks { return Sinks{api: api} }

func (p Sinks) Service() model.ServiceType { return model.ServiceLogSink }

// ListPermissions is what List needs: sinks list at every CRM scope.
func (p Sinks) ListPermissions(scopeID string) []string {
	if model.ScopeKind(scopeID) == "" {
		return nil
	}
	return []string{"logging.sinks.list"}
}

func (p Sinks) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if model.ScopeKind(scopeID) == "" {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("logging: no API")
	}
	sinks, err := p.api.ListSinks(ctx, scopeID)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(sinks))
	for _, s := range sinks {
		out = append(out, sinkResource(scopeID, s))
	}
	return out, nil
}

// Buckets implements model.ResourceProvider for a project's log buckets.
// Organizations and folders cannot hold user-defined buckets, so only
// projects list them.
type Buckets struct{ api API }

func NewBuckets(api API) Buckets { return Buckets{api: api} }

func (p Buckets) Service() model.ServiceType { return model.ServiceLogBucket }

// ListPermissions is what List needs.
func (p Buckets) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "logging.buckets.list")
}

func (p Buckets) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("logging: no API")
	}
	buckets, err := p.api.ListBuckets(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, bucketResource(project, b))
	}
	return out, nil
}

func sinkResource(scopeID string, s Sink) model.Resource {
	md := map[string]string{}
	set(md, "destinationType", destinationType(s.Destination))
	set(md, "destination", s.Destination)
	filter := s.Filter
	if filter == "" {
		filter = "(all logs)"
	}
	md["filter"] = filter
	if len(s.Exclusions) > 0 {
		md["exclusions"] = strconv.Itoa(len(s.Exclusions))
	}
	if model.ScopeKind(scopeID) != model.KindProject {
		md["includeChildren"] = onOff(s.IncludeChildren)
		if s.InterceptChildren {
			md["interceptChildren"] = "on"
		}
	}
	set(md, "writerIdentity", s.WriterIdentity)
	if s.PartitionedTables {
		md["partitionedTables"] = "on"
	}
	set(md, "updated", display.Stamp(s.Updated))
	r := model.Resource{
		ID:          scopeID + "/sinks/" + url.PathEscape(s.Name),
		Name:        s.Name,
		Service:     model.ServiceLogSink,
		ScopeID:     scopeID,
		Location:    "global",
		Description: s.Description,
		Created:     s.Created,
		Metadata:    md,
		Fields: model.FieldsOf(md, "destinationType", "destination", "filter", "exclusions", "includeChildren",
			"interceptChildren", "writerIdentity", "partitionedTables", "updated"),
	}
	if s.Disabled {
		r.Status = model.StatusWarning
		r.State = "DISABLED"
		r.Badge = "off"
	}
	if len(s.Exclusions) > 0 {
		items := make([]string, 0, len(s.Exclusions))
		for _, e := range s.Exclusions {
			line := e.Name + "  " + e.Filter
			if e.Disabled {
				line += "  (disabled)"
			}
			items = append(items, line)
		}
		r.Lists = []model.List{{ID: "exclusions", Label: "Exclusions", Items: items}}
	}
	return r
}

func bucketResource(project string, b Bucket) model.Resource {
	md := map[string]string{}
	if b.RetentionDays > 0 {
		md["retention"] = strconv.FormatInt(b.RetentionDays, 10) + " days"
	}
	md["locked"] = onOff(b.Locked)
	md["analytics"] = onOff(b.Analytics)
	set(md, "kmsKey", b.KMSKey)
	set(md, "kmsKeyVersion", b.KMSKeyVersion)
	set(md, "updated", display.Stamp(b.Updated))
	var lists []model.List
	if len(b.Indexes) > 0 {
		md["indexedFields"] = strconv.Itoa(len(b.Indexes))
		lists = append(lists, model.List{ID: "indexedFields", Label: "Indexed fields", Items: slices.Clone(b.Indexes)})
	}
	if len(b.Restricted) > 0 {
		md["restrictedFields"] = strconv.Itoa(len(b.Restricted))
		lists = append(lists, model.List{ID: "restrictedFields", Label: "Restricted fields", Items: slices.Clone(b.Restricted)})
	}
	// Only a bucket with Log Analytics on can link a BigQuery dataset.
	facets := []model.Facet{{ID: model.FacetViews, Label: "Views"}}
	if b.Analytics {
		facets = append(facets, model.Facet{ID: model.FacetLinks, Label: "Linked Datasets"})
	}
	return model.Resource{
		ID:          bucketID(project, b.Location, b.Name),
		Name:        b.Name,
		Service:     model.ServiceLogBucket,
		ScopeID:     "projects/" + project,
		Location:    b.Location,
		Status:      bucketStatus(b.State),
		State:       b.State,
		Description: b.Description,
		Created:     b.Created,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "retention", "locked", "analytics", "kmsKey", "kmsKeyVersion", "indexedFields", "restrictedFields", "updated"),
		Lists:       lists,
		Facets:      facets,
	}
}

func bucketID(project, location, bucket string) string {
	return "projects/" + project + "/locations/" + url.PathEscape(location) + "/buckets/" + url.PathEscape(bucket)
}

// ListRelated lists a bucket's log views and linked datasets, and a log
// view's IAM bindings.
func (p Buckets) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	switch {
	case parent.Service == model.ServiceLogBucket && (facetID == model.FacetViews || facetID == model.FacetLinks):
	case parent.Service == model.ServiceLogView && facetID == model.FacetPermissions:
	default:
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Location == "" || n.Kind != "buckets" {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("logging: no API")
	}
	if parent.Service == model.ServiceLogView {
		if len(n.Rest) != 2 || n.Rest[0] != "views" || n.Rest[1] == "" {
			return nil, false, nil
		}
		bindings, err := p.api.GetViewIAMPolicy(ctx, n.Project, n.Location, n.Name, n.Rest[1])
		if err != nil {
			return nil, false, err
		}
		out := make([]model.Resource, 0, len(bindings))
		for _, b := range bindings {
			r := model.BindingResource(parent.ID, parent.ScopeID, b.Role, b.Members, b.Condition)
			r.ParentID = parent.ID
			r.FacetID = model.FacetPermissions
			out = append(out, r)
		}
		return out, false, nil
	}
	if len(n.Rest) != 0 {
		return nil, false, nil
	}
	var out []model.Resource
	if facetID == model.FacetViews {
		views, err := p.api.ListViews(ctx, n.Project, n.Location, n.Name)
		if err != nil {
			return nil, false, err
		}
		for _, v := range views {
			out = append(out, viewResource(parent, v))
		}
		return out, false, nil
	}
	links, err := p.api.ListLinks(ctx, n.Project, n.Location, n.Name)
	if err != nil {
		return nil, false, err
	}
	for _, l := range links {
		out = append(out, linkResource(parent, n.Project, l))
	}
	return out, false, nil
}

func viewResource(bucket model.Resource, v View) model.Resource {
	md := map[string]string{}
	filter := v.Filter
	if filter == "" {
		filter = "(all logs)"
	}
	md["filter"] = filter
	set(md, "updated", display.Stamp(v.Updated))
	return model.Resource{
		ID:          bucket.ID + "/views/" + url.PathEscape(v.Name),
		Name:        v.Name,
		Service:     model.ServiceLogView,
		ScopeID:     bucket.ScopeID,
		Location:    bucket.Location,
		Description: v.Description,
		Created:     v.Created,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "filter", "updated"),
		Facets:      []model.Facet{{ID: model.FacetPermissions, Label: "Permissions", Optional: true}},
		ParentID:    bucket.ID,
		FacetID:     model.FacetViews,
	}
}

func linkResource(bucket model.Resource, project string, l Link) model.Resource {
	md := map[string]string{}
	if l.Dataset != "" {
		md["dataset"] = project + "." + l.Dataset
	}
	return model.Resource{
		ID:          bucket.ID + "/links/" + url.PathEscape(l.Name),
		Name:        l.Name,
		Service:     model.ServiceLogLink,
		ScopeID:     bucket.ScopeID,
		Location:    bucket.Location,
		Status:      bucketStatus(l.State),
		State:       l.State,
		Description: l.Description,
		Created:     l.Created,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "dataset"),
		ParentID:    bucket.ID,
		FacetID:     model.FacetLinks,
	}
}

// Metrics implements model.ResourceProvider for a project's log-based
// metrics.
type Metrics struct{ api API }

func NewMetrics(api API) Metrics { return Metrics{api: api} }

func (p Metrics) Service() model.ServiceType { return model.ServiceLogMetric }

// ListPermissions is what List needs.
func (p Metrics) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "logging.logMetrics.list")
}

func (p Metrics) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("logging: no API")
	}
	metrics, err := p.api.ListMetrics(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(metrics))
	for _, m := range metrics {
		out = append(out, metricResource(project, m))
	}
	return out, nil
}

// metricResource is a log-based metric. A disabled metric counts nothing,
// so it is a warning badged "off", as a disabled sink is.
func metricResource(project string, m Metric) model.Resource {
	md := map[string]string{}
	md["filter"] = m.Filter
	if m.Kind != "" || m.ValueType != "" {
		md["type"] = strings.TrimSpace(m.Kind + " " + m.ValueType)
	}
	set(md, "unit", m.Unit)
	set(md, "valueExtractor", m.ValueExtractor)
	set(md, "bucket", m.Bucket)
	set(md, "updated", display.Stamp(m.Updated))
	r := model.Resource{
		ID:          "projects/" + project + "/metrics/" + url.PathEscape(m.Name),
		Name:        m.Name,
		Service:     model.ServiceLogMetric,
		ScopeID:     "projects/" + project,
		Location:    "global",
		Description: m.Description,
		Created:     m.Created,
		Metadata:    md,
	}
	if len(m.Labels) > 0 {
		md["labels"] = strconv.Itoa(len(m.Labels))
		r.Lists = []model.List{{ID: "labels", Label: "Label extractors", Items: slices.Clone(m.Labels)}}
	}
	r.Fields = model.FieldsOf(md, "filter", "type", "unit", "valueExtractor", "labels", "bucket", "updated")
	if m.Disabled {
		r.Status, r.State, r.Badge = model.StatusWarning, "DISABLED", "off"
	}
	return r
}

// destinationType names the product a sink routes to, from its destination's
// service host.
func destinationType(dest string) string {
	host, rest, _ := strings.Cut(dest, "/")
	switch host {
	case "storage.googleapis.com":
		return "Cloud Storage"
	case "bigquery.googleapis.com":
		return "BigQuery"
	case "pubsub.googleapis.com":
		return "Pub/Sub"
	case "logging.googleapis.com":
		if strings.Contains(rest, "/buckets/") {
			return "Log bucket"
		}
		return "Logging project"
	case "":
		return ""
	default:
		return host
	}
}

func bucketStatus(s string) model.ResourceStatus {
	switch s {
	case "ACTIVE":
		return model.StatusHealthy
	case "DELETE_REQUESTED", "UPDATING", "CREATING":
		return model.StatusWarning
	case "FAILED":
		return model.StatusError
	default:
		return model.StatusUnknown
	}
}

// set stores v under k when v is not empty.
func set(md map[string]string, k, v string) {
	if v != "" {
		md[k] = v
	}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

var (
	_ model.ResourceProvider = Sinks{}
	_ model.ResourceProvider = Buckets{}
)
