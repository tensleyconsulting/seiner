package bigquery

import (
	"context"
	"time"
)

// Dataset is the Google-free slice of a BigQuery dataset the adapter needs.
// The list call fills the first block; Detailed reports whether the rest
// was read from the dataset itself.
type Dataset struct {
	ID           string // dataset id, not the full name
	Location     string // US, EU, or a region
	Type         string // DEFAULT, LINKED, EXTERNAL
	FriendlyName string
	Labels       map[string]string

	Detailed            bool
	Description         string
	Created             time.Time
	Modified            time.Time
	TableExpiration     time.Duration // default for new tables; 0 = none
	PartitionExpiration time.Duration // default for new partitions; 0 = none
	KMSKey              string        // default CMEK for new tables
	TimeTravel          time.Duration
	StorageBilling      string // LOGICAL or PHYSICAL
	CaseInsensitive     bool
	LinkedSource        string // the source dataset of a LINKED dataset
	ExternalSource      string // the external source of an EXTERNAL dataset
	Access              []Access
}

// Access is one entry in a dataset's access list. An entry grants Role to
// Member, or authorizes a view, routine, or dataset (Authorized) to read
// the dataset.
type Access struct {
	Role       string // READER, WRITER, OWNER, or a role name
	Member     string // IAM member form: user:a@x, group:g@x, projectViewer:p, allAuthenticatedUsers
	Authorized string // "view p.d.v", "routine p.d.r", or "dataset p.d"
	Condition  string // condition title; "" when unconditional
}

// Table is one table, view, or other table-like entry in a dataset, from
// the list call (which reports no size or row count).
type Table struct {
	ID              string
	Type            string // TABLE, VIEW, MATERIALIZED_VIEW, EXTERNAL, SNAPSHOT, CLONE
	FriendlyName    string
	Created         time.Time
	Expires         time.Time
	Partitioning    string // "DAY on event_date", "RANGE on id", "HOUR"
	Clustering      []string
	LegacySQL       bool // a view written in legacy SQL
	Labels          map[string]string
	PartitionFilter bool // queries must filter on the partition column
}

// Routine is a stored procedure or user-defined function.
type Routine struct {
	ID       string
	Type     string // SCALAR_FUNCTION, PROCEDURE, TABLE_VALUED_FUNCTION, AGGREGATE_FUNCTION
	Language string // SQL, JAVASCRIPT, PYTHON, JAVA, SCALA
	Created  time.Time
	Modified time.Time
}

// Model is a BigQuery ML model.
type Model struct {
	ID           string
	Type         string // LINEAR_REGRESSION, KMEANS, ...
	FriendlyName string
	Created      time.Time
	Modified     time.Time
	Expires      time.Time
	Labels       map[string]string
}

// Reservation is a slot reservation in an administration project.
type Reservation struct {
	Name              string // short name
	Location          string
	Edition           string // STANDARD, ENTERPRISE, ENTERPRISE_PLUS
	Slots             int64  // baseline slots
	MaxSlots          int64  // autoscaling slots on top of the baseline; 0 = no autoscaling
	CurrentSlots      int64  // autoscaling slots now in use
	IgnoreIdle        bool   // do not borrow idle slots
	Concurrency       int64  // target job concurrency; 0 = automatic
	SecondaryLocation string // failover location of a managed disaster recovery reservation
	Created           time.Time
	Updated           time.Time
	Labels            map[string]string
}

// Assignment gives a project, folder, or organization a reservation's
// slots for one job type.
type Assignment struct {
	Name     string // assignment id
	Assignee string // projects/p, folders/f, or organizations/o
	JobType  string // QUERY, PIPELINE, ML_EXTERNAL, BACKGROUND, CONTINUOUS
	State    string // PENDING, ACTIVE
}

// Commitment is a capacity commitment: slots bought for a term.
type Commitment struct {
	Name        string // commitment id
	Location    string
	Edition     string
	Slots       int64
	Plan        string // FLEX, MONTHLY, ANNUAL, THREE_YEAR, ...
	RenewalPlan string
	State       string // PENDING, ACTIVE, FAILED
	Failure     string // why a FAILED commitment failed
	Start       time.Time
	End         time.Time
}

// Transfer is a Data Transfer Service config: a scheduled query or a
// transfer from another source.
type Transfer struct {
	ID          string // config id
	Location    string
	DisplayName string
	DataSource  string // scheduled_query, google_cloud_storage, ...
	Dataset     string // destination dataset id
	Schedule    string // "every 24 hours"
	Disabled    bool
	State       string // the latest run's: PENDING, RUNNING, SUCCEEDED, FAILED, CANCELLED
	Error       string // the latest run's error message
	NextRun     time.Time
	Updated     time.Time
	Owner       string // the owning user's email
	Topic       string // Pub/Sub topic for run notifications
	KMSKey      string
}

// Connection is an external connection: the identity BigQuery uses to
// reach Cloud SQL, Spanner, AWS, Azure, or Cloud resources. Credentials
// are never read.
type Connection struct {
	ID           string
	Location     string
	FriendlyName string
	Description  string
	Kind         string // Cloud SQL, Cloud Spanner, Cloud resource, Spark, AWS, Azure
	Identity     string // the service account, AWS role, or Azure identity it acts as
	Target       string // the instance, database, or account it reaches
	KMSKey       string
	Created      time.Time
	Modified     time.Time
}

// DatasetAPI is the BigQuery API ↔ Resource boundary. Tests inject a fake;
// live wraps the BigQuery REST client.
type DatasetAPI interface {
	// ListDatasets lists the datasets the caller can see, in every location.
	ListDatasets(ctx context.Context, project string) ([]Dataset, error)
	GetDataset(ctx context.Context, project, dataset string) (Dataset, error)
	ListTables(ctx context.Context, project, dataset string, limit int) ([]Table, bool, error)
	ListRoutines(ctx context.Context, project, dataset string, limit int) ([]Routine, bool, error)
	ListModels(ctx context.Context, project, dataset string, limit int) ([]Model, bool, error)
}

// ReservationAPI is the BigQuery Reservation API ↔ Resource boundary.
type ReservationAPI interface {
	// Locations is where to look: the API cannot list its locations.
	Locations(ctx context.Context, project string) ([]string, error)
	ListReservations(ctx context.Context, project, location string) ([]Reservation, error)
	ListAssignments(ctx context.Context, project, location, reservation string) ([]Assignment, error)
	ListCommitments(ctx context.Context, project, location string) ([]Commitment, error)
}

// TransferAPI is the BigQuery Data Transfer API ↔ Resource boundary.
type TransferAPI interface {
	Locations(ctx context.Context, project string) ([]string, error)
	ListTransfers(ctx context.Context, project, location string) ([]Transfer, error)
}

// ConnectionAPI is the BigQuery Connection API ↔ Resource boundary.
type ConnectionAPI interface {
	// Locations is where to look: the API cannot list its locations.
	Locations(ctx context.Context, project string) ([]string, error)
	ListConnections(ctx context.Context, project, location string) ([]Connection, error)
}
