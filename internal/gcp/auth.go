package gcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/impersonate"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// CloudPlatform is the OAuth scope gcloud ADC actually grants.
// IAM still has to grant the principal viewer-level roles on the resources.
const CloudPlatform = "https://www.googleapis.com/auth/cloud-platform"

// adcResult is the Google-free slice of ADC that resolveSession needs.
type adcResult struct {
	JSON      []byte
	ProjectID string
	Creds     *google.Credentials
}

// Session holds ADC for live clients. The UI never sees this type.
type Session struct {
	Identity model.Identity
	creds    *google.Credentials
}

// Ready reports whether live clients can be constructed.
func (s Session) Ready() bool {
	return s.Identity.Err == nil && s.creds != nil
}

type adcFindFunc func(ctx context.Context, scopes ...string) (adcResult, error)

func findGoogleADC(ctx context.Context, scopes ...string) (adcResult, error) {
	if err := ctx.Err(); err != nil {
		return adcResult{}, err
	}
	// Token refresh must outlive the startup timeout that called Open.
	c, err := google.FindDefaultCredentials(context.WithoutCancel(ctx), scopes...)
	if err != nil {
		return adcResult{}, err
	}
	return adcResult{JSON: c.JSON, ProjectID: c.ProjectID, Creds: c}, nil
}

// ResolveSession loads Application Default Credentials and optional
// impersonation, and keeps the credentials handle for live clients. It does
// not call resource APIs. On failure the Identity still carries the error so
// the TUI can show a banner.
func ResolveSession(ctx context.Context, impersonate string) Session {
	return resolveSession(ctx, impersonate, findGoogleADC).withUserEmail(ctx, userEmail)
}

// userADC is the account label for user credentials whose email is unknown:
// authorized_user ADC JSON carries none.
const userADC = "user (ADC)"

// userinfoURL is Google's OpenID Connect userinfo endpoint. It returns the
// signed-in user's email for a token with the userinfo.email scope, which
// gcloud auth application-default login grants by default.
const userinfoURL = "https://openidconnect.googleapis.com/v1/userinfo"

// emailTimeout bounds the email lookup, which only improves a label.
const emailTimeout = 5 * time.Second

type emailFunc func(ctx context.Context, ts oauth2.TokenSource) (string, error)

// withUserEmail replaces the userADC label with the user's email, read with
// one userinfo call. Any failure (no network, a token without the scope)
// keeps the label; it never fails the session.
func (s Session) withUserEmail(ctx context.Context, lookup emailFunc) Session {
	if s.Identity.Account != userADC || s.Identity.Impersonate != "" || s.creds == nil || s.creds.TokenSource == nil {
		return s
	}
	email, err := lookup(ctx, s.creds.TokenSource)
	if err != nil {
		slog.Info("user email unavailable; showing a generic account label", "err", err)
		return s
	}
	s.Identity.Account = email
	return s
}

func userEmail(ctx context.Context, ts oauth2.TokenSource) (string, error) {
	return fetchEmail(ctx, ts, userinfoURL)
}

func fetchEmail(ctx context.Context, ts oauth2.TokenSource, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, emailTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := oauth2.NewClient(ctx, ts).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("userinfo: %s", resp.Status)
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil {
		return "", fmt.Errorf("userinfo: %w", err)
	}
	if strings.TrimSpace(body.Email) == "" {
		return "", errors.New("userinfo: no email (token lacks the userinfo.email scope)")
	}
	return strings.TrimSpace(body.Email), nil
}

func resolveSession(ctx context.Context, impersonate string, find adcFindFunc) Session {
	impersonate = strings.TrimSpace(impersonate)
	id := model.Identity{Source: model.IdentityNone, Impersonate: impersonate}

	if err := ctx.Err(); err != nil {
		id.Err = err
		if impersonate != "" {
			id.Account = impersonate
		}
		return Session{Identity: id}
	}

	adc, err := find(ctx, CloudPlatform)
	if err != nil {
		id.Err = err
		if impersonate != "" {
			id.Account = impersonate
		}
		return Session{Identity: id}
	}

	account, project := parseADC(adc)
	id.ADCProject = project
	id.Source = model.IdentityADC
	id.Account = account

	if impersonate != "" {
		id.Source = model.IdentityImpersonate
		id.Account = impersonate
	}
	return Session{Identity: id, creds: adc.Creds}
}

func (s Session) clientOptions(ctx context.Context) ([]option.ClientOption, error) {
	if s.creds == nil {
		return nil, errors.New("no credentials")
	}
	// Do not pass option.WithQuotaProject: storage.NewClient uses WithHTTPClient,
	// and those two options are incompatible. User ADC JSON already carries
	// quota_project_id via WithCredentialsJSON.
	if s.Identity.Impersonate != "" {
		ts, err := s.tokenSource(ctx)
		if err != nil {
			return nil, err
		}
		return []option.ClientOption{option.WithTokenSource(ts)}, nil
	}
	if len(s.creds.JSON) > 0 {
		return []option.ClientOption{option.WithCredentialsJSON(s.creds.JSON)}, nil
	}
	if s.creds.TokenSource != nil {
		return []option.ClientOption{option.WithTokenSource(s.creds.TokenSource)}, nil
	}
	return nil, errors.New("no credentials")
}

func (s Session) tokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	if s.creds == nil {
		return nil, errors.New("no credentials")
	}
	if s.Identity.Impersonate == "" {
		return s.creds.TokenSource, nil
	}
	return impersonate.CredentialsTokenSource(context.WithoutCancel(ctx), impersonate.CredentialsConfig{
		TargetPrincipal: s.Identity.Impersonate,
		Scopes:          []string{CloudPlatform},
	}, option.WithTokenSource(s.creds.TokenSource))
}

type adcFile struct {
	Type           string `json:"type"`
	ClientEmail    string `json:"client_email"`
	ProjectID      string `json:"project_id"`
	QuotaProjectID string `json:"quota_project_id"`
}

func parseADC(adc adcResult) (account, project string) {
	project = strings.TrimSpace(adc.ProjectID)
	if len(adc.JSON) == 0 {
		if project == "" {
			return model.AccountMetadata, ""
		}
		return model.AccountMetadata, project
	}

	var f adcFile
	if err := json.Unmarshal(adc.JSON, &f); err != nil {
		return "ADC", project
	}
	if project == "" {
		project = strings.TrimSpace(f.ProjectID)
	}
	if project == "" {
		project = strings.TrimSpace(f.QuotaProjectID)
	}

	switch {
	case strings.TrimSpace(f.ClientEmail) != "":
		account = strings.TrimSpace(f.ClientEmail)
	case f.Type == "authorized_user":
		account = userADC
	case f.Type == "external_account":
		account = model.AccountWorkloadIdentity
	case f.Type != "":
		account = f.Type
	default:
		account = "ADC"
	}
	return account, project
}

// quotaProjectEnv overrides the quota project for every request, as the
// client libraries read it.
const quotaProjectEnv = "GOOGLE_CLOUD_QUOTA_PROJECT"

// quotaProject is the project Google checks client-based API calls
// against, by the client libraries' rules: GOOGLE_CLOUD_QUOTA_PROJECT, then
// the credentials' quota_project_id (sent as x-goog-user-project). Without
// either, a service account principal, impersonated or keyed, is billed to
// its own project. "" when it cannot be told.
func (s Session) quotaProject() string {
	if env := strings.TrimSpace(os.Getenv(quotaProjectEnv)); env != "" {
		return env
	}
	if s.Identity.Impersonate != "" {
		return serviceAccountProject(s.Identity.Impersonate)
	}
	if s.creds == nil || len(s.creds.JSON) == 0 {
		return ""
	}
	var f adcFile
	if err := json.Unmarshal(s.creds.JSON, &f); err != nil {
		return ""
	}
	if q := strings.TrimSpace(f.QuotaProjectID); q != "" {
		return q
	}
	if f.Type == "service_account" {
		return strings.TrimSpace(f.ProjectID)
	}
	return ""
}

// serviceAccountProject is the project id in a user-managed service
// account's email (name@project.iam.gserviceaccount.com), or "".
func serviceAccountProject(email string) string {
	_, domain, ok := strings.Cut(strings.TrimSpace(email), "@")
	if !ok {
		return ""
	}
	project, ok := strings.CutSuffix(domain, ".iam.gserviceaccount.com")
	if !ok {
		return ""
	}
	return project
}
