package gcp

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"cloud.google.com/go/auth"
	"github.com/googleapis/gax-go/v2/apierror"
	"golang.org/x/oauth2"
	"google.golang.org/api/googleapi"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// maxErrMessage caps the fallback message for unclassified errors.
const maxErrMessage = 120

var (
	// quotedPermRE finds an IAM permission quoted in an error message, as in
	// "Required 'container.clusters.list' permission".
	quotedPermRE = regexp.MustCompile(`['"]([a-z]+\.[A-Za-z]+\.[A-Za-z]+)['"]`)
	// accessPermRE finds the unquoted form Cloud Storage uses: "does not have
	// storage.buckets.list access".
	accessPermRE = regexp.MustCompile(`does not have ([a-z]+\.[A-Za-z]+\.[A-Za-z]+) access`)
	apiNameRE    = regexp.MustCompile(`([a-z0-9-]+\.googleapis\.com)`)
	projectRE    = regexp.MustCompile(`(?:in project |project=|projects/)([a-z0-9-]+)`)
)

// errFacts is what Classify could read from the Google error types in a chain.
type errFacts struct {
	httpCode int
	grpcCode codes.Code
	hasGRPC  bool
	reasons  []string
	metadata map[string]string
	messages []string
	// tokenRejected is set when fetching an access token failed for a
	// reason other than a transient token-endpoint outage.
	tokenRejected bool
}

// Classify maps a provider error onto a model.ErrorKind and a short message
// such as "missing compute.instances.list". It never makes a network call.
func Classify(err error) (model.ErrorKind, string) {
	if err == nil {
		return "", ""
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return model.ErrKindTimeout, model.ErrKindTimeout.Label()
	case errors.Is(err, context.Canceled):
		return model.ErrKindCanceled, model.ErrKindCanceled.Label()
	}

	f := facts(err)
	text := errText(f, err)
	switch {
	case f.tokenRejected || f.httpCode == http.StatusUnauthorized || f.grpc(codes.Unauthenticated):
		return model.ErrKindUnauthenticated, unauthenticatedMessage(text)
	case f.hasReason("accessNotConfigured", "SERVICE_DISABLED") ||
		(f.httpCode == http.StatusForbidden && (strings.Contains(text, "has not been used") || strings.Contains(text, "is disabled"))):
		return model.ErrKindAPIDisabled, apiDisabledMessage(f, text)
	case restricted(f, text):
		// An organization policy (constraints/gcp.restrictServiceUsage)
		// denies the API in the project: as unusable there as a disabled
		// one, and no IAM grant changes that.
		return model.ErrKindAPIDisabled, restrictedMessage(f, text)
	case f.hasReason("rateLimitExceeded", "userRateLimitExceeded", "RATE_LIMIT_EXCEEDED"):
		// Compute and Cloud Storage report rate limits as 403.
		return model.ErrKindQuota, model.ErrKindQuota.Label()
	case f.hasReason("BILLING_DISABLED"):
		return model.ErrKindUnknown, "billing disabled"
	case f.httpCode == http.StatusForbidden || f.grpc(codes.PermissionDenied) || f.hasReason("IAM_PERMISSION_DENIED"):
		if p := permission(f, text); p != "" {
			return model.ErrKindPermissionDenied, "missing " + p
		}
		return model.ErrKindPermissionDenied, model.ErrKindPermissionDenied.Label()
	case f.httpCode == http.StatusNotFound || f.grpc(codes.NotFound):
		return model.ErrKindNotFound, model.ErrKindNotFound.Label()
	case f.httpCode == http.StatusTooManyRequests || f.grpc(codes.ResourceExhausted):
		return model.ErrKindQuota, model.ErrKindQuota.Label()
	case f.grpc(codes.DeadlineExceeded):
		return model.ErrKindTimeout, model.ErrKindTimeout.Label()
	case f.grpc(codes.Canceled):
		return model.ErrKindCanceled, model.ErrKindCanceled.Label()
	}
	return model.ErrKindUnknown, firstLine(err.Error(), maxErrMessage)
}

// providerError wraps err for svc with its classification. An error that is
// already a classified ProviderError keeps its kind and message.
func providerError(svc model.ServiceType, err error) model.ProviderError {
	var pe model.ProviderError
	if errors.As(err, &pe) && pe.Kind != "" {
		if pe.Service == "" {
			pe.Service = svc
		}
		return pe
	}
	kind, msg := Classify(err)
	pe = model.ProviderError{Service: svc, Kind: kind, Message: msg, Err: err}
	if kind == model.ErrKindAPIDisabled {
		f := facts(err)
		pe.API, pe.Project = disabledService(f, errText(f, err))
	}
	return pe
}

func facts(err error) errFacts {
	f := errFacts{metadata: map[string]string{}}
	var ae *apierror.APIError
	if errors.As(err, &ae) {
		if c := ae.HTTPCode(); c > 0 {
			f.httpCode = c
		}
		if st := ae.GRPCStatus(); st != nil {
			f.hasGRPC, f.grpcCode = true, st.Code()
		}
		f.addInfo(ae.Details().ErrorInfo)
		f.messages = append(f.messages, ae.Message())
	}
	// REST clients surface a failed token fetch as an *auth.Error, sometimes
	// wrapping an *oauth2.RetrieveError. gRPC clients turn it into
	// codes.Unauthenticated, which the grpc case below reads.
	var te *auth.Error
	if errors.As(err, &te) {
		f.addTokenFailure(te.Response)
	}
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		f.addTokenFailure(re.Response)
	}
	var ge *googleapi.Error
	if errors.As(err, &ge) {
		f.httpCode = ge.Code
		f.messages = append(f.messages, ge.Message)
		for _, item := range ge.Errors {
			f.reasons = append(f.reasons, item.Reason)
			f.messages = append(f.messages, item.Message)
		}
		f.addJSONDetails(ge.Details)
	}
	if st, ok := status.FromError(err); ok && st.Code() != codes.OK {
		f.hasGRPC, f.grpcCode = true, st.Code()
		f.messages = append(f.messages, st.Message())
		for _, d := range st.Details() {
			if info, ok := d.(*errdetails.ErrorInfo); ok {
				f.addInfo(info)
			}
		}
	}
	return f
}

// addTokenFailure records a failed token fetch. A 5xx, 408, or 429 from the
// token endpoint is an outage, not rejected credentials.
func (f *errFacts) addTokenFailure(resp *http.Response) {
	if resp != nil {
		switch c := resp.StatusCode; {
		case c >= 500, c == http.StatusRequestTimeout, c == http.StatusTooManyRequests:
			return
		}
	}
	f.tokenRejected = true
}

func (f *errFacts) addInfo(info *errdetails.ErrorInfo) {
	if info == nil {
		return
	}
	f.reasons = append(f.reasons, info.GetReason())
	for k, v := range info.GetMetadata() {
		f.metadata[k] = v
	}
}

// addJSONDetails reads google.rpc.ErrorInfo entries from a googleapi.Error's
// decoded Details slice. gax's apierror.ParseError drops Details whenever a
// REST v1 error body also carries an "errors" array (strict protojson
// rejects the unknown field), which every ?alt=json Discovery client gets.
func (f *errFacts) addJSONDetails(details []any) {
	for _, raw := range details {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		t, _ := m["@type"].(string)
		if t != "type.googleapis.com/google.rpc.ErrorInfo" && !strings.HasSuffix(t, "/google.rpc.ErrorInfo") {
			continue
		}
		if r, ok := m["reason"].(string); ok && r != "" {
			f.reasons = append(f.reasons, r)
		}
		if md, ok := m["metadata"].(map[string]any); ok {
			for k, v := range md {
				if s, ok := v.(string); ok && f.metadata[k] == "" {
					f.metadata[k] = s
				}
			}
		}
	}
}

func (f errFacts) hasReason(reasons ...string) bool {
	for _, r := range f.reasons {
		for _, want := range reasons {
			if r == want {
				return true
			}
		}
	}
	return false
}

func (f errFacts) grpc(c codes.Code) bool { return f.hasGRPC && f.grpcCode == c }

func permission(f errFacts, text string) string {
	if p := f.metadata["permission"]; p != "" {
		return p
	}
	// A quoted API name such as 'bigquery.googleapis.com' has the same
	// shape as a permission.
	for _, m := range quotedPermRE.FindAllStringSubmatch(text, -1) {
		if !strings.HasSuffix(m[1], ".googleapis.com") {
			return m[1]
		}
	}
	if m := accessPermRE.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

// unauthenticatedMessage says whether the sign-in expired (the OAuth token
// endpoint's invalid_grant, or a reauthentication demand) or the credentials
// were otherwise rejected.
func unauthenticatedMessage(text string) string {
	for _, s := range []string{"invalid_grant", "invalid_rapt", "reauth"} {
		if strings.Contains(text, s) {
			return "sign-in expired"
		}
	}
	return model.ErrKindUnauthenticated.Label()
}

// errText is every message in err's chain, for the regexps to search.
func errText(f errFacts, err error) string {
	return strings.Join(append(f.messages, err.Error()), "\n")
}

func apiDisabledMessage(f errFacts, text string) string {
	api, project := disabledService(f, text)
	switch {
	case api == "":
		return model.ErrKindAPIDisabled.Label()
	case project == "":
		return "enable " + api
	}
	return "enable " + api + " in project " + project
}

// restricted reports whether err is an organization policy denying an
// API in a project.
func restricted(f errFacts, text string) bool {
	return f.hasReason("RESOURCE_USAGE_RESTRICTION_VIOLATED") ||
		(f.httpCode == http.StatusForbidden && strings.Contains(text, "constraints/gcp.restrictServiceUsage"))
}

func isRestrictedError(err error) bool {
	if err == nil {
		return false
	}
	f := facts(err)
	return restricted(f, errText(f, err))
}

func restrictedMessage(f errFacts, text string) string {
	api, project := disabledService(f, text)
	switch {
	case api == "":
		return "API blocked by organization policy"
	case project == "":
		return "organization policy blocks " + api
	}
	return "organization policy blocks " + api + " in project " + project
}

// disabledService reads the disabled API and the project Google checked
// from an API-disabled error, by id or number. Either may be "".
func disabledService(f errFacts, text string) (api, project string) {
	api = f.metadata["service"]
	if api == "" {
		if m := apiNameRE.FindStringSubmatch(text); m != nil {
			api = m[1]
		}
	}
	if api == "" {
		return "", ""
	}
	project = strings.TrimPrefix(f.metadata["consumer"], "projects/")
	if project == "" {
		if m := projectRE.FindStringSubmatch(text); m != nil {
			project = m[1]
		}
	}
	return api, project
}

// firstLine returns the first line of s, trimmed to n runes.
func firstLine(s string, n int) string {
	s, _, _ = strings.Cut(s, "\n")
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
