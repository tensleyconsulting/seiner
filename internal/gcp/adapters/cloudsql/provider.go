package cloudsql

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tensleyconsulting/seiner/internal/gcp/display"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// Provider implements model.ResourceProvider for Cloud SQL instances. An
// instance expands to its databases.
type Provider struct{ api API }

func New(api API) Provider { return Provider{api: api} }

func (p Provider) Service() model.ServiceType { return model.ServiceCloudSQL }

// ListPermissions is what List needs.
func (p Provider) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "cloudsql.instances.list")
}

func (p Provider) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("cloudsql: no API")
	}
	insts, err := p.api.ListInstances(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(insts))
	for _, in := range insts {
		out = append(out, instanceResource(project, in))
	}
	return out, nil
}

// maxBackups caps an instance's backups. The API lists them newest first.
const maxBackups = 100

// ListRelated lists an instance's databases, users, and backups.
func (p Provider) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceCloudSQL {
		return nil, false, nil
	}
	switch facetID {
	case model.FacetDatabases, model.FacetUsers, model.FacetBackups:
	default:
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Kind != "instances" || len(n.Rest) != 0 {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("cloudsql: no API")
	}
	var (
		out       []model.Resource
		truncated bool
	)
	switch facetID {
	case model.FacetDatabases:
		dbs, err := p.api.ListDatabases(ctx, n.Project, n.Name)
		if err != nil {
			return nil, false, err
		}
		for _, db := range dbs {
			out = append(out, databaseResource(n.Project, parent.Location, parent.ID, db))
		}
	case model.FacetUsers:
		users, err := p.api.ListUsers(ctx, n.Project, n.Name)
		if err != nil {
			return nil, false, err
		}
		for _, u := range users {
			out = append(out, userResource(parent.Location, parent.ID, u))
		}
	case model.FacetBackups:
		runs, more, err := p.api.ListBackupRuns(ctx, n.Project, n.Name, maxBackups)
		if err != nil {
			return nil, false, err
		}
		truncated = more
		for _, b := range runs {
			out = append(out, backupResource(parent.Location, parent.ID, b))
		}
	}
	for i := range out {
		out[i].ScopeID = parent.ScopeID
	}
	return out, truncated, nil
}

func instanceID(project, name string) string {
	return "projects/" + project + "/instances/" + url.PathEscape(name)
}

func instanceResource(project string, in Instance) model.Resource {
	md := map[string]string{}
	set(md, "engine", in.DatabaseVersion)
	set(md, "edition", in.Edition)
	set(md, "tier", in.Tier)
	set(md, "role", role(in))
	set(md, "availability", in.Availability)
	zones := in.Zone
	if in.SecondaryZone != "" {
		zones += " (standby " + in.SecondaryZone + ")"
	}
	set(md, "zone", zones)
	if in.ActivationPolicy == "NEVER" {
		md["serving"] = "stopped"
	}
	set(md, "connectionName", in.ConnectionName)
	set(md, "publicIP", in.PublicIP)
	set(md, "privateIP", in.PrivateIP)
	set(md, "network", in.PrivateNetwork)
	if in.PSC {
		md["psc"] = "on"
	}
	if in.DiskSizeGB > 0 {
		d := strconv.FormatInt(in.DiskSizeGB, 10) + " GB"
		if in.DiskType != "" {
			d += " " + in.DiskType
		}
		if in.AutoResize {
			d += ", auto-resize"
		}
		md["storage"] = d
	}
	backups := onOff(in.Backups)
	if in.Backups && in.PITR {
		backups += ", point-in-time recovery"
	}
	md["backups"] = backups
	md["deletionProtection"] = onOff(in.DeletionProtect)
	set(md, "replicas", strings.Join(in.Replicas, ", "))
	if in.PublicIPEnabled {
		md["publicAccess"] = "on"
		md["authorizedNetworks"] = strconv.Itoa(len(in.AuthorizedNetworks))
	} else if in.PublicIP == "" {
		md["publicAccess"] = "off"
	}
	set(md, "ssl", sslMode(in))
	set(md, "kmsKey", in.KMSKey)
	set(md, "maintenance", maintenanceWindow(in))
	set(md, "nextMaintenance", display.Stamp(in.NextMaintenance))
	set(md, "denyMaintenance", strings.Join(in.DenyMaintenance, ", "))
	if in.Backups {
		if in.BackupStart != "" {
			md["backupWindow"] = in.BackupStart + " UTC"
		}
		set(md, "backupLocation", in.BackupLocation)
		if in.RetainedBackups > 0 {
			unit := "backups"
			if in.RetentionUnit != "" && in.RetentionUnit != "COUNT" {
				unit = strings.ToLower(in.RetentionUnit)
			}
			md["backupRetention"] = strconv.FormatInt(in.RetainedBackups, 10) + " " + unit
		}
		if in.PITR && in.LogRetentionDays > 0 {
			md["logRetention"] = days(in.LogRetentionDays)
		}
	}
	if in.QueryInsights {
		md["queryInsights"] = "on"
	}
	var lists []model.List
	if len(in.AuthorizedNetworks) > 0 {
		lists = append(lists, model.List{ID: "authorizedNetworks", Label: "Authorized networks", Items: slices.Clone(in.AuthorizedNetworks)})
	}
	if len(in.Flags) > 0 {
		md["flags"] = strconv.Itoa(len(in.Flags))
		lists = append(lists, model.List{ID: "flags", Label: "Database flags", Items: slices.Clone(in.Flags)})
	}
	return model.Resource{
		ID:       instanceID(project, in.Name),
		Name:     in.Name,
		Service:  model.ServiceCloudSQL,
		ScopeID:  "projects/" + project,
		Location: in.Region,
		Status:   instanceStatus(in.State, in.ActivationPolicy),
		State:    in.State,
		Created:  in.Created,
		Labels:   maps.Clone(in.Labels),
		Metadata: md,
		Fields: model.FieldsOf(md, "engine", "edition", "tier", "role", "availability", "zone", "serving",
			"connectionName", "publicAccess", "publicIP", "authorizedNetworks", "privateIP", "network", "psc", "ssl",
			"kmsKey", "storage", "backups", "backupWindow", "backupLocation", "backupRetention", "logRetention",
			"maintenance", "nextMaintenance", "denyMaintenance", "queryInsights", "flags", "deletionProtection", "replicas"),
		Lists:  lists,
		Facets: instanceFacets(in),
	}
}

func databaseResource(project, location, parentID string, db Database) model.Resource {
	md := map[string]string{}
	set(md, "charset", db.Charset)
	set(md, "collation", db.Collation)
	return model.Resource{
		ID:       parentID + "/databases/" + url.PathEscape(db.Name),
		Name:     db.Name,
		Service:  model.ServiceCloudSQLDatabase,
		ScopeID:  "projects/" + project,
		Location: location,
		Metadata: md,
		Fields:   model.FieldsOf(md, "charset", "collation"),
		ParentID: parentID,
		FacetID:  model.FacetDatabases,
	}
}

// instanceFacets: read replicas take no backups of their own, so they
// have no Backups facet.
func instanceFacets(in Instance) []model.Facet {
	facets := []model.Facet{
		{ID: model.FacetDatabases, Label: "Databases"},
		{ID: model.FacetUsers, Label: "Users"},
	}
	switch in.InstanceType {
	case "READ_REPLICA_INSTANCE", "READ_POOL_INSTANCE":
		return facets
	}
	return append(facets, model.Facet{ID: model.FacetBackups, Label: "Backups"})
}

// userResource is a database user. A locked or disabled user is a
// warning; otherwise the API says nothing about health.
func userResource(location, parentID string, u User) model.Resource {
	name := u.Name
	if u.Host != "" {
		name += "@" + u.Host
	}
	md := map[string]string{}
	set(md, "type", u.Type)
	set(md, "host", u.Host)
	set(md, "iamEmail", u.IAMEmail)
	set(md, "iamStatus", u.IAMStatus)
	set(md, "passwordExpires", display.Stamp(u.PasswordExpires))
	status, state := model.StatusUnknown, ""
	switch {
	case u.Disabled:
		status, state = model.StatusWarning, "DISABLED"
	case u.Locked:
		status, state = model.StatusWarning, "LOCKED"
	}
	var lists []model.List
	if len(u.Roles) > 0 {
		md["roles"] = strconv.Itoa(len(u.Roles))
		lists = []model.List{{ID: "roles", Label: "Roles", Items: slices.Clone(u.Roles)}}
	}
	return model.Resource{
		ID:       parentID + "/users/" + url.PathEscape(name),
		Name:     name,
		Service:  model.ServiceCloudSQLUser,
		Location: location,
		Status:   status,
		State:    state,
		Metadata: md,
		Fields:   model.FieldsOf(md, "type", "host", "iamEmail", "iamStatus", "passwordExpires", "roles"),
		Lists:    lists,
		Badge:    userBadge(u.Type),
		ParentID: parentID,
		FacetID:  model.FacetUsers,
	}
}

// userBadge tags IAM users; built-in users have no badge.
func userBadge(typ string) string {
	switch typ {
	case "CLOUD_IAM_USER":
		return "IAM"
	case "CLOUD_IAM_SERVICE_ACCOUNT":
		return "IAM SA"
	case "CLOUD_IAM_GROUP", "CLOUD_IAM_GROUP_USER", "CLOUD_IAM_GROUP_SERVICE_ACCOUNT":
		return "IAM GROUP"
	case "ENTRAID_USER":
		return "ENTRA"
	default:
		return ""
	}
}

// backupResource is one backup, named by when it started (or was queued).
func backupResource(location, parentID string, b BackupRun) model.Resource {
	when := b.Started
	if when.IsZero() {
		when = b.Enqueued
	}
	name := display.Stamp(when)
	if name == "" {
		name = b.ID
	}
	md := map[string]string{}
	set(md, "type", b.Type)
	set(md, "backupKind", b.Kind)
	set(md, "location", b.Location)
	set(md, "ended", display.Stamp(b.Ended))
	if !b.Started.IsZero() && b.Ended.After(b.Started) {
		md["duration"] = b.Ended.Sub(b.Started).Round(time.Second).String()
	}
	set(md, "error", b.Error)
	set(md, "kmsKey", b.KMSKey)
	set(md, "id", b.ID)
	return model.Resource{
		ID:          parentID + "/backupRuns/" + url.PathEscape(b.ID),
		Name:        name,
		Service:     model.ServiceCloudSQLBackup,
		Location:    location,
		Status:      backupStatus(b.Status),
		State:       b.Status,
		Created:     when,
		Description: b.Description,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "type", "backupKind", "location", "ended", "duration", "error", "kmsKey", "id"),
		Badge:       b.Status,
		ParentID:    parentID,
		FacetID:     model.FacetBackups,
	}
}

func backupStatus(s string) model.ResourceStatus {
	switch s {
	case "SUCCESSFUL":
		return model.StatusHealthy
	case "FAILED":
		return model.StatusError
	case "SKIPPED", "DELETION_FAILED":
		return model.StatusWarning
	default: // RUNNING, ENQUEUED, OVERDUE, DELETED, ...
		return model.StatusUnknown
	}
}

// sslMode is how the instance takes connections: the API's sslMode, or
// the legacy requireSsl flag.
func sslMode(in Instance) string {
	switch {
	case in.SSLMode != "" && in.SSLMode != "SSL_MODE_UNSPECIFIED":
		return in.SSLMode
	case in.RequireSSL:
		return "SSL required"
	default:
		return ""
	}
}

var weekdays = [...]string{"", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

// maintenanceWindow reads "Sun 03:00 UTC, week5". Without a window it
// shows only the update track, when one is chosen.
func maintenanceWindow(in Instance) string {
	track := in.UpdateTrack
	if track == "SQL_UPDATE_TRACK_UNSPECIFIED" {
		track = ""
	}
	if !in.MaintenanceSet || in.MaintenanceDay < 1 || in.MaintenanceDay > 7 {
		if track == "" {
			return ""
		}
		return "no window, " + track
	}
	s := fmt.Sprintf("%s %02d:00 UTC", weekdays[in.MaintenanceDay], in.MaintenanceHour)
	if track != "" {
		s += ", " + track
	}
	return s
}

func days(n int64) string {
	if n == 1 {
		return "1 day"
	}
	return strconv.FormatInt(n, 10) + " days"
}

// role is "primary", "read replica of p", or the API's instance type for
// anything else (on-premises sources).
func role(in Instance) string {
	switch in.InstanceType {
	case "CLOUD_SQL_INSTANCE", "":
		return "primary"
	case "READ_REPLICA_INSTANCE", "READ_POOL_INSTANCE":
		kind := "read replica"
		if in.InstanceType == "READ_POOL_INSTANCE" {
			kind = "read pool"
		}
		if in.Primary != "" {
			return kind + " of " + in.Primary
		}
		return kind
	default:
		return in.InstanceType
	}
}

// instanceStatus maps the serving state onto the coarse UI bucket. A
// RUNNABLE instance whose activation policy is NEVER is stopped.
func instanceStatus(state, activation string) model.ResourceStatus {
	switch state {
	case "RUNNABLE":
		if activation == "NEVER" {
			return model.StatusWarning
		}
		return model.StatusHealthy
	case "PENDING_CREATE", "PENDING_DELETE", "MAINTENANCE", "ONLINE_MAINTENANCE", "REPAIRING":
		return model.StatusWarning
	case "SUSPENDED", "FAILED":
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
	_ model.ResourceProvider = Provider{}
	_ model.RelatedLister    = Provider{}
)
