package model

import (
	"errors"
	"strings"
	"testing"
)

func TestIdentityDisplayAndBanner(t *testing.T) {
	t.Parallel()
	ok := Identity{Account: "sa@x", Source: IdentityADC}
	if ok.DisplayAccount() != "sa@x" {
		t.Fatalf("account %q", ok.DisplayAccount())
	}
	if ok.Banner() != "" {
		t.Fatalf("banner %q", ok.Banner())
	}

	missing := Identity{Err: errors.New("could not find default credentials")}
	if missing.DisplayAccount() != "not signed in" {
		t.Fatalf("account %q", missing.DisplayAccount())
	}
	if !strings.Contains(missing.Banner(), "could not find default credentials") {
		t.Fatalf("banner %q", missing.Banner())
	}
	if strings.Contains(missing.Banner(), "fixture") {
		t.Fatalf("nothing falls back to the demo catalog: %q", missing.Banner())
	}

	notice := Identity{Account: "sa@x", Source: IdentityADC, Notice: NoticeNoProject}
	if notice.Banner() != "" {
		t.Fatalf("a notice is not an auth banner: %q", notice.Banner())
	}
}

func TestRejectedBanner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		ident Identity
		msg   string
		want  string
	}{
		{"adc", Identity{Account: "user (ADC)", Source: IdentityADC}, "sign-in expired",
			"auth: sign-in expired; run gcloud auth application-default login, then press r"},
		{"impersonate", Identity{Source: IdentityImpersonate, Impersonate: "sa@p.iam.gserviceaccount.com"}, "credentials rejected",
			"auth: credentials rejected impersonating sa@p.iam.gserviceaccount.com; check the source credentials, then press r"},
		{"metadata server", Identity{Account: AccountMetadata, Source: IdentityADC}, "credentials rejected",
			"auth: credentials rejected (compute metadata); check the credentials seiner runs with, then press r"},
		{"workload identity", Identity{Account: AccountWorkloadIdentity, Source: IdentityADC}, "credentials rejected",
			"auth: credentials rejected (workload identity); check the credentials seiner runs with, then press r"},
		{"empty message", Identity{Source: IdentityADC}, "",
			"auth: credentials rejected; run gcloud auth application-default login, then press r"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.ident.RejectedBanner(tt.msg); got != tt.want {
				t.Errorf("RejectedBanner(%q) = %q, want %q", tt.msg, got, tt.want)
			}
		})
	}
}
