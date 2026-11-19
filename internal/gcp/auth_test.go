package gcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func TestResolveServiceAccount(t *testing.T) {
	t.Parallel()
	find := func(context.Context, ...string) (adcResult, error) {
		return adcResult{
			JSON: []byte(`{
				"type": "service_account",
				"project_id": "adc-proj",
				"client_email": "sa@adc-proj.iam.gserviceaccount.com"
			}`),
			ProjectID: "adc-proj",
		}, nil
	}
	id := resolveSession(context.Background(), "", find).Identity
	if id.Err != nil {
		t.Fatal(id.Err)
	}
	if id.Source != model.IdentityADC {
		t.Fatalf("source %q", id.Source)
	}
	if id.Account != "sa@adc-proj.iam.gserviceaccount.com" {
		t.Fatalf("account %q", id.Account)
	}
	if id.ADCProject != "adc-proj" {
		t.Fatalf("project %q", id.ADCProject)
	}
	if id.Banner() != "" {
		t.Fatalf("banner %q", id.Banner())
	}
}

func TestResolveImpersonate(t *testing.T) {
	t.Parallel()
	find := func(context.Context, ...string) (adcResult, error) {
		return adcResult{
			JSON:      []byte(`{"type":"authorized_user","quota_project_id":"quota-proj"}`),
			ProjectID: "",
		}, nil
	}
	id := resolveSession(context.Background(), "target@x.iam.gserviceaccount.com", find).Identity
	if id.Err != nil {
		t.Fatal(id.Err)
	}
	if id.Source != model.IdentityImpersonate {
		t.Fatalf("source %q", id.Source)
	}
	if id.Account != "target@x.iam.gserviceaccount.com" {
		t.Fatalf("account %q", id.Account)
	}
	if id.ADCProject != "quota-proj" {
		t.Fatalf("project %q", id.ADCProject)
	}
}

func TestResolveMissingADC(t *testing.T) {
	t.Parallel()
	find := func(context.Context, ...string) (adcResult, error) {
		return adcResult{}, errors.New("could not find default credentials")
	}
	id := resolveSession(context.Background(), "", find).Identity
	if id.Err == nil {
		t.Fatal("expected err")
	}
	if id.Source != model.IdentityNone {
		t.Fatalf("source %q", id.Source)
	}
	if id.DisplayAccount() != "not signed in" {
		t.Fatalf("account %q", id.DisplayAccount())
	}
	if !strings.Contains(id.Banner(), "auth:") {
		t.Fatalf("banner %q", id.Banner())
	}
}

func TestResolveMissingADCStillShowsImpersonateTarget(t *testing.T) {
	t.Parallel()
	find := func(context.Context, ...string) (adcResult, error) {
		return adcResult{}, errors.New("no ADC")
	}
	id := resolveSession(context.Background(), "sa@x.iam.gserviceaccount.com", find).Identity
	if id.Err == nil {
		t.Fatal("expected err")
	}
	if id.Account != "sa@x.iam.gserviceaccount.com" {
		t.Fatalf("account %q", id.Account)
	}
}

func TestResolveCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	find := func(context.Context, ...string) (adcResult, error) {
		called = true
		return adcResult{}, errors.New("should not run")
	}
	id := resolveSession(ctx, "", find).Identity
	if !called && id.Err == nil {
		t.Fatal("expected ctx err")
	}
	if id.Err == nil {
		t.Fatal("expected err")
	}
}

func TestParseADCVariants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		adc         adcResult
		wantAccount string
		wantProject string
	}{
		{
			name:        "empty json",
			adc:         adcResult{ProjectID: "meta-proj"},
			wantAccount: "compute metadata",
			wantProject: "meta-proj",
		},
		{
			name:        "user",
			adc:         adcResult{JSON: []byte(`{"type":"authorized_user"}`)},
			wantAccount: "user (ADC)",
		},
		{
			name:        "wif",
			adc:         adcResult{JSON: []byte(`{"type":"external_account"}`)},
			wantAccount: "workload identity",
		},
		{
			name:        "bad json",
			adc:         adcResult{JSON: []byte(`{`), ProjectID: "p"},
			wantAccount: "ADC",
			wantProject: "p",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			account, project := parseADC(tc.adc)
			if account != tc.wantAccount {
				t.Errorf("account %q want %q", account, tc.wantAccount)
			}
			if project != tc.wantProject {
				t.Errorf("project %q want %q", project, tc.wantProject)
			}
		})
	}
}

func TestResolveDoesNotCallFindWhenCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	id := resolveSession(ctx, "sa@x", func(context.Context, ...string) (adcResult, error) {
		t.Fatal("find must not run")
		return adcResult{}, nil
	}).Identity
	if id.Account != "sa@x" {
		t.Fatalf("account %q", id.Account)
	}
}

func TestQuotaProject(t *testing.T) {
	cases := []struct {
		name        string
		env         string
		impersonate string
		json        string
		want        string
	}{
		{name: "user ADC quota project", json: `{"type":"authorized_user","quota_project_id":"q"}`, want: "q"},
		{name: "user ADC without one", json: `{"type":"authorized_user"}`, want: ""},
		{name: "key bills its own project", json: `{"type":"service_account","project_id":"sa-proj"}`, want: "sa-proj"},
		{name: "key with quota project", json: `{"type":"service_account","project_id":"sa-proj","quota_project_id":"q"}`, want: "q"},
		{name: "environment wins", env: "env-q", json: `{"type":"authorized_user","quota_project_id":"q"}`, want: "env-q"},
		{name: "impersonated account's project", impersonate: "sa@ops.iam.gserviceaccount.com", json: `{"type":"authorized_user","quota_project_id":"q"}`, want: "ops"},
		{name: "impersonated default account", impersonate: "1-compute@developer.gserviceaccount.com", want: ""},
		{name: "environment wins over impersonation", env: "env-q", impersonate: "sa@ops.iam.gserviceaccount.com", want: "env-q"},
		{name: "metadata server", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(quotaProjectEnv, tc.env)
			s := Session{
				Identity: model.Identity{Impersonate: tc.impersonate},
				creds:    &google.Credentials{JSON: []byte(tc.json)},
			}
			if tc.json == "" {
				s.creds.JSON = nil
			}
			if got := s.quotaProject(); got != tc.want {
				t.Fatalf("quotaProject %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWithUserEmail(t *testing.T) {
	t.Parallel()
	creds := &google.Credentials{TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t"})}
	found := func(context.Context, oauth2.TokenSource) (string, error) { return "dev@example.com", nil }
	failed := func(context.Context, oauth2.TokenSource) (string, error) { return "", errors.New("offline") }
	tests := []struct {
		name   string
		sess   Session
		lookup emailFunc
		want   string
	}{
		{"user ADC gets the email", Session{Identity: model.Identity{Account: userADC}, creds: creds}, found, "dev@example.com"},
		{"lookup failure keeps the label", Session{Identity: model.Identity{Account: userADC}, creds: creds}, failed, userADC},
		{"service account untouched", Session{Identity: model.Identity{Account: "sa@p.iam.gserviceaccount.com"}, creds: creds}, found, "sa@p.iam.gserviceaccount.com"},
		{"impersonation untouched", Session{Identity: model.Identity{Account: "sa@x", Impersonate: "sa@x"}, creds: creds}, found, "sa@x"},
		{"no credentials", Session{Identity: model.Identity{Account: userADC}}, found, userADC},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.sess.withUserEmail(context.Background(), tt.lookup).Identity.Account; got != tt.want {
				t.Errorf("Account = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFetchEmail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr bool
	}{
		{"email", http.StatusOK, `{"sub":"1","email":"dev@example.com","email_verified":true}`, "dev@example.com", false},
		{"no email scope", http.StatusOK, `{"sub":"1"}`, "", true},
		{"rejected", http.StatusUnauthorized, `{"error":"invalid_token"}`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer t" {
					t.Errorf("Authorization = %q; the token belongs in the header", got)
				}
				if r.URL.RawQuery != "" {
					t.Errorf("query %q; the token must not be in the URL", r.URL.RawQuery)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			got, err := fetchEmail(context.Background(), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t"}), srv.URL)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("fetchEmail = %q, %v; want %q, err %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}
