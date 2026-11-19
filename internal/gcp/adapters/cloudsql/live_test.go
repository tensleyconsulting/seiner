package cloudsql

import (
	"slices"
	"testing"

	sqladmin "google.golang.org/api/sqladmin/v1"
)

func TestFromInstance(t *testing.T) {
	t.Parallel()
	resize := true
	got := fromInstance(&sqladmin.DatabaseInstance{
		Name: "pg-replica", Region: "us-central1", GceZone: "us-central1-b", State: "RUNNABLE",
		InstanceType: "READ_REPLICA_INSTANCE", MasterInstanceName: "p:pg", ReplicaNames: []string{},
		IpAddresses: []*sqladmin.IpMapping{
			{Type: "PRIMARY", IpAddress: "198.51.100.90"},
			{Type: "OUTGOING", IpAddress: "198.51.100.91"},
			{Type: "PRIVATE", IpAddress: "10.20.0.7"},
		},
		Settings: &sqladmin.Settings{
			ActivationPolicy: "NEVER", Tier: "db-custom-2-7680", DataDiskSizeGb: 100, StorageAutoResize: &resize,
			UserLabels:          map[string]string{"env": "prod"},
			BackupConfiguration: &sqladmin.BackupConfiguration{Enabled: true, BinaryLogEnabled: true},
			IpConfiguration: &sqladmin.IpConfiguration{
				PrivateNetwork: "projects/p/global/networks/default",
				PscConfig:      &sqladmin.PscConfig{PscEnabled: true},
				Ipv4Enabled:    true,
				SslMode:        "ENCRYPTED_ONLY",
				AuthorizedNetworks: []*sqladmin.AclEntry{
					{Name: "office", Value: "203.0.113.0/24"},
					{Value: "198.51.100.7/32", ExpirationTime: "2026-12-01T00:00:00Z"},
				},
			},
			MaintenanceWindow:      &sqladmin.MaintenanceWindow{Day: 7, Hour: 3, UpdateTrack: "stable"},
			DenyMaintenancePeriods: []*sqladmin.DenyMaintenancePeriod{{StartDate: "2026-12-15", EndDate: "2027-01-05"}},
			InsightsConfig:         &sqladmin.InsightsConfig{QueryInsightsEnabled: true},
			DatabaseFlags:          []*sqladmin.DatabaseFlags{{Name: "max_connections", Value: "500"}},
		},
		DiskEncryptionConfiguration: &sqladmin.DiskEncryptionConfiguration{KmsKeyName: "projects/p/locations/l/keyRings/r/cryptoKeys/k"},
		ScheduledMaintenance:        &sqladmin.SqlScheduledMaintenance{StartTime: "2026-11-08T03:00:00Z"},
	})
	if got.Primary != "pg" || got.PublicIP != "198.51.100.90" || got.PrivateIP != "10.20.0.7" ||
		got.PrivateNetwork != "default" || !got.PSC || !got.AutoResize || !got.Backups || !got.PITR ||
		got.ActivationPolicy != "NEVER" || got.Labels["env"] != "prod" || !slices.Equal(got.Replicas, []string{}) {
		t.Fatalf("fromInstance = %+v", got)
	}
	if !got.PublicIPEnabled || got.SSLMode != "ENCRYPTED_ONLY" || got.KMSKey == "" || !got.MaintenanceSet || got.MaintenanceDay != 7 ||
		got.UpdateTrack != "stable" || got.NextMaintenance.IsZero() || !got.QueryInsights ||
		!slices.Equal(got.Flags, []string{"max_connections=500"}) || !slices.Equal(got.DenyMaintenance, []string{"2026-12-15 – 2027-01-05"}) ||
		!slices.Equal(got.AuthorizedNetworks, []string{"office 203.0.113.0/24", "198.51.100.7/32 (expires 2026-12-01)"}) {
		t.Fatalf("fromInstance settings = %+v", got)
	}
	unset := fromInstance(&sqladmin.DatabaseInstance{Name: "y", Settings: &sqladmin.Settings{MaintenanceWindow: &sqladmin.MaintenanceWindow{}}})
	if unset.MaintenanceSet || maintenanceWindow(unset) != "" {
		t.Fatalf("an unset window {day: 0, hour: 0} is no window: %+v", unset)
	}
	if bare := fromInstance(&sqladmin.DatabaseInstance{Name: "x"}); bare.Name != "x" || bare.Backups {
		t.Fatalf("instance without settings = %+v", bare)
	}
}

func TestFromUser(t *testing.T) {
	t.Parallel()
	got := fromUser(&sqladmin.User{
		Name: "report", Host: "%", Type: "BUILT_IN", Password: "never read",
		PasswordPolicy: &sqladmin.UserPasswordValidationPolicy{Status: &sqladmin.PasswordStatus{Locked: true, PasswordExpirationTime: "2026-12-01T00:00:00Z"}},
	})
	if got.Name != "report" || got.Host != "%" || !got.Locked || got.PasswordExpires.IsZero() {
		t.Fatalf("fromUser = %+v", got)
	}
	ss := fromUser(&sqladmin.User{Name: "sa", SqlserverUserDetails: &sqladmin.SqlServerUserDetails{Disabled: true, ServerRoles: []string{"sysadmin"}}})
	if !ss.Disabled || !slices.Equal(ss.Roles, []string{"sysadmin"}) {
		t.Fatalf("SQL Server user = %+v", ss)
	}
}

func TestFromBackupRun(t *testing.T) {
	t.Parallel()
	got := fromBackupRun(&sqladmin.BackupRun{
		Id: 1730257200000, Status: "FAILED", Type: "ON_DEMAND", BackupKind: "SNAPSHOT", Location: "us",
		EnqueuedTime: "2026-10-30T02:00:00Z", Error: &sqladmin.OperationError{Code: "DISK_FULL"},
		DiskEncryptionConfiguration: &sqladmin.DiskEncryptionConfiguration{KmsKeyName: "k"},
	})
	if got.ID != "1730257200000" || got.Status != "FAILED" || got.Error != "DISK_FULL" || got.KMSKey != "k" || got.Enqueued.IsZero() {
		t.Fatalf("fromBackupRun = %+v", got)
	}
}
