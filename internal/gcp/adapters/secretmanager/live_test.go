package secretmanager

import (
	"slices"
	"testing"

	sm "google.golang.org/api/secretmanager/v1"
)

func TestFromSecret(t *testing.T) {
	t.Parallel()
	got := fromSecret(&sm.Secret{
		Name: "projects/123/secrets/api-key",
		Replication: &sm.Replication{UserManaged: &sm.UserManaged{Replicas: []*sm.Replica{
			{Location: "us-central1", CustomerManagedEncryption: &sm.CustomerManagedEncryption{KmsKeyName: "k1"}},
			{Location: "us-east1", CustomerManagedEncryption: &sm.CustomerManagedEncryption{KmsKeyName: "k2"}},
		}}},
		Rotation:       &sm.Rotation{RotationPeriod: "86400s", NextRotationTime: "2026-11-01T00:00:00Z"},
		Topics:         []*sm.Topic{{Name: "projects/123/topics/secret-events"}},
		VersionAliases: map[string]string{"current": "3"},
		CreateTime:     "2025-03-04T11:00:00Z",
	})
	if got.Name != "api-key" || got.Replication != "user-managed" || len(got.Locations) != 2 || got.KMSKey != "k1" ||
		got.RotationPeriod != "86400s" || got.NextRotation.IsZero() || got.Topics[0] != "secret-events" ||
		got.Aliases["current"] != "3" || got.Created.IsZero() {
		t.Fatalf("fromSecret = %+v", got)
	}
	auto := fromSecret(&sm.Secret{Name: "x", Replication: &sm.Replication{Automatic: &sm.Automatic{}}})
	if auto.Replication != "automatic" || auto.KMSKey != "" {
		t.Fatalf("automatic = %+v", auto)
	}
}

func TestFromVersion(t *testing.T) {
	t.Parallel()
	got := fromVersion(&sm.SecretVersion{Name: "projects/123/secrets/s/versions/7", State: "DESTROYED", DestroyTime: "2025-01-10T12:00:00Z"})
	if got.Name != "7" || got.State != "DESTROYED" || got.Destroyed.IsZero() {
		t.Fatalf("fromVersion = %+v", got)
	}
}

func TestFromSecretTypeAndAnnotations(t *testing.T) {
	t.Parallel()
	got := fromSecret(&sm.Secret{
		Name: "projects/123/secrets/db", SecretType: "OTHER_DB_CREDENTIALS", Annotations: map[string]string{"owner": "data", "app": "billing"},
		CustomerManagedEncryption: &sm.CustomerManagedEncryption{KmsKeyName: "regional-key"},
	})
	if got.Type != "OTHER_DB_CREDENTIALS" || got.Annotations["owner"] != "data" || got.KMSKey != "regional-key" {
		t.Fatalf("fromSecret = %+v", got)
	}
	r := secretResource("p", got)
	if r.Metadata["type"] != "OTHER_DB_CREDENTIALS" || r.Metadata["annotations"] != "2" ||
		!slices.Equal(r.List("annotations"), []string{"app: billing", "owner: data"}) {
		t.Fatalf("secret resource = %+v %+v", r.Metadata, r.Lists)
	}
}

func TestFromVersionKMSKeyVersions(t *testing.T) {
	t.Parallel()
	got := fromVersion(&sm.SecretVersion{Name: "projects/123/secrets/db/versions/2", ReplicationStatus: &sm.ReplicationStatus{
		UserManaged: &sm.UserManagedStatus{Replicas: []*sm.ReplicaStatus{
			{Location: "us-central1", CustomerManagedEncryption: &sm.CustomerManagedEncryptionStatus{KmsKeyVersionName: "k1/cryptoKeyVersions/4"}},
			{Location: "us-east1", CustomerManagedEncryption: &sm.CustomerManagedEncryptionStatus{KmsKeyVersionName: "k2/cryptoKeyVersions/1"}},
		}},
	}})
	if !slices.Equal(got.KMSKeyVersions, []string{"k1/cryptoKeyVersions/4", "k2/cryptoKeyVersions/1"}) {
		t.Fatalf("fromVersion = %+v", got)
	}
	if r := versionResource("projects/p/secrets/db", got); r.Metadata["kmsKeyVersion"] != "k1/cryptoKeyVersions/4, k2/cryptoKeyVersions/1" {
		t.Fatalf("version resource = %+v", r.Metadata)
	}
}
