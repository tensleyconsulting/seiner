package gcs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// Bucket is the Google-free slice of a Cloud Storage bucket the adapter needs.
type Bucket struct {
	Name           string
	Location       string
	LocationType   string
	StorageClass   string
	Versioning     bool
	UniformAccess  bool
	PublicAccess   string // public access prevention: enforced, inherited
	Retention      time.Duration
	RetentionLock  bool
	SoftDelete     time.Duration
	LifecycleRules int
	Autoclass      bool
	RequesterPays  bool
	KMSKey         string
	Created        time.Time
	Updated        time.Time
	Labels         map[string]string

	TurboReplication bool     // RPO ASYNC_TURBO on a dual-region bucket
	DataLocations    []string // a configurable dual-region's two regions
	LogBucket        string   // where usage and storage logs go
	HNS              bool     // hierarchical namespace
	ObjectRetention  string   // object retention mode: Enabled
	EventBasedHold   bool     // new objects get an event-based hold
}

// Object is one object inside a bucket.
type Object struct {
	Name         string
	Size         int64
	ContentType  string
	StorageClass string
	Generation   int64
	Created      time.Time
	Updated      time.Time
}

// PolicyBinding is a bucket IAM role binding.
type PolicyBinding struct {
	Role      string
	Members   []string
	Condition string // condition title; "" when unconditional
}

// API is the GCS ↔ Resource boundary. Tests inject a fake; live uses the
// Cloud Storage client.
type API interface {
	ListBuckets(ctx context.Context, project string) ([]Bucket, error)
	ListObjects(ctx context.Context, bucket string, limit int) (objects []Object, truncated bool, err error)
	GetBucketPolicy(ctx context.Context, bucket string) ([]PolicyBinding, error)
}

// Provider implements model.ResourceProvider for Cloud Storage.
type Provider struct {
	api     API
	project string
}

// New returns a Provider over api. A non-empty project is the default for
// empty scopes and bare project ids; the live constructors pass "".
func New(api API, project string) Provider {
	return Provider{api: api, project: project}
}

func (p Provider) Service() model.ServiceType { return model.ServiceCloudStorage }

// ListPermissions is what List needs.
func (p Provider) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "storage.buckets.list")
}

func (p Provider) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		if p.project != "" && (scopeID == "" || scopeID == p.project) {
			project = p.project
			ok = true
		}
	}
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("gcs: no API")
	}
	buckets, err := p.api.ListBuckets(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, ResourceFrom(project, b))
	}
	return out, nil
}

// ResourceFrom maps a bucket onto the UI model.
func ResourceFrom(project string, b Bucket) model.Resource {
	md := map[string]string{}
	set(md, "storageClass", b.StorageClass)
	set(md, "locationType", b.LocationType)
	md["versioning"] = enabled(b.Versioning)
	if b.UniformAccess {
		md["access"] = "uniform"
	} else {
		md["access"] = "fine-grained"
	}
	set(md, "publicAccessPrevention", b.PublicAccess)
	if b.Retention > 0 {
		r := duration(b.Retention)
		if b.RetentionLock {
			r += " (locked)"
		}
		md["retention"] = r
	}
	if b.SoftDelete > 0 {
		md["softDelete"] = duration(b.SoftDelete)
	}
	if b.LifecycleRules > 0 {
		md["lifecycleRules"] = strconv.Itoa(b.LifecycleRules)
	}
	if b.Autoclass {
		md["autoclass"] = "enabled"
	}
	if b.RequesterPays {
		// Reading objects would bill a project of the caller's choosing,
		// so a requester-pays bucket's objects are not listed.
		md["requesterPays"] = "enabled; objects not listed"
	}
	set(md, "kmsKey", b.KMSKey)
	set(md, "dataLocations", strings.Join(b.DataLocations, ", "))
	if b.TurboReplication {
		md["turboReplication"] = "enabled"
	}
	if b.HNS {
		md["hierarchicalNamespace"] = "enabled"
	}
	if b.ObjectRetention == "Enabled" {
		md["objectRetention"] = "enabled"
	}
	if b.EventBasedHold {
		md["defaultEventBasedHold"] = "enabled"
	}
	set(md, "accessLogs", b.LogBucket)
	if !b.Updated.IsZero() {
		md["updated"] = b.Updated.UTC().Format("2006-01-02 15:04 UTC")
	}
	facets := []model.Facet{{ID: model.FacetPermissions, Label: "Permissions"}}
	if !b.RequesterPays {
		facets = append(facets, model.Facet{ID: model.FacetObjects, Label: "Objects", Nested: true})
	}
	return model.Resource{
		ID:       "projects/" + project + "/buckets/" + b.Name,
		Name:     b.Name,
		Service:  model.ServiceCloudStorage,
		ScopeID:  "projects/" + project,
		Location: b.Location,
		Metadata: md,
		Fields: model.FieldsOf(md, "storageClass", "locationType", "dataLocations", "turboReplication", "access",
			"publicAccessPrevention", "versioning", "softDelete", "retention", "objectRetention", "defaultEventBasedHold",
			"lifecycleRules", "autoclass", "hierarchicalNamespace", "requesterPays", "kmsKey", "accessLogs", "updated"),
		Created: b.Created,
		Labels:  maps.Clone(b.Labels),
		Facets:  facets,
	}
}

// set stores v under k when v is not empty.
func set(md map[string]string, k, v string) {
	if v != "" {
		md[k] = v
	}
}

func enabled(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

// duration renders d in whole days when it is a multiple of a day.
func duration(d time.Duration) string {
	const day = 24 * time.Hour
	if d%day == 0 {
		n := int64(d / day)
		if n == 1 {
			return "1 day"
		}
		return strconv.FormatInt(n, 10) + " days"
	}
	return d.String()
}

// humanSize renders n bytes with binary units.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

const maxObjects = 200

func (p Provider) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if p.api == nil {
		return nil, false, errors.New("gcs: no API")
	}
	project, bucket := parseBucketID(parent.ID, p.project)
	if bucket == "" {
		return nil, false, nil
	}
	if project == "" {
		project, _ = model.ProjectID(parent.ScopeID)
	}
	switch facetID {
	case model.FacetObjects:
		objs, truncated, err := p.api.ListObjects(ctx, bucket, maxObjects)
		if err != nil {
			return nil, false, err
		}
		out := make([]model.Resource, 0, len(objs))
		for _, o := range objs {
			out = append(out, objectResource(project, bucket, parent.ID, o))
		}
		return out, truncated, nil
	case model.FacetPermissions:
		bindings, err := p.api.GetBucketPolicy(ctx, bucket)
		if err != nil {
			return nil, false, err
		}
		out := make([]model.Resource, 0, len(bindings))
		for _, b := range bindings {
			out = append(out, bindingResource(project, bucket, parent.ID, b))
		}
		return out, false, nil
	default:
		return nil, false, nil
	}
}

func objectResource(project, bucket, parentID string, o Object) model.Resource {
	md := map[string]string{}
	if o.ContentType != "" {
		md["contentType"] = o.ContentType
	}
	if o.Size > 0 {
		md["size"] = humanSize(o.Size)
	}
	set(md, "storageClass", o.StorageClass)
	if o.Generation != 0 {
		md["generation"] = strconv.FormatInt(o.Generation, 10)
	}
	if !o.Updated.IsZero() {
		md["updated"] = o.Updated.UTC().Format("2006-01-02 15:04 UTC")
	}
	return model.Resource{
		ID:       "projects/" + project + "/buckets/" + bucket + "/objects/" + url.PathEscape(o.Name),
		Name:     o.Name,
		Service:  model.ServiceCloudStorage,
		ScopeID:  "projects/" + project,
		Location: "global",
		Metadata: md,
		Fields:   model.FieldsOf(md, "size", "contentType", "storageClass", "updated", "generation"),
		Created:  o.Created,
		ParentID: parentID,
		FacetID:  model.FacetObjects,
	}
}

func bindingResource(project, bucket, parentID string, b PolicyBinding) model.Resource {
	r := model.BindingResource("projects/"+project+"/buckets/"+bucket, "projects/"+project, b.Role, b.Members, b.Condition)
	r.ParentID = parentID
	r.FacetID = model.FacetPermissions
	return r
}

// parseBucketID reads projects/p/buckets/b, or a bare bucket name in
// fallbackProject. Object and binding ids under a bucket are not buckets.
func parseBucketID(id, fallbackProject string) (project, name string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return fallbackProject, ""
	}
	if !strings.Contains(id, "/") {
		return fallbackProject, id
	}
	n, ok := model.ParseName(id)
	if !ok || n.Kind != "buckets" || len(n.Rest) > 0 {
		return fallbackProject, ""
	}
	return n.Project, n.Name
}

var _ model.ResourceProvider = Provider{}
var _ model.RelatedLister = Provider{}
