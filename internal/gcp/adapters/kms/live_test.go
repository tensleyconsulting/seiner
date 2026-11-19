package kms

import (
	"testing"

	cloudkms "google.golang.org/api/cloudkms/v1"
)

func TestFromKey(t *testing.T) {
	t.Parallel()
	got := fromKey(&cloudkms.CryptoKey{
		Name:             "projects/p/locations/us-central1/keyRings/r/cryptoKeys/k",
		Purpose:          "ENCRYPT_DECRYPT",
		VersionTemplate:  &cloudkms.CryptoKeyVersionTemplate{Algorithm: "GOOGLE_SYMMETRIC_ENCRYPTION", ProtectionLevel: "SOFTWARE"},
		Primary:          &cloudkms.CryptoKeyVersion{Name: "projects/p/locations/us-central1/keyRings/r/cryptoKeys/k/cryptoKeyVersions/4", State: "ENABLED", ProtectionLevel: "HSM"},
		RotationPeriod:   "7776000s",
		NextRotationTime: "2026-12-01T00:00:00Z",
		CryptoKeyBackend: "projects/p/locations/us-central1/ekmConnections/ekm",
		CreateTime:       "2024-04-01T09:00:00Z",
	})
	if got.Name != "k" || got.Primary != "4" || got.PrimaryState != "ENABLED" || got.Algorithm != "GOOGLE_SYMMETRIC_ENCRYPTION" ||
		got.ProtectionLevel != "HSM" || got.Backend != "ekm" || got.NextRotation.IsZero() || got.Created.IsZero() {
		t.Fatalf("fromKey = %+v", got)
	}
	asym := fromKey(&cloudkms.CryptoKey{Name: "x/cryptoKeys/s", Purpose: "ASYMMETRIC_SIGN", VersionTemplate: &cloudkms.CryptoKeyVersionTemplate{Algorithm: "EC_SIGN_P256_SHA256"}})
	if asym.Primary != "" || asym.PrimaryState != "" || asym.Algorithm != "EC_SIGN_P256_SHA256" {
		t.Fatalf("asymmetric = %+v", asym)
	}
}

func TestFromKeyTemplateAndJustifications(t *testing.T) {
	t.Parallel()
	got := fromKey(&cloudkms.CryptoKey{
		Name:                          "x/cryptoKeys/k",
		VersionTemplate:               &cloudkms.CryptoKeyVersionTemplate{Algorithm: "GOOGLE_SYMMETRIC_ENCRYPTION", ProtectionLevel: "HSM"},
		Primary:                       &cloudkms.CryptoKeyVersion{Name: "x/cryptoKeyVersions/1", ProtectionLevel: "SOFTWARE"},
		KeyAccessJustificationsPolicy: &cloudkms.KeyAccessJustificationsPolicy{AllowedAccessReasons: []string{"CUSTOMER_INITIATED_ACCESS"}},
	})
	if got.TemplateProtection != "HSM" || got.ProtectionLevel != "SOFTWARE" || len(got.AccessReasons) != 1 {
		t.Fatalf("fromKey = %+v", got)
	}
}

func TestFromVersion(t *testing.T) {
	t.Parallel()
	got := fromVersion(&cloudkms.CryptoKeyVersion{
		Name:                           "projects/p/locations/l/keyRings/r/cryptoKeys/k/cryptoKeyVersions/7",
		State:                          "IMPORT_FAILED",
		Algorithm:                      "RSA_SIGN_PSS_2048_SHA256",
		ProtectionLevel:                "EXTERNAL",
		CreateTime:                     "2024-04-01T09:00:00Z",
		DestroyTime:                    "2024-05-01T09:00:00Z",
		ImportJob:                      "projects/p/locations/l/keyRings/r/importJobs/migrate",
		ImportFailureReason:            "bad wrapping",
		ExternalProtectionLevelOptions: &cloudkms.ExternalProtectionLevelOptions{ExternalKeyUri: "https://ekm.example.com/k"},
		ReimportEligible:               true,
	})
	if got.Name != "7" || got.State != "IMPORT_FAILED" || got.ImportJob != "migrate" || got.FailureReason != "bad wrapping" ||
		got.ExternalKeyURI != "https://ekm.example.com/k" || !got.ReimportEligible || got.Created.IsZero() || got.DestroyScheduled.IsZero() {
		t.Fatalf("fromVersion = %+v", got)
	}
	vpc := fromVersion(&cloudkms.CryptoKeyVersion{Name: "v/2", ExternalProtectionLevelOptions: &cloudkms.ExternalProtectionLevelOptions{EkmConnectionKeyPath: "keys/k"}})
	if vpc.ExternalKeyURI != "keys/k" {
		t.Fatalf("EKM via VPC key path = %q", vpc.ExternalKeyURI)
	}
}

func TestFromImportJob(t *testing.T) {
	t.Parallel()
	got := fromImportJob(&cloudkms.ImportJob{
		Name:            "projects/p/locations/l/keyRings/r/importJobs/migrate",
		State:           "EXPIRED",
		ImportMethod:    "RSA_OAEP_3072_SHA256",
		ProtectionLevel: "HSM",
		ExpireTime:      "2024-04-04T09:00:00Z",
		ExpireEventTime: "2024-04-04T09:00:01Z",
		PublicKey:       &cloudkms.WrappingPublicKey{Pem: "-----BEGIN PUBLIC KEY-----"},
	})
	if got.Name != "migrate" || got.State != "EXPIRED" || got.Method != "RSA_OAEP_3072_SHA256" || got.Expires.IsZero() || got.Expired.IsZero() {
		t.Fatalf("fromImportJob = %+v", got)
	}
}

func TestFromBindings(t *testing.T) {
	t.Parallel()
	got := fromBindings([]*cloudkms.Binding{
		{Role: "roles/a", Members: []string{"user:a@example.com"}},
		{Role: "roles/b", Members: []string{"user:b@example.com"}, Condition: &cloudkms.Expr{Title: "temp"}},
		{Role: "roles/c", Condition: &cloudkms.Expr{Expression: "request.time < timestamp(\"2027-01-01T00:00:00Z\")"}},
	})
	if len(got) != 3 || got[0].Condition != "" || got[1].Condition != "temp" || got[2].Condition == "" || got[0].Members[0] != "user:a@example.com" {
		t.Fatalf("fromBindings = %+v", got)
	}
}

func TestFromAttestation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      *cloudkms.KeyOperationAttestation
		ok      bool
		content string
		wantErr bool
	}{
		{name: "none", in: nil},
		{name: "empty", in: &cloudkms.KeyOperationAttestation{}},
		{name: "std base64", in: &cloudkms.KeyOperationAttestation{Format: "CAVIUM_V2_COMPRESSED", Content: "H4sI/w==", CertChains: &cloudkms.CertificateChains{
			CaviumCerts: []string{"c1", "c2"}, GoogleCardCerts: []string{"g1"}, GooglePartitionCerts: []string{"p1"},
		}}, ok: true, content: "\x1f\x8b\x08\xff"},
		{name: "url base64", in: &cloudkms.KeyOperationAttestation{Content: "H4sI_w=="}, ok: true, content: "\x1f\x8b\x08\xff"},
		{name: "bad base64", in: &cloudkms.KeyOperationAttestation{Content: "!!"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok, err := fromAttestation(tt.in)
			if (err != nil) != tt.wantErr || ok != tt.ok || string(got.Content) != tt.content {
				t.Fatalf("fromAttestation = %+v, %v, %v", got, ok, err)
			}
			if tt.name == "std base64" && (got.Format != "CAVIUM_V2_COMPRESSED" || len(got.Cavium) != 2 || len(got.GoogleCard) != 1 || len(got.GooglePartition) != 1) {
				t.Fatalf("chains = %+v", got)
			}
		})
	}
}
