package cloudsql

import (
	"context"
	"time"
)

// Instance is the Google-free slice of a Cloud SQL instance the adapter
// needs.
type Instance struct {
	Name             string
	Region           string
	Zone             string // primary zone; "" when unknown
	SecondaryZone    string // standby zone of a regional instance
	State            string // the API's enum word, e.g. "RUNNABLE"
	ActivationPolicy string // ALWAYS, NEVER (stopped), ON_DEMAND
	DatabaseVersion  string // POSTGRES_16, MYSQL_8_0, SQLSERVER_2022_STANDARD
	Edition          string // ENTERPRISE, ENTERPRISE_PLUS
	Tier             string // db-custom-2-7680
	Availability     string // ZONAL or REGIONAL
	InstanceType     string // CLOUD_SQL_INSTANCE, READ_REPLICA_INSTANCE, ...
	Primary          string // the primary's name for a replica
	Replicas         []string
	ConnectionName   string // project:region:instance
	PublicIP         string
	PrivateIP        string
	PrivateNetwork   string // network name
	PSC              bool
	DiskSizeGB       int64
	DiskType         string
	AutoResize       bool
	Backups          bool
	PITR             bool
	DeletionProtect  bool
	Created          time.Time
	Labels           map[string]string

	PublicIPEnabled    bool     // ipv4Enabled: the instance has a public IP
	AuthorizedNetworks []string // "name value", or "value"; " (expires …)" when set
	SSLMode            string   // ENCRYPTED_ONLY, TRUSTED_CLIENT_CERTIFICATE_REQUIRED, ...
	RequireSSL         bool     // legacy requireSsl
	KMSKey             string
	MaintenanceDay     int64  // 1 = Monday … 7 = Sunday; 0 = any day
	MaintenanceHour    int64  // hour of day, UTC
	MaintenanceSet     bool   // a maintenance window is configured
	UpdateTrack        string // canary, stable, week5
	NextMaintenance    time.Time
	DenyMaintenance    []string // "start – end" for each deny period
	BackupStart        string   // HH:MM, UTC
	BackupLocation     string
	RetainedBackups    int64
	RetentionUnit      string // COUNT
	LogRetentionDays   int64
	QueryInsights      bool
	Flags              []string // "name=value"
}

// User is one database user. Passwords are never read.
type User struct {
	Name            string
	Host            string // MySQL host pattern; "" for other engines
	Type            string // BUILT_IN, CLOUD_IAM_USER, CLOUD_IAM_SERVICE_ACCOUNT, CLOUD_IAM_GROUP, ...
	IAMEmail        string
	IAMStatus       string
	Locked          bool
	PasswordExpires time.Time
	Disabled        bool     // SQL Server
	Roles           []string // database roles, or SQL Server server roles
}

// BackupRun is one backup of an instance.
type BackupRun struct {
	ID          string
	Status      string // SUCCESSFUL, FAILED, RUNNING, ENQUEUED, SKIPPED, ...
	Type        string // AUTOMATED, ON_DEMAND
	Kind        string // SNAPSHOT, PHYSICAL
	Location    string
	Description string
	Error       string
	KMSKey      string
	Enqueued    time.Time
	Started     time.Time
	Ended       time.Time
}

// Database is one database in an instance.
type Database struct {
	Name      string
	Charset   string
	Collation string
}

// API is the Cloud SQL ↔ Resource boundary. Tests inject a fake; live wraps
// the SQL Admin REST client.
type API interface {
	ListInstances(ctx context.Context, project string) ([]Instance, error)
	ListDatabases(ctx context.Context, project, instance string) ([]Database, error)
	ListUsers(ctx context.Context, project, instance string) ([]User, error)
	// ListBackupRuns lists an instance's backups, newest first, stopping
	// after limit.
	ListBackupRuns(ctx context.Context, project, instance string, limit int) ([]BackupRun, bool, error)
}
