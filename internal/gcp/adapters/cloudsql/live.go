package cloudsql

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/option"
	sqladmin "google.golang.org/api/sqladmin/v1"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
)

type liveAPI struct{ svc *sqladmin.Service }

// NewLazy defers client construction to the first call, so a slow or failing
// constructor cannot block startup; the failure surfaces as this service's
// ProviderError instead.
func NewLazy(opts ...option.ClientOption) Provider {
	l := lazy.New(func(ctx context.Context) (*sqladmin.Service, error) {
		return sqladmin.NewService(ctx, opts...)
	})
	return New(lazyAPI{l: l})
}

type lazyAPI struct {
	l *lazy.Value[*sqladmin.Service]
}

func (a lazyAPI) ListInstances(ctx context.Context, project string) ([]Instance, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListInstances(ctx, project)
}

func (a lazyAPI) ListDatabases(ctx context.Context, project, instance string) ([]Database, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListDatabases(ctx, project, instance)
}

func (a lazyAPI) ListUsers(ctx context.Context, project, instance string) ([]User, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListUsers(ctx, project, instance)
}

func (a lazyAPI) ListBackupRuns(ctx context.Context, project, instance string, limit int) ([]BackupRun, bool, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveAPI{svc: svc}.ListBackupRuns(ctx, project, instance, limit)
}

var (
	_ API = lazyAPI{}
	_ API = liveAPI{}
)

func (a liveAPI) ListInstances(ctx context.Context, project string) ([]Instance, error) {
	var out []Instance
	err := a.svc.Instances.List(project).Pages(ctx, func(resp *sqladmin.InstancesListResponse) error {
		for _, in := range resp.Items {
			out = append(out, fromInstance(in))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListDatabases returns every database; the API does not page them.
func (a liveAPI) ListDatabases(ctx context.Context, project, instance string) ([]Database, error) {
	resp, err := a.svc.Databases.List(project, instance).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	out := make([]Database, 0, len(resp.Items))
	for _, db := range resp.Items {
		out = append(out, fromDatabase(db))
	}
	return out, nil
}

// ListUsers returns every user; the API does not page them.
func (a liveAPI) ListUsers(ctx context.Context, project, instance string) ([]User, error) {
	resp, err := a.svc.Users.List(project, instance).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	out := make([]User, 0, len(resp.Items))
	for _, u := range resp.Items {
		out = append(out, fromUser(u))
	}
	return out, nil
}

// errStop ends paging once the limit is reached.
var errStop = errors.New("stop")

func (a liveAPI) ListBackupRuns(ctx context.Context, project, instance string, limit int) ([]BackupRun, bool, error) {
	var out []BackupRun
	truncated := false
	err := a.svc.BackupRuns.List(project, instance).Pages(ctx, func(resp *sqladmin.BackupRunsListResponse) error {
		for _, b := range resp.Items {
			if len(out) == limit {
				truncated = true
				return errStop
			}
			out = append(out, fromBackupRun(b))
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return nil, false, err
	}
	return out, truncated, nil
}

func fromUser(u *sqladmin.User) User {
	out := User{
		Name:      u.Name,
		Host:      u.Host,
		Type:      u.Type,
		IAMEmail:  u.IamEmail,
		IAMStatus: u.IamStatus,
		Roles:     slices.Clone(u.DatabaseRoles),
	}
	if p := u.PasswordPolicy; p != nil && p.Status != nil {
		out.Locked = p.Status.Locked
		out.PasswordExpires = parseTime(p.Status.PasswordExpirationTime)
	}
	if d := u.SqlserverUserDetails; d != nil {
		out.Disabled = d.Disabled
		if len(out.Roles) == 0 {
			out.Roles = slices.Clone(d.ServerRoles)
		}
	}
	return out
}

func fromBackupRun(b *sqladmin.BackupRun) BackupRun {
	out := BackupRun{
		ID:          strconv.FormatInt(b.Id, 10),
		Status:      b.Status,
		Type:        b.Type,
		Kind:        b.BackupKind,
		Location:    b.Location,
		Description: b.Description,
		Enqueued:    parseTime(b.EnqueuedTime),
		Started:     parseTime(b.StartTime),
		Ended:       parseTime(b.EndTime),
	}
	if e := b.Error; e != nil {
		out.Error = e.Message
		if out.Error == "" {
			out.Error = e.Code
		}
	}
	if d := b.DiskEncryptionConfiguration; d != nil {
		out.KMSKey = d.KmsKeyName
	}
	return out
}

func fromInstance(in *sqladmin.DatabaseInstance) Instance {
	out := Instance{
		Name:            in.Name,
		Region:          in.Region,
		Zone:            in.GceZone,
		SecondaryZone:   in.SecondaryGceZone,
		State:           in.State,
		DatabaseVersion: in.DatabaseVersion,
		InstanceType:    in.InstanceType,
		Primary:         afterColon(in.MasterInstanceName),
		Replicas:        in.ReplicaNames,
		ConnectionName:  in.ConnectionName,
		Created:         parseTime(in.CreateTime),
	}
	for _, ip := range in.IpAddresses {
		switch ip.Type {
		case "PRIMARY":
			out.PublicIP = ip.IpAddress
		case "PRIVATE":
			out.PrivateIP = ip.IpAddress
		}
	}
	if d := in.DiskEncryptionConfiguration; d != nil {
		out.KMSKey = d.KmsKeyName
	}
	if m := in.ScheduledMaintenance; m != nil {
		out.NextMaintenance = parseTime(m.StartTime)
	}
	if s := in.Settings; s != nil {
		out.ActivationPolicy = s.ActivationPolicy
		out.Edition = s.Edition
		out.Tier = s.Tier
		out.Availability = s.AvailabilityType
		out.DiskSizeGB = s.DataDiskSizeGb
		out.DiskType = s.DataDiskType
		out.AutoResize = s.StorageAutoResize != nil && *s.StorageAutoResize
		out.DeletionProtect = s.DeletionProtectionEnabled
		out.Labels = s.UserLabels
		if b := s.BackupConfiguration; b != nil {
			out.Backups = b.Enabled
			out.PITR = b.PointInTimeRecoveryEnabled || b.BinaryLogEnabled
			out.BackupStart = b.StartTime
			out.BackupLocation = b.Location
			out.LogRetentionDays = b.TransactionLogRetentionDays
			if r := b.BackupRetentionSettings; r != nil {
				out.RetainedBackups = r.RetainedBackups
				out.RetentionUnit = r.RetentionUnit
			}
		}
		if ipc := s.IpConfiguration; ipc != nil {
			out.PrivateNetwork = lastSeg(ipc.PrivateNetwork)
			out.PSC = ipc.PscConfig != nil && ipc.PscConfig.PscEnabled
			out.PublicIPEnabled = ipc.Ipv4Enabled
			out.SSLMode = ipc.SslMode
			out.RequireSSL = ipc.RequireSsl
			for _, n := range ipc.AuthorizedNetworks {
				out.AuthorizedNetworks = append(out.AuthorizedNetworks, aclEntry(n))
			}
		}
		// The API returns {day: 0, hour: 0} when no window is set; day 0
		// means "no preference", so only a day of 1–7 is a window.
		if w := s.MaintenanceWindow; w != nil {
			out.MaintenanceSet = w.Day >= 1 && w.Day <= 7
			out.MaintenanceDay = w.Day
			out.MaintenanceHour = w.Hour
			out.UpdateTrack = w.UpdateTrack
		}
		for _, d := range s.DenyMaintenancePeriods {
			out.DenyMaintenance = append(out.DenyMaintenance, d.StartDate+" – "+d.EndDate)
		}
		out.QueryInsights = s.InsightsConfig != nil && s.InsightsConfig.QueryInsightsEnabled
		for _, f := range s.DatabaseFlags {
			out.Flags = append(out.Flags, f.Name+"="+f.Value)
		}
	}
	return out
}

// aclEntry is an authorized network as "name value (expires …)", leaving
// out what is not set.
func aclEntry(n *sqladmin.AclEntry) string {
	s := n.Value
	if n.Name != "" {
		s = n.Name + " " + s
	}
	if t := parseTime(n.ExpirationTime); !t.IsZero() {
		s += " (expires " + t.UTC().Format("2006-01-02") + ")"
	}
	return s
}

func fromDatabase(db *sqladmin.Database) Database {
	return Database{Name: db.Name, Charset: db.Charset, Collation: db.Collation}
}

// afterColon turns the API's project:instance into instance.
func afterColon(s string) string {
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func lastSeg(link string) string {
	link = strings.TrimSuffix(link, "/")
	if i := strings.LastIndexByte(link, '/'); i >= 0 {
		return link[i+1:]
	}
	return link
}
