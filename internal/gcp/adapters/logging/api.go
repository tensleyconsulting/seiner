package logging

import (
	"context"
	"time"
)

// Sink is the Google-free slice of a Cloud Logging sink the adapter needs.
type Sink struct {
	Name              string
	Description       string
	Destination       string // storage.googleapis.com/b, bigquery.googleapis.com/projects/p/datasets/d, ...
	Filter            string
	Disabled          bool
	IncludeChildren   bool // organization and folder sinks: also route child resources' logs
	InterceptChildren bool
	WriterIdentity    string
	PartitionedTables bool // BigQuery destinations: write to partitioned tables
	Exclusions        []Exclusion
	Created           time.Time
	Updated           time.Time
}

// Exclusion is one filter a sink drops logs by.
type Exclusion struct {
	Name     string
	Filter   string
	Disabled bool
}

// Bucket is the Google-free slice of a project's log bucket.
type Bucket struct {
	Name          string // short name, _Default, _Required, or user-defined
	Location      string
	Description   string
	RetentionDays int64
	Locked        bool
	State         string // ACTIVE, DELETE_REQUESTED, UPDATING, ...
	Analytics     bool
	KMSKey        string
	KMSKeyVersion string   // the key version in use, when the API reports it
	Indexes       []string // indexed fields: "jsonPayload.user INDEX_TYPE_STRING"
	Restricted    []string // fields restricted by field-level access
	Created       time.Time
	Updated       time.Time
}

// Metric is a log-based metric.
type Metric struct {
	Name           string
	Description    string
	Filter         string
	Disabled       bool
	Bucket         string // the log bucket a bucket-scoped metric counts from
	Kind           string // DELTA, GAUGE, CUMULATIVE
	ValueType      string // INT64, DISTRIBUTION, ...
	Unit           string
	ValueExtractor string
	Labels         []string // "key  extractor", sorted by key
	Created        time.Time
	Updated        time.Time
}

// View is a log view on a bucket.
type View struct {
	Name        string
	Description string
	Filter      string
	Created     time.Time
	Updated     time.Time
}

// Link is a BigQuery dataset linked to an analytics bucket.
type Link struct {
	Name        string
	Description string
	Dataset     string // the BigQuery dataset id
	State       string // ACTIVE, CREATING, DELETE_REQUESTED, ...
	Created     time.Time
}

// API is the Cloud Logging ↔ Resource boundary. Tests inject a fake; live
// wraps the Logging REST client. One API serves both Sinks and Buckets.
type API interface {
	// ListSinks lists the sinks set on parent (organizations/o, folders/f,
	// or projects/p).
	ListSinks(ctx context.Context, parent string) ([]Sink, error)
	// ListBuckets lists a project's log buckets in every location.
	ListBuckets(ctx context.Context, project string) ([]Bucket, error)
	ListMetrics(ctx context.Context, project string) ([]Metric, error)
	// ListViews and ListLinks list a bucket's log views and linked
	// datasets.
	ListViews(ctx context.Context, project, location, bucket string) ([]View, error)
	ListLinks(ctx context.Context, project, location, bucket string) ([]Link, error)
}
