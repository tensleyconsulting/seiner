package fake

import (
	"net/url"
	"strings"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

var (
	sqlFields = []string{"engine", "edition", "tier", "role", "availability", "zone", "serving",
		"connectionName", "publicAccess", "publicIP", "authorizedNetworks", "privateIP", "network", "psc", "ssl",
		"kmsKey", "storage", "backups", "backupWindow", "backupLocation", "backupRetention", "logRetention",
		"maintenance", "nextMaintenance", "denyMaintenance", "queryInsights", "flags", "deletionProtection", "replicas"}
	redisFields = []string{"statusMessage", "displayName", "version", "tier", "capacity", "zone", "primaryEndpoint",
		"readEndpoint", "replicas", "network", "connectMode", "reservedIpRange", "auth", "transitEncryption", "persistence",
		"nextSnapshot", "cmek", "maintenance", "nextMaintenance"}
	valkeyFields = []string{"version", "mode", "nodeType", "nodeSize", "shards", "replicas", "zones", "endpoint",
		"network", "authorization", "transitEncryption", "persistence", "backups", "cmek", "replication",
		"maintenance", "nextMaintenance", "deletionProtection"}
)

const (
	bqAnalytics = "projects/demo-project/datasets/analytics"
	bqWarehouse = "projects/analytics-project/datasets/warehouse"
	bqETL       = "projects/demo-project/locations/US/reservations/prod-etl"
	bqAdhoc     = "projects/demo-project/locations/US/reservations/adhoc"
)

var (
	bqDatasetFields     = []string{"friendlyName", "type", "linkedSource", "externalSource", "tableExpiration", "partitionExpiration", "kmsKey", "timeTravel", "storageBilling", "caseInsensitive", "authorized", "modified"}
	bqTableFields       = []string{"type", "friendlyName", "partitioning", "partitionFilter", "clustering", "sql", "expires"}
	bqReservationFields = []string{"edition", "slots", "autoscale", "idleSlots", "concurrency", "secondaryLocation", "updated"}
	bqCommitmentFields  = []string{"failure", "edition", "slots", "plan", "renewalPlan", "start", "end"}
	bqTransferFields    = []string{"error", "dataSource", "destination", "schedule", "nextRun", "owner", "notifications", "kmsKey", "configId", "updated"}
	bqConnectionFields  = []string{"friendlyName", "type", "identity", "target", "kmsKey", "modified"}
)

// bigQueryFixtures is two datasets with their tables, routines, models,
// and access lists, plus demo-project's reservations, commitments,
// transfers, and connections.
func bigQueryFixtures() []model.Resource {
	analytics := extra(ordered(bqDataset(bqAnalytics, "projects/demo-project", "Product analytics events and rollups", map[string]string{"friendlyName": "Product analytics", "kmsKey": "projects/demo-project/locations/us/keyRings/bq/cryptoKeys/analytics", "timeTravel": "7 days", "storageBilling": "PHYSICAL", "partitionExpiration": "400 days", "authorized": "1", "modified": "2026-10-30 04:12 UTC"}), bqDatasetFields...), "", "2023-11-02T10:00:00Z", map[string]string{"team": "data", "env": "prod"})
	analytics.Lists = []model.List{{ID: "authorized", Label: "Authorized views, routines, and datasets", Items: []string{"view demo-project.reporting.daily_revenue"}}}
	warehouse := extra(ordered(bqDataset(bqWarehouse, "projects/analytics-project", "Analytics warehouse", map[string]string{"friendlyName": "Warehouse", "tableExpiration": "90 days", "timeTravel": "2 days", "storageBilling": "LOGICAL", "modified": "2026-09-14 08:00 UTC"}), bqDatasetFields...), "", "2022-04-18T09:00:00Z", map[string]string{"team": "data"})
	return []model.Resource{
		analytics,
		bqTable(bqAnalytics, "events", "TABLE", "2023-11-02T10:05:00Z", map[string]string{"partitioning": "DAY on event_date", "partitionFilter": "required", "clustering": "user_id, event_name"}),
		bqTable(bqAnalytics, "sessions", "TABLE", "2023-11-02T10:05:00Z", map[string]string{"partitioning": "DAY on started_at", "clustering": "user_id"}),
		bqTable(bqAnalytics, "daily_active_users", "VIEW", "2024-01-10T12:00:00Z", map[string]string{"friendlyName": "Daily active users"}),
		bqTable(bqAnalytics, "revenue_by_day", "MATERIALIZED_VIEW", "2024-03-05T09:30:00Z", map[string]string{"partitioning": "DAY on day"}),
		bqTable(bqAnalytics, "events_raw", "EXTERNAL", "2024-06-21T15:00:00Z", map[string]string{"friendlyName": "Raw events in GCS"}),
		bqTable(bqAnalytics, "events_backup_20261001", "SNAPSHOT", "2026-10-01T00:00:00Z", map[string]string{"expires": "2026-12-31 00:00 UTC"}),
		bqRoutine(bqAnalytics, "normalize_email", "SCALAR_FUNCTION", "SQL", "2024-02-01T09:00:00Z", "2025-05-12 10:20 UTC"),
		bqRoutine(bqAnalytics, "refresh_rollups", "PROCEDURE", "SQL", "2024-03-05T09:00:00Z", "2026-08-02 06:45 UTC"),
		bqModel(bqAnalytics, "churn_predictor", "LOGISTIC_REGRESSION", "2025-07-14T11:00:00Z", map[string]string{"friendlyName": "Churn predictor", "modified": "2026-10-01 03:00 UTC"}),
		bqBinding(bqAnalytics, "roles/bigquery.dataOwner", "projectOwner:demo-project"),
		bqBinding(bqAnalytics, "roles/bigquery.dataViewer", "group:analysts@example.com", "projectViewer:demo-project"),
		bqBinding(bqAnalytics, "roles/bigquery.dataEditor", "serviceAccount:etl@demo-project.iam.gserviceaccount.com", "projectEditor:demo-project"),

		warehouse,
		bqTable(bqWarehouse, "orders", "TABLE", "2022-04-18T09:10:00Z", map[string]string{"partitioning": "MONTH on order_date", "clustering": "region"}),
		bqTable(bqWarehouse, "customers", "TABLE", "2022-04-18T09:10:00Z", nil),
		bqRoutine(bqWarehouse, "fiscal_quarter", "SCALAR_FUNCTION", "SQL", "2022-05-01T09:00:00Z", ""),
		bqModel(bqWarehouse, "demand_forecast", "ARIMA_PLUS", "2025-01-20T08:00:00Z", map[string]string{"expires": "2027-01-20 08:00 UTC"}),
		bqBinding(bqWarehouse, "roles/bigquery.dataViewer", "group:analysts@example.com"),
		bqBinding(bqWarehouse, "roles/bigquery.dataOwner", "projectOwner:analytics-project"),

		bqReservation(bqETL, "Pipelines and scheduled queries", map[string]string{"edition": "ENTERPRISE", "slots": "500", "autoscale": "up to 500 more (100 in use)", "idleSlots": "used", "updated": "2026-09-01 12:00 UTC"}, "2024-06-01T00:00:00Z"),
		bqAssignment(bqETL, "8821739401", "projects/demo-project", "PIPELINE", "ACTIVE"),
		bqAssignment(bqETL, "8821739402", "projects/analytics-project", "QUERY", "ACTIVE"),
		bqAssignment(bqETL, "8821739403", "folders/prod-data", "QUERY", "PENDING"),
		bqReservation(bqAdhoc, "Analyst ad hoc queries", map[string]string{"edition": "STANDARD", "slots": "0", "autoscale": "up to 200 more (0 in use)", "idleSlots": "not used", "concurrency": "10"}, "2025-02-10T00:00:00Z"),
		bqAssignment(bqAdhoc, "9011234567", "projects/checkout-project", "QUERY", "ACTIVE"),
		bqCommitment("US", "4421873390", "ACTIVE", map[string]string{"edition": "ENTERPRISE", "slots": "500", "plan": "ANNUAL", "renewalPlan": "ANNUAL", "start": "2026-01-01 00:00 UTC", "end": "2027-01-01 00:00 UTC"}),
		bqCommitment("EU", "4421873391", "FAILED", map[string]string{"edition": "ENTERPRISE", "slots": "100", "plan": "MONTHLY", "failure": "Not enough slot quota in EU; request an increase"}),
		bqTransfer("us", "6d1a0b2c-0000-4a1b-8c2d-0a1b2c3d4e51", "Daily revenue rollup", "SUCCEEDED", map[string]string{"dataSource": "scheduled_query", "destination": "analytics", "schedule": "every 24 hours", "nextRun": "2026-11-03 04:00 UTC", "owner": "alice@example.com", "notifications": "projects/demo-project/topics/bq-transfer-runs", "updated": "2026-08-02 06:45 UTC"}),
		bqTransfer("us", "6d1a0b2c-0000-4a1b-8c2d-0a1b2c3d4e52", "Import events from GCS", "FAILED", map[string]string{"error": "Not found: URI gs://my-app-bucket/events/2026-11-01/*.json", "dataSource": "google_cloud_storage", "destination": "analytics", "schedule": "every 1 hours", "nextRun": "2026-11-02 15:00 UTC", "owner": "etl@demo-project.iam.gserviceaccount.com"}),
		bqTransfer("us", "6d1a0b2c-0000-4a1b-8c2d-0a1b2c3d4e53", "Ads import (paused)", "SUCCEEDED", map[string]string{"dataSource": "google_ads", "destination": "analytics", "schedule": "every 24 hours (disabled)", "owner": "bob@example.com"}),
		bqConnection("us", "cloudsql-orders", "Reads the orders database", map[string]string{"friendlyName": "Orders (Cloud SQL)", "type": "Cloud SQL POSTGRES", "identity": "service-123456789012@gcp-sa-bigqueryconnection.iam.gserviceaccount.com", "target": "demo-project:us-central1:postgres-prod / app", "modified": "2025-03-02 10:00 UTC"}),
		bqConnection("us", "biglake-events", "BigLake tables over the events bucket", map[string]string{"type": "Cloud resource", "identity": "bqcx-123456789012-a1b2@gcp-sa-bigquery-condel.iam.gserviceaccount.com", "kmsKey": "projects/demo-project/locations/us/keyRings/bq/cryptoKeys/analytics"}),
	}
}

// bqDataset is a dataset with its four facets.
func bqDataset(id, scope, desc string, md map[string]string) model.Resource {
	name := id[strings.LastIndexByte(id, '/')+1:]
	r := res(id, name, model.ServiceBigQueryDataset, scope, "US", "", desc, md)
	r.Facets = []model.Facet{
		{ID: model.FacetTables, Label: "Tables"},
		{ID: model.FacetRoutines, Label: "Routines"},
		{ID: model.FacetModels, Label: "Models"},
		{ID: model.FacetPermissions, Label: "Permissions"},
	}
	return r
}

// bqChild is a dataset's facet child of kind (tables, routines, models).
func bqChild(dataset, facet, kind, name string, svc model.ServiceType, md map[string]string) model.Resource {
	scope, _, _ := strings.Cut(dataset, "/datasets/")
	return under(res(dataset+"/"+kind+"/"+name, name, svc, scope, "US", "", "", md), dataset, facet)
}

func bqTable(dataset, name, typ, created string, md map[string]string) model.Resource {
	if md == nil {
		md = map[string]string{}
	}
	md["type"] = typ
	r := bqChild(dataset, model.FacetTables, "tables", name, model.ServiceBigQueryTable, md)
	r.Badge = map[string]string{"VIEW": "VIEW", "MATERIALIZED_VIEW": "MV", "EXTERNAL": "EXT", "SNAPSHOT": "SNAP", "CLONE": "CLONE"}[typ]
	return extra(ordered(r, bqTableFields...), "", created, nil)
}

func bqRoutine(dataset, name, typ, lang, created, modified string) model.Resource {
	md := map[string]string{"type": typ, "language": lang}
	if modified != "" {
		md["modified"] = modified
	}
	r := bqChild(dataset, model.FacetRoutines, "routines", name, model.ServiceBigQueryRoutine, md)
	r.Badge = map[string]string{"PROCEDURE": "PROC", "SCALAR_FUNCTION": "UDF", "TABLE_VALUED_FUNCTION": "TVF", "AGGREGATE_FUNCTION": "UDAF"}[typ]
	return extra(ordered(r, "type", "language", "modified"), "", created, nil)
}

func bqModel(dataset, name, typ, created string, md map[string]string) model.Resource {
	md["type"] = typ
	r := bqChild(dataset, model.FacetModels, "models", name, model.ServiceBigQueryModel, md)
	return extra(ordered(r, "type", "friendlyName", "modified", "expires"), "", created, nil)
}

// bqBinding is a dataset's Permissions facet child.
func bqBinding(dataset, role string, members ...string) model.Resource {
	scope, _, _ := strings.Cut(dataset, "/datasets/")
	return under(model.BindingResource(dataset, scope, role, members, ""), dataset, model.FacetPermissions)
}

func bqReservation(id, desc string, md map[string]string, created string) model.Resource {
	name := id[strings.LastIndexByte(id, '/')+1:]
	r := res(id, name, model.ServiceBigQueryReservation, "projects/demo-project", "US", "", desc, md)
	r.Facets = []model.Facet{{ID: model.FacetAssignments, Label: "Assignments", Inline: true}}
	return extra(ordered(r, bqReservationFields...), "", created, nil)
}

func bqAssignment(reservation, id, assignee, jobType, state string) model.Resource {
	st := map[string]model.ResourceStatus{"ACTIVE": model.StatusHealthy, "PENDING": model.StatusWarning}[state]
	r := res(reservation+"/assignments/"+id, assignee, model.ServiceBigQueryAssignment, "projects/demo-project", "US", st, "", map[string]string{"assignee": assignee, "jobType": jobType, "assignmentId": id})
	r.Badge = jobType
	return under(extra(ordered(r, "assignee", "jobType", "assignmentId"), state, "", nil), reservation, model.FacetAssignments)
}

func bqCommitment(loc, id, state string, md map[string]string) model.Resource {
	st := map[string]model.ResourceStatus{"ACTIVE": model.StatusHealthy, "PENDING": model.StatusWarning, "FAILED": model.StatusError}[state]
	r := res("projects/demo-project/locations/"+loc+"/capacityCommitments/"+id, id, model.ServiceBigQueryCommitment, "projects/demo-project", loc, st, "", md)
	r.Badge = md["plan"]
	return extra(ordered(r, bqCommitmentFields...), state, "", nil)
}

func bqTransfer(loc, id, name, state string, md map[string]string) model.Resource {
	st := map[string]model.ResourceStatus{"SUCCEEDED": model.StatusHealthy, "FAILED": model.StatusError, "CANCELLED": model.StatusWarning}[state]
	md["configId"] = id
	r := res("projects/demo-project/locations/"+loc+"/transferConfigs/"+id, name, model.ServiceBigQueryTransfer, "projects/demo-project", loc, st, "", md)
	if strings.HasSuffix(md["schedule"], "(disabled)") {
		r.Badge = "OFF"
		r.Status = model.StatusWarning
	}
	return extra(ordered(r, bqTransferFields...), state, "", nil)
}

func bqConnection(loc, id, desc string, md map[string]string) model.Resource {
	r := res("projects/demo-project/locations/"+loc+"/connections/"+id, id, model.ServiceBigQueryConnection, "projects/demo-project", loc, "", desc, md)
	return extra(ordered(r, bqConnectionFields...), "", "2025-03-01T09:00:00Z", nil)
}

const sqlPrefix = "projects/demo-project/instances/"

// sqlInstance is a Cloud SQL instance in us-central1 with its databases,
// users, and (unless a read replica) backups. flags are its database flags.
func sqlInstance(name string, md map[string]string, flags ...string) model.Resource {
	r := res(sqlPrefix+name, name, model.ServiceCloudSQL, "projects/demo-project", "us-central1", model.StatusHealthy, "", md)
	r.Facets = []model.Facet{{ID: model.FacetDatabases, Label: "Databases"}, {ID: model.FacetUsers, Label: "Users"}}
	if !strings.HasPrefix(md["role"], "read replica") {
		r.Facets = append(r.Facets, model.Facet{ID: model.FacetBackups, Label: "Backups"})
	}
	if len(flags) > 0 {
		r.Lists = append(r.Lists, model.List{ID: "flags", Label: "Database flags", Items: flags})
	}
	if md["authorizedNetworks"] != "" {
		r.Lists = append(r.Lists, model.List{ID: "authorizedNetworks", Label: "Authorized networks", Items: []string{"office 203.0.113.0/24", "198.51.100.7/32 (expires 2026-12-01)"}})
	}
	return ordered(r, sqlFields...)
}

func sqlDatabase(instance, name, charset, collation string) model.Resource {
	r := res(sqlPrefix+instance+"/databases/"+name, name, model.ServiceCloudSQLDatabase, "projects/demo-project", "us-central1", "", "", map[string]string{"charset": charset, "collation": collation})
	return under(ordered(r, "charset", "collation"), sqlPrefix+instance, model.FacetDatabases)
}

// sqlUser is a database user; name includes "@host" for MySQL. state is
// LOCKED, DISABLED, or "".
func sqlUser(instance, name, badge, state string, md map[string]string, roles ...string) model.Resource {
	st := model.ResourceStatus("")
	if state != "" {
		st = model.StatusWarning
	}
	r := res(sqlPrefix+instance+"/users/"+url.PathEscape(name), name, model.ServiceCloudSQLUser, "projects/demo-project", "us-central1", st, "", md)
	r.Badge = badge
	if len(roles) > 0 {
		r.Lists = []model.List{{ID: "roles", Label: "Roles", Items: roles}}
	}
	return under(extra(ordered(r, "type", "host", "iamEmail", "iamStatus", "passwordExpires", "roles"), state, "", nil), sqlPrefix+instance, model.FacetUsers)
}

// sqlBackup is one backup run, named by its start time.
func sqlBackup(instance, id, started, state, desc string, md map[string]string) model.Resource {
	st := map[string]model.ResourceStatus{"SUCCESSFUL": model.StatusHealthy, "FAILED": model.StatusError}[state]
	t, err := time.Parse(time.RFC3339, started)
	if err != nil {
		panic(err)
	}
	r := res(sqlPrefix+instance+"/backupRuns/"+id, t.UTC().Format("2006-01-02 15:04 UTC"), model.ServiceCloudSQLBackup, "projects/demo-project", "us-central1", st, desc, md)
	r.Badge = state
	return under(extra(ordered(r, "type", "backupKind", "location", "ended", "duration", "error", "kmsKey", "id"), state, started, nil), sqlPrefix+instance, model.FacetBackups)
}

// valkeyFix is a cluster-mode Valkey instance with its PSC endpoints.
func valkeyFix() model.Resource {
	r := res("projects/demo-project/locations/us-central1/instances/valkey-cache", "valkey-cache", model.ServiceValkey, "projects/demo-project", "us-central1", model.StatusHealthy, "", map[string]string{"version": "VALKEY_8_0", "mode": "CLUSTER", "nodeType": "HIGHMEM_MEDIUM", "nodeSize": "13 GB", "shards": "3", "replicas": "1", "zones": "MULTI_ZONE", "endpoint": "10.40.0.2:6379", "network": "default", "authorization": "IAM_AUTH", "transitEncryption": "SERVER_AUTHENTICATION", "persistence": "AOF", "backups": "on, daily 02:00 UTC, keep 7 days", "maintenance": "SATURDAY 04:30 UTC", "deletionProtection": "on"})
	r.Lists = []model.List{{ID: "endpoints", Label: "Endpoints", Items: []string{"DISCOVERY  10.40.0.2:6379", "DISCOVERY  10.40.0.3:6379"}}}
	return extra(ordered(r, valkeyFields...), "ACTIVE", "2025-06-11T15:20:00Z", map[string]string{"env": "prod", "team": "checkout"})
}
