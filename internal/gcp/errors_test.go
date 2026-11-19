package gcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"cloud.google.com/go/auth"
	"github.com/googleapis/gax-go/v2/apierror"
	"golang.org/x/oauth2"
	"google.golang.org/api/googleapi"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/tensleyconsulting/seiner/internal/model"
)

func grpcErr(t *testing.T, c codes.Code, msg string, info *errdetails.ErrorInfo) error {
	t.Helper()
	st := status.New(c, msg)
	if info != nil {
		var err error
		if st, err = st.WithDetails(info); err != nil {
			t.Fatal(err)
		}
	}
	return st.Err()
}

func TestClassify(t *testing.T) {
	t.Parallel()
	disabledMsg := "Kubernetes Engine API has not been used in project 123 before or it is disabled. " +
		"Enable it by visiting https://console.developers.google.com/apis/api/container.googleapis.com/overview?project=123 then retry."
	tests := []struct {
		name     string
		err      func(t *testing.T) error
		wantKind model.ErrorKind
		wantMsg  string
	}{
		{
			name:     "deadline",
			err:      func(*testing.T) error { return fmt.Errorf("list: %w", context.DeadlineExceeded) },
			wantKind: model.ErrKindTimeout,
			wantMsg:  "timed out",
		},
		{
			name:     "canceled",
			err:      func(*testing.T) error { return fmt.Errorf("list: %w", context.Canceled) },
			wantKind: model.ErrKindCanceled,
			wantMsg:  "canceled",
		},
		{
			name: "googleapi 403 quoted permission",
			err: func(*testing.T) error {
				return &googleapi.Error{Code: 403, Message: "Required 'container.clusters.list' permission for 'projects/p'."}
			},
			wantKind: model.ErrKindPermissionDenied,
			wantMsg:  "missing container.clusters.list",
		},
		{
			name: "googleapi 403 storage access form",
			err: func(*testing.T) error {
				return fmt.Errorf("list buckets: %w", &googleapi.Error{
					Code:    403,
					Message: "sa@p.iam.gserviceaccount.com does not have storage.buckets.list access to the Google Cloud project.",
				})
			},
			wantKind: model.ErrKindPermissionDenied,
			wantMsg:  "missing storage.buckets.list",
		},
		{
			name:     "googleapi 403 no permission named",
			err:      func(*testing.T) error { return &googleapi.Error{Code: 403, Message: "Forbidden"} },
			wantKind: model.ErrKindPermissionDenied,
			wantMsg:  "permission denied",
		},
		{
			name: "googleapi accessNotConfigured",
			err: func(*testing.T) error {
				return &googleapi.Error{
					Code:    403,
					Message: disabledMsg,
					Errors:  []googleapi.ErrorItem{{Reason: "accessNotConfigured", Message: disabledMsg}},
				}
			},
			wantKind: model.ErrKindAPIDisabled,
			wantMsg:  "enable container.googleapis.com in project 123",
		},
		{
			name:     "googleapi 403 disabled by message",
			err:      func(*testing.T) error { return &googleapi.Error{Code: 403, Message: disabledMsg} },
			wantKind: model.ErrKindAPIDisabled,
			wantMsg:  "enable container.googleapis.com in project 123",
		},
		{
			name:     "googleapi org policy restriction",
			err:      func(t *testing.T) error { return restrictedErr(t, "bigquery.googleapis.com", "prj-a") },
			wantKind: model.ErrKindAPIDisabled,
			wantMsg:  "organization policy blocks bigquery.googleapis.com in project prj-a",
		},
		{
			name: "googleapi org policy restriction by message",
			err: func(*testing.T) error {
				return &googleapi.Error{Code: 403, Message: "Request is disallowed by organization's constraints/gcp.restrictServiceUsage constraint " +
					"for 'projects/prj-a' attempting to use service 'bigquery.googleapis.com'."}
			},
			wantKind: model.ErrKindAPIDisabled,
			wantMsg:  "organization policy blocks bigquery.googleapis.com in project prj-a",
		},
		{
			name: "googleapi 403 quoted API name is not a permission",
			err: func(*testing.T) error {
				return &googleapi.Error{Code: 403, Message: "Caller may not use 'bigquery.googleapis.com'."}
			},
			wantKind: model.ErrKindPermissionDenied,
			wantMsg:  "permission denied",
		},
		{
			name:     "googleapi 404",
			err:      func(*testing.T) error { return &googleapi.Error{Code: 404, Message: "bucket gone"} },
			wantKind: model.ErrKindNotFound,
			wantMsg:  "not found",
		},
		{
			name:     "googleapi 429",
			err:      func(*testing.T) error { return &googleapi.Error{Code: 429, Message: "rate limited"} },
			wantKind: model.ErrKindQuota,
			wantMsg:  "quota exceeded",
		},
		{
			name: "googleapi 403 rate limit",
			err: func(*testing.T) error {
				return &googleapi.Error{Code: 403, Message: "Rate Limit Exceeded", Errors: []googleapi.ErrorItem{{Reason: "rateLimitExceeded"}}}
			},
			wantKind: model.ErrKindQuota,
			wantMsg:  "quota exceeded",
		},
		{
			name: "googleapi 403 user rate limit",
			err: func(*testing.T) error {
				return &googleapi.Error{Code: 403, Message: "User Rate Limit Exceeded", Errors: []googleapi.ErrorItem{{Reason: "userRateLimitExceeded"}}}
			},
			wantKind: model.ErrKindQuota,
			wantMsg:  "quota exceeded",
		},
		{
			name: "grpc billing disabled",
			err: func(t *testing.T) error {
				return grpcErr(t, codes.PermissionDenied, "This API method requires billing to be enabled.", &errdetails.ErrorInfo{
					Reason: "BILLING_DISABLED",
					Domain: "googleapis.com",
				})
			},
			wantKind: model.ErrKindUnknown,
			wantMsg:  "billing disabled",
		},
		{
			name: "grpc permission with ErrorInfo",
			err: func(t *testing.T) error {
				return grpcErr(t, codes.PermissionDenied, "Permission denied on resource project p.", &errdetails.ErrorInfo{
					Reason:   "IAM_PERMISSION_DENIED",
					Domain:   "iam.googleapis.com",
					Metadata: map[string]string{"permission": "compute.instances.list"},
				})
			},
			wantKind: model.ErrKindPermissionDenied,
			wantMsg:  "missing compute.instances.list",
		},
		{
			name: "grpc SERVICE_DISABLED via apierror",
			err: func(t *testing.T) error {
				ae, ok := apierror.FromError(grpcErr(t, codes.PermissionDenied, disabledMsg, &errdetails.ErrorInfo{
					Reason:   "SERVICE_DISABLED",
					Domain:   "googleapis.com",
					Metadata: map[string]string{"service": "pubsub.googleapis.com", "consumer": "projects/demo-project"},
				}))
				if !ok {
					t.Fatal("apierror.FromError")
				}
				return fmt.Errorf("list topics: %w", ae)
			},
			wantKind: model.ErrKindAPIDisabled,
			wantMsg:  "enable pubsub.googleapis.com in project demo-project",
		},
		{
			name:     "grpc not found",
			err:      func(t *testing.T) error { return grpcErr(t, codes.NotFound, "no such thing", nil) },
			wantKind: model.ErrKindNotFound,
			wantMsg:  "not found",
		},
		{
			name:     "grpc resource exhausted",
			err:      func(t *testing.T) error { return grpcErr(t, codes.ResourceExhausted, "quota", nil) },
			wantKind: model.ErrKindQuota,
			wantMsg:  "quota exceeded",
		},
		{
			name:     "grpc deadline",
			err:      func(t *testing.T) error { return grpcErr(t, codes.DeadlineExceeded, "slow", nil) },
			wantKind: model.ErrKindTimeout,
			wantMsg:  "timed out",
		},
		{
			name:     "grpc canceled",
			err:      func(t *testing.T) error { return grpcErr(t, codes.Canceled, "bye", nil) },
			wantKind: model.ErrKindCanceled,
			wantMsg:  "canceled",
		},
		{
			name:     "unknown first line",
			err:      func(*testing.T) error { return errors.New("list: backend exploded\nstack trace") },
			wantKind: model.ErrKindUnknown,
			wantMsg:  "list: backend exploded",
		},
		{
			name:     "unknown trimmed",
			err:      func(*testing.T) error { return errors.New(strings.Repeat("x", 200)) },
			wantKind: model.ErrKindUnknown,
			wantMsg:  strings.Repeat("x", maxErrMessage-1) + "…",
		},
		{
			name: "token refresh invalid_grant (REST)",
			err: func(*testing.T) error {
				return &url.Error{Op: "Get", URL: "https://storage.googleapis.com/storage/v1/b", Err: tokenErr(400, `{"error": "invalid_grant", "error_description": "reauth related error (invalid_rapt)"}`)}
			},
			wantKind: model.ErrKindUnauthenticated,
			wantMsg:  "sign-in expired",
		},
		{
			name: "oauth2 retrieve error",
			err: func(*testing.T) error {
				return fmt.Errorf("token: %w", &oauth2.RetrieveError{Response: &http.Response{StatusCode: 401}, ErrorCode: "invalid_client"})
			},
			wantKind: model.ErrKindUnauthenticated,
			wantMsg:  "credentials rejected",
		},
		{
			name:     "token endpoint outage is not an auth failure",
			err:      func(*testing.T) error { return tokenErr(503, "unavailable") },
			wantKind: model.ErrKindUnknown,
			wantMsg:  "auth: cannot fetch token: 503",
		},
		{
			name: "grpc per-RPC creds failure",
			err: func(t *testing.T) error {
				return grpcErr(t, codes.Unauthenticated, `transport: per-RPC creds failed due to error: auth: "invalid_grant" "reauth related error (invalid_rapt)"`, nil)
			},
			wantKind: model.ErrKindUnauthenticated,
			wantMsg:  "sign-in expired",
		},
		{
			name: "googleapi 401",
			err: func(*testing.T) error {
				return &googleapi.Error{Code: 401, Message: "Request had invalid authentication credentials."}
			},
			wantKind: model.ErrKindUnauthenticated,
			wantMsg:  "credentials rejected",
		},
		{
			name:     "googleapi 500",
			err:      func(*testing.T) error { return &googleapi.Error{Code: 500, Message: "internal"} },
			wantKind: model.ErrKindUnknown,
			wantMsg:  "googleapi: Error 500: internal",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			kind, msg := Classify(tc.err(t))
			if kind != tc.wantKind || msg != tc.wantMsg {
				t.Errorf("Classify = (%s, %q), want (%s, %q)", kind, msg, tc.wantKind, tc.wantMsg)
			}
		})
	}
}

// restrictedErr is a 403 from a ?alt=json Discovery REST client (such as
// BigQuery) when an organization policy (constraints/gcp.restrictServiceUsage)
// denies api in project. The body includes the REST v1 "errors" array beside
// "details", matching Google's live response via googleapi.CheckResponseWithBody
// and gensupport.WrapError.
func restrictedErr(t *testing.T, api, project string) error {
	t.Helper()
	msg := "Request is disallowed by organization's constraints/gcp.restrictServiceUsage constraint for 'projects/" + project +
		"' attempting to use service '" + api + "'."
	body := fmt.Sprintf(`{"error": {"code": 403, "message": %q, "errors": [{"message": %q, "domain": "global", "reason": "forbidden"}], "status": "PERMISSION_DENIED", "details": [{
		"@type": "type.googleapis.com/google.rpc.ErrorInfo",
		"reason": "RESOURCE_USAGE_RESTRICTION_VIOLATED",
		"domain": "googleapis.com",
		"metadata": {"consumer": "projects/%s", "service": %q}}]}}`, msg, msg, project, api)
	err := googleapi.CheckResponseWithBody(&http.Response{StatusCode: 403}, []byte(body))
	var ge *googleapi.Error
	if !errors.As(err, &ge) {
		t.Fatalf("expected googleapi.Error, got %T", err)
	}
	if ae, ok := apierror.ParseError(err, false); ok {
		ge.Wrap(ae)
	}
	return err
}

// tokenErr is a failed token fetch as cloud.google.com/go/auth reports it.
func tokenErr(status int, body string) *auth.Error {
	return &auth.Error{Response: &http.Response{StatusCode: status}, Body: []byte(body)}
}

func TestClassifyNil(t *testing.T) {
	t.Parallel()
	if kind, msg := Classify(nil); kind != "" || msg != "" {
		t.Fatalf("Classify(nil) = (%s, %q)", kind, msg)
	}
}

func TestProviderErrorKeepsClassification(t *testing.T) {
	t.Parallel()
	inner := model.ProviderError{Kind: model.ErrKindQuota, Message: "custom", Err: errors.New("x")}
	pe := providerError(model.ServiceGKE, fmt.Errorf("wrap: %w", inner))
	if pe.Kind != model.ErrKindQuota || pe.Message != "custom" || pe.Service != model.ServiceGKE {
		t.Fatalf("%+v", pe)
	}
}

// An API-disabled error carries the API and the project Google checked, by
// metadata or from the message text.
func TestProviderErrorDisabledProject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		err         func(t *testing.T) error
		wantAPI     string
		wantProject string
	}{
		{
			name: "metadata consumer by number",
			err: func(t *testing.T) error {
				return grpcErr(t, codes.PermissionDenied, "API disabled", &errdetails.ErrorInfo{
					Reason:   "SERVICE_DISABLED",
					Domain:   "googleapis.com",
					Metadata: map[string]string{"service": "iam.googleapis.com", "consumer": "projects/111122223333"},
				})
			},
			wantAPI: "iam.googleapis.com", wantProject: "111122223333",
		},
		{
			name: "message text",
			err: func(*testing.T) error {
				return &googleapi.Error{Code: 403, Message: "Kubernetes Engine API has not been used in project prj-a before or it is disabled. " +
					"Enable it by visiting https://console.developers.google.com/apis/api/container.googleapis.com/overview?project=prj-a then retry."}
			},
			wantAPI: "container.googleapis.com", wantProject: "prj-a",
		},
		{
			name:    "not api-disabled",
			err:     func(*testing.T) error { return &googleapi.Error{Code: 404, Message: "gone in project p"} },
			wantAPI: "", wantProject: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pe := providerError(model.ServiceGKE, tc.err(t))
			if pe.API != tc.wantAPI || pe.Project != tc.wantProject {
				t.Fatalf("API %q project %q, want %q %q", pe.API, pe.Project, tc.wantAPI, tc.wantProject)
			}
		})
	}
}
