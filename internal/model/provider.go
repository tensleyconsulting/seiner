package model

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotFound marks a lookup of an id that does not exist.
var ErrNotFound = errors.New("resource not found")

// ResourceProvider is the GCP ↔ Resource boundary.
// Implementations live under internal/gcp; UI code never sees API types.
type ResourceProvider interface {
	Service() ServiceType
	List(ctx context.Context, scopeID string) ([]Resource, error)
}

// RelatedLister is an optional provider capability. Catalog.ListRelatedEach
// skips providers that do not implement it. truncated is true when the
// provider stopped at a limit and more items exist.
type RelatedLister interface {
	ListRelated(ctx context.Context, parent Resource, facetID string) (items []Resource, truncated bool, err error)
}

// EnabledLister is an optional provider capability. It reports enabled API
// names (storage.googleapis.com) for a project. The catalog uses the first
// EnabledLister among its providers to skip products whose API is disabled.
type EnabledLister interface {
	EnabledAPIs(ctx context.Context, project string) ([]string, error)
}

// PermissionDeclarer is an optional provider capability: the IAM permissions
// List needs on scopeID, for every call whose failure fails List. Calls that
// only add detail are left out. nil means List is not gated on scopeID,
// including scopes it does not serve. Names must be real permissions: one
// unknown name fails the whole check, which then gates nothing.
type PermissionDeclarer interface {
	ListPermissions(scopeID string) []string
}

// ProjectPermissions returns perms for a project scope and nil for any
// other: the ListPermissions of a provider that lists only in projects.
func ProjectPermissions(scopeID string, perms ...string) []string {
	if _, ok := ProjectID(scopeID); !ok {
		return nil
	}
	return perms
}

// PermissionTester reports which of perms the caller holds on scopeID (a
// full CRM name). The catalog checks every provider's ListPermissions in
// one call per scope and skips a provider missing any of them.
type PermissionTester interface {
	TestPermissions(ctx context.Context, scopeID string, perms []string) ([]string, error)
}

// Invalidator is an optional Catalog capability that drops cached list
// results under scopeID (a prefix match, covering the scope itself and any
// facets of resources within it). The app's refresh path calls it before
// reloading so a manual refresh bypasses the session cache.
type Invalidator interface {
	Invalidate(scopeID string)
}

// Catalog fans out across providers. A failure in one must not hide others.
type Catalog interface {
	// Hierarchy returns the CRM tree. A tree returned beside an error
	// (a ProviderError with Partial set) is usable but may be missing
	// ancestors; calling again retries.
	Hierarchy(ctx context.Context) (HierarchyNode, error)
	// ListEach calls fn once per provider as results arrive. fn may be
	// called from multiple goroutines concurrently and must be safe for
	// that. ListEach returns when every provider has reported or ctx is
	// done.
	ListEach(ctx context.Context, scopeID string, fn func(ProviderResult))
	// ListRelatedEach is ListEach for providers that implement RelatedLister.
	ListRelatedEach(ctx context.Context, parent Resource, facetID string, fn func(ProviderResult))
}

// ProviderResult is one provider's List outcome.
type ProviderResult struct {
	Service   ServiceType
	Resources []Resource
	Truncated bool  // set when the provider stopped before the last page
	Err       error // nil on success; a ProviderError when from gcp.Catalog
	// Missing lists the permissions the caller lacks for List. When set,
	// the provider was not called: Resources and Err are empty.
	Missing []string
}

// Denial is a service the explorer skipped in a scope because the caller
// lacks Missing, the permissions its listing needs.
type Denial struct {
	Service ServiceType
	Missing []string
}

// ErrorKind classifies a provider failure so the UI can explain it without
// inspecting Google error types.
type ErrorKind string

const (
	ErrKindUnknown          ErrorKind = "unknown"
	ErrKindUnauthenticated  ErrorKind = "unauthenticated"
	ErrKindPermissionDenied ErrorKind = "permission-denied"
	ErrKindAPIDisabled      ErrorKind = "api-disabled"
	ErrKindNotFound         ErrorKind = "not-found"
	ErrKindTimeout          ErrorKind = "timeout"
	ErrKindQuota            ErrorKind = "quota"
	ErrKindCanceled         ErrorKind = "canceled"
)

// Label is a short human-readable name for the kind.
func (k ErrorKind) Label() string {
	switch k {
	case ErrKindUnauthenticated:
		return "credentials rejected"
	case ErrKindPermissionDenied:
		return "permission denied"
	case ErrKindAPIDisabled:
		return "API disabled"
	case ErrKindNotFound:
		return "not found"
	case ErrKindTimeout:
		return "timed out"
	case ErrKindQuota:
		return "quota exceeded"
	case ErrKindCanceled:
		return "canceled"
	default:
		return "error"
	}
}

// ProviderError records a per-service failure without aborting the catalog.
// The catalog in internal/gcp sets Kind and Message; the UI only reads them.
type ProviderError struct {
	Service ServiceType
	Kind    ErrorKind
	Message string // short, human-readable, no prefix chain
	Err     error
	// API and Project are set on an ErrKindAPIDisabled error that names
	// them: the service Google found disabled and the project it checked,
	// by id or number.
	API, Project string
	// QuotaProject marks an ErrKindAPIDisabled error whose Project is the
	// caller's quota project: a setup problem shared by every scope, not
	// this scope's.
	QuotaProject bool
	// Partial marks an error that left the list incomplete: the provider's
	// items are kept beside it.
	Partial bool
}

// Partial wraps err, from a provider that still returns items, to say the
// items are incomplete rather than wrong. The catalog keeps them and
// reports err as a ProviderError with Partial set.
func Partial(err error) error {
	if err == nil {
		return nil
	}
	return partialError{err}
}

// IsPartial reports whether err was wrapped by Partial.
func IsPartial(err error) bool {
	var p partialError
	return errors.As(err, &p)
}

type partialError struct{ error }

func (e partialError) Unwrap() error { return e.error }

func (e ProviderError) Error() string {
	return fmt.Sprintf("%s: %s", e.Service.Label(), e.Summary())
}

func (e ProviderError) Unwrap() error { return e.Err }

// Summary is Message, or the wrapped error's text when Message is empty,
// prefixed "partial list: " when the items were kept beside it.
func (e ProviderError) Summary() string {
	if e.Partial {
		return "partial list: " + e.summary()
	}
	return e.summary()
}

func (e ProviderError) summary() string {
	if e.Message != "" {
		return cleanLine(e.Message)
	}
	if e.Err != nil {
		return cleanLine(e.Err.Error())
	}
	return e.Kind.Label()
}

// AsProviderError returns err as a ProviderError for svc. Errors that are not
// already a ProviderError become ErrKindUnknown with no Message.
func AsProviderError(svc ServiceType, err error) ProviderError {
	var pe ProviderError
	if errors.As(err, &pe) {
		if pe.Service == "" {
			pe.Service = svc
		}
		if pe.Kind == "" {
			pe.Kind = ErrKindUnknown
		}
		return pe
	}
	return ProviderError{Service: svc, Kind: ErrKindUnknown, Err: err}
}
