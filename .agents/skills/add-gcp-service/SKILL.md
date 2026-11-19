---
name: add-gcp-service
description: Add a Google Cloud service to Seiner end to end (ServiceSpec, fixtures, live adapter, registration, icon, tests). Use when introducing a new resource type or giving a fixture-only service a live adapter.
---

# Add a GCP service

Work through these steps in order. When you finish, the new service lists in
the fixture catalog and the live catalog, and `internal/app` and
`internal/ui` are unchanged.

Read `AGENTS.md` and `docs/architecture.md` §5 first. `internal/gcp/adapters/gcs` is the
reference adapter. It is older than the file layout below and keeps its `API`
interface in `provider.go`. New adapters split the files as shown here.

Placeholders used below: `<svc>` is the package name (`spanner`), `Thing` is
the top-level resource (`Instance`), `Child` is a facet child (`Database`),
and `ServiceThing` is the `model.ServiceType` constant.

## 0. Check what already exists

Before writing code, research what the product's API (preferring packages
already in `go.mod`, such as `google.golang.org/api/<svc>/v1`) can list and
read: whether `locations/-` wildcard listing works, tier or scope limits,
which resources expose `getIamPolicy`, and whether list responses carry raw
PEM/attestation payloads (`Resource.Copies`). Put the open decisions—which
resource types and facets to show, where they sit in the tree, and anything
the API cannot do directly—to the user and build only after they answer.

Some products already have a `ServiceSpec`, a fixture, and an SVG but no live
adapter (Cloud Run, Pub/Sub, Firestore, Cloud Functions). If yours
is one of them, skip to step 3. Leave the existing `ServiceType` string alone;
it is also the SVG filename and the Kitty image ID.

## 1. Model: one `ServiceSpec` entry

In `internal/model/service.go`, add a `ServiceType` constant. Use a lowercase
kebab-case value (`"cloud-spanner"`). In `internal/model/service_spec.go`,
add one entry to `specs`:

```go
{
    Type:       ServiceCloudSpanner,
    Label:      "Cloud Spanner",          // product name as the Console shows it
    Abbrev:     "SPN",                    // 2–3 capitals; search term
    Glyph:      "🔧",                     // text-mode icon; see below
    Collection: "Spanner Instances",      // plural noun the Console uses
    API:        "spanner.googleapis.com", // Service Usage name
    Gate:       true,                     // true for every product
    Grouped:    true,                     // true for every product
    Group:      "",                       // product node to nest under, or empty
    Console:    namedConsole("/spanner/instances/", "/details/databases"),
},
```

- `Glyph` is the text-mode icon: one emoji that fits the resource (a
  spanner is a wrench). Use one with default emoji presentation, a single
  rune that `lipgloss.Width` measures as 2, never a symbol that needs
  `U+FE0F` (`🛡️`, `⚙️`): terminals disagree on its width. Prefer Unicode 13
  or earlier. `TestTextGlyphsAreWide` checks the width. A glyph may repeat
  one in another product's tree, but not a sibling's.
- `Gate` is false only for products that list even when their API is off
  in the project: IAM (bindings come from Resource Manager; service
  accounts and custom roles are client-based). Those are listed in the
  `ungated` map in `service_spec_test.go`. Do not ungate a product without
  seeing it list in a project whose enabled services lack its API.
- `ClientBased` marks an API Google checks in the caller's quota project
  rather than the listed project. Set it, with `Gate: false`, only on
  evidence: list a project that lacks the API while billing a project
  that has it (`gcloud … --billing-project OTHER`). If that lists, and
  billing the listed project itself fails, the API is client-based. A product whose adapter calls its
  own API is gated even if the Console shows it everywhere: Org Policy
  calls `orgpolicy.googleapis.com`, which each project must enable.
- `Group` nests the collection row under a product node when a product has
  several resource types. Leave it empty for a product with one resource
  type. The group must be a registered, non-synthetic spec. The Console's
  split of the shared Compute API, and the other multi-type products, map to:

  | Group | Collections |
  |-------|-------------|
  | `ServiceComputeEngine` | VMs, disks, templates, instance groups |
  | `ServiceVPC` | Networks, IP addresses, routes |
  | `ServiceNetworkServices` | Load balancing, Cloud DNS |
  | `ServicePSC` | PSC endpoints, service attachments, regional endpoints, service connection policies |
  | `ServiceNetworkSecurity` | Cloud Armor, SSL policies, TLS inspection policies, IAP-secured apps |
  | `ServiceCertificateManager` | Certificates, certificate maps, DNS authorizations, issuance configs, trust configs |
  | `ServiceSecureWebProxy` | Web proxies, SWP policies, URL lists |
  | `ServiceNetworkConnectivity` | Cloud Router (later VPN, Interconnect) |
  | `ServiceMemorystore` | Redis, Valkey |
  | `ServiceBigQuery` | Datasets, reservations, capacity commitments, data transfers, connections |
  | `ServicePrivateCA` | CA pools, certificate templates |
  | `ServiceLogging` | Log sinks, log buckets, log-based metrics |
  | `ServiceACM` | Access bindings (organizations only) |
  | `ServiceVPCSC` | Access policies (organizations only), service perimeters |
  | `ServiceWorkspace` | Users, groups (organizations only) |

  `ServicePSC` is a `GroupOnly` product nested under Network Services,
  `ServiceSecureWebProxy` and `ServiceCertificateManager` ones nested
  under Network Security, and
  `ServiceVPCSC` one nested under Access Context Manager. A
  product that is only a node (IAM, Network Services, Network Connectivity)
  sets `GroupOnly` and is not `Grouped`. Give a product node's `Console` func
  an `r.IsProductGroup()` case that opens the product's list page (see
  `vpcConsole`), or use `listConsole` when there is no per-resource page.
- `Console` builds the Cloud Console deep link. Reuse the helpers in
  `console.go`. `namedConsole(prefix, suffix)` produces
  `prefix + Name + suffix`, and `locatedConsole(prefix, suffix)` produces
  `prefix + Location/Name + suffix`. Both add `?project=`. Write a custom
  func only if neither fits (see `storageConsole`, `computeConsole`).
- Order matters only when several specs share an `API`. `ServiceForAPI`
  returns the first spec in table order. Put a new spec after the existing
  owner of a shared API (Compute Engine owns `compute.googleapis.com`).
- Nothing else in `internal/model` needs a per-service case. Label, abbrev,
  collection row, scope/grouping, gating, and console URL all read
  this table (except `iamConsole`, if a resource's `FacetPermissions` bindings
  should open that product's Console page rather than `/iam-admin/iam`).

`TestSpecsComplete` fails if any field is missing, if `Gate` or `Grouped`
does not match the expected values, or if the console URL falls back to the
project dashboard. Add a case to `internal/model/console_test.go` for the new
deep link.

## 2. Fixtures

In `internal/gcp/fake/fixtures.go` `Resources()`:

- Add at least one resource under `projects/demo-project`. Give it a full
  relative resource name as its `ID`
  (`projects/demo-project/instances/main-spanner`) and
  `ScopeID: "projects/demo-project"`. The `ID` scope must equal `ScopeID`.
- Fill in what the live mapper will set: `Location`, `Status`, `State`,
  `Created`, and `Labels`, using the `extra(...)` helper, plus a few
  `Metadata` keys.
- If the resource declares facets, add a small constructor like `bucket` or
  `vpc` that sets `Facets`. Add at least one child per facet with
  `ParentID` = the parent `ID` and `FacetID` = the facet id (see `gcsChild`).

Add the API to `enabledAPIs` in `internal/gcp/fake/provider.go`. Otherwise the
fixture catalog gates the rows away. `fake.Providers()` builds one `Static`
provider per `KnownServices()` entry, so `FakeCatalog` picks up the fixture
with no further registration.

These tests guard fixtures: `TestFixturesCoverEveryGroupedService`,
`TestFixtureFacetsHaveChildren`, `TestFixtureResourcesValidateAndAreUnique`,
`TestFixtureScopesAreCRMNames` (in `internal/gcp/fake/fixtures_test.go`), and
`TestFakeCatalogGatingKeepsFixtures` (in `internal/gcp`).

If the service needs a facet id that does not exist yet, add a `Facet*`
constant in `internal/model/facet.go`. Facets are data on `Resource`. The UI
renders whatever facets a resource declares.

## 3. Adapter package `internal/gcp/adapters/<svc>/`

Create exactly these four files. Only `live.go` imports Google packages.

### `api.go`: Google-free boundary

```go
package <svc>

import (
    "context"
    "time"
)

// Thing is the Google-free slice of a <product> thing the adapter needs.
type Thing struct {
    Name     string // short name, not the full resource name
    Location string
    State    string // the API's own enum word, e.g. "READY"
    Created  time.Time
    Labels   map[string]string
    // ...only the fields the mapper uses
}

// Child is one item under a Thing's facet. Omit it if there are no facets.
type Child struct {
    Name string
}

// API is the <product> ↔ Resource boundary. Tests inject a fake; live wraps
// the Google client.
type API interface {
    ListThings(ctx context.Context, project string) ([]Thing, error)
    ListChildren(ctx context.Context, project, location, thing string) ([]Child, error) // only with facets
}
```

### `provider.go`: mapping to `model.Resource`

```go
package <svc>

import (
    "context"
    "errors"
    "maps"

    "github.com/tensleyconsulting/seiner/internal/model"
)

// Provider implements model.ResourceProvider for <product>.
type Provider struct{ api API }

func New(api API) Provider { return Provider{api: api} }

func (p Provider) Service() model.ServiceType { return model.ServiceThing }

func (p Provider) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
    if err := ctx.Err(); err != nil {
        return nil, err
    }
    project, ok := model.ProjectID(scopeID)
    if !ok {
        return nil, nil // org and folder scopes: not ours
    }
    if p.api == nil {
        return nil, errors.New("<svc>: no API")
    }
    things, err := p.api.ListThings(ctx, project)
    if err != nil {
        return nil, err // wrap with %w if you add context; never flatten
    }
    out := make([]model.Resource, 0, len(things))
    for _, t := range things {
        out = append(out, thingResource(project, t))
    }
    return out, nil
}

func thingResource(project string, t Thing) model.Resource {
    md := map[string]string{ /* display keys */ }
    return model.Resource{
        ID:       "projects/" + project + "/locations/" + t.Location + "/things/" + t.Name,
        Name:     t.Name,
        Service:  model.ServiceThing,
        ScopeID:  "projects/" + project, // always the full CRM name
        Location: t.Location,
        Status:   thingStatus(t.State),
        State:    t.State,
        Created:  t.Created,
        Labels:   maps.Clone(t.Labels),
        Metadata: md,
        Fields:   model.FieldsOf(md, "most-important-key", "next-key"),
        Facets:   []model.Facet{{ID: model.FacetChildren, Label: "Children"}}, // add the Facet* constant
    }
}

// thingStatus maps the API's state word onto the coarse UI bucket.
func thingStatus(s string) model.ResourceStatus { /* switch */ }

// ListPermissions is what List needs: every permission whose absence makes
// List fail. Use the permission of the API method List calls (an
// aggregatedList needs the plain .list permission).
func (p Provider) ListPermissions(scopeID string) []string {
    return model.ProjectPermissions(scopeID, "<svc>.things.list")
}

var (
    _ model.ResourceProvider = Provider{}
    _ model.PermissionDeclarer = Provider{}
    _ model.RelatedLister    = Provider{} // only if facets
)
```

Rules for `provider.go`:

- **Scope.** `List` receives a full CRM name (`projects/p`, `folders/2`,
  `organizations/1`). Read it with `model.ProjectID` or `model.ScopeKind`.
  Return `nil, nil` for scopes the API does not serve. Set every `ScopeID` to
  the full name (`"projects/" + project`), never a bare id.
- **IDs.** `ID` is the full relative resource name the API uses. Escape
  user-controlled path segments (`url.PathEscape`), as GCS does for object
  names. Parse ids with `model.ParseName`. It skips `locations|regions|zones`,
  `global`, and `iam`, and returns `Project`, `Location`, `Kind`, `Name`, and
  `Rest`.
- **Fields.** Put the API's state word in `State` and a coarse `Status`
  (`StatusHealthy`, `StatusWarning`, `StatusError`, `StatusUnknown`) next to
  it. Put the creation time in `Created` and GCP labels in `Labels`, not
  `Metadata`. Use `Fields: model.FieldsOf(md, ...)` to order the details pane.
  Otherwise `Normalize` sorts `Metadata` by key. `ID`, `Name`, and `Service`
  are required. The catalog drops resources that fail `Validate` and reports
  them as a provider error. Put raw values the user copies rather than reads
  in details (PEM certificates/chains, CRLs, HSM attestations) in
  `Resource.Copies` so they stay out of JSON, search, and logs.
- **Errors.** Return the Google error unchanged or wrapped with `%w`. The
  catalog runs every error through `gcp.Classify`, which reads
  `*apierror.APIError`, `*googleapi.Error`, and gRPC status to produce an
  `ErrorKind` and an actionable message (`missing spanner.instances.list`,
  `enable spanner.googleapis.com in project p`). Formatting the error into a
  new string loses that information.
- **Permissions.** Implement `ListPermissions` (`model.PermissionDeclarer`).
  The catalog checks every provider's permissions in one
  `testIamPermissions` call per scope and skips a provider missing one, so
  the user sees "no access" instead of a 403. List the permission of every
  call whose failure fails `List`; leave out calls that only add detail and
  log on failure. Return nil for scopes `List` does not serve
  (`model.ProjectPermissions` does this for project-only adapters). Check
  each name against Google's permissions reference: a wrong but valid name
  hides data the user can read, and an unknown name fails the whole check.
  `TestLiveProvidersDeclarePermissions` fails if a live provider has none.
- **Facets are optional** (`model.RelatedLister`). The catalog calls
  `ListRelated` on *every* related-lister for *every* facet expansion, so
  return `nil, false, nil` unless `parent.Service` is yours and `facetID` is
  one you declared. Children get `ParentID = parent.ID`,
  `FacetID = facetID`, and the parent's `ScopeID`. A child row may use
  another service's type (GCS bucket permissions are `ServiceIAMBinding`
  rows built with `model.BindingResource`). Set `Inline: true` on a
  resource's sole facet when a facet row would add nothing (a repository
  whose only content is its images); the children then list directly under
  the resource. If you stop at a limit, return `truncated = true`; the
  navigation pane then shows a "more not shown" row.
- **Several resource types, one API.** Give each `ServiceType` its own small
  provider struct that shares one `API` value. See `internal/gcp/adapters/compute`:
  `Networks`, `Subnets`, `Firewalls`, and `Instances` all wrap one
  `compute.API`.
- **Regional APIs.** If the API has no aggregated list and needs one call per
  region, first check whether its `List` accepts `projects/{p}/locations/-`
  (as Memorystore and Private CA do): if so, make one paginated wildcard call,
  log `resp.Unreachable` with `slog.Debug`, and declare only the resource's
  `.list` permission. Otherwise use `fanout.Regions` (`internal/gcp/fanout`)
  with a
  `regions.Lister` (`internal/gcp/regions`). Do not write your own goroutine
  pool. The catalog throws away all of a provider's items when `List`
  returns an error. If only some locations fail, log those failures with
  `slog` and return the items that did load. Return an error only when
  every location fails, and wrap the first `LocationError` with `%w` so
  `Classify` can still read the Google error.

### `live.go`: the only Google import

```go
package <svc>

import (
    "context"

    // the product's Google client package
    "google.golang.org/api/iterator"
    "google.golang.org/api/option"

    "github.com/tensleyconsulting/seiner/internal/gcp/lazy"
)

type liveAPI struct{ client *<client type> }

// NewLazy defers client construction to the first call, so a slow or failing
// constructor cannot block startup; the failure surfaces as this service's
// ProviderError instead.
func NewLazy(opts ...option.ClientOption) Provider {
    l := lazy.New(func(ctx context.Context) (*<client type>, error) {
        return <newClient>(ctx, opts...) // lazy.Value already strips cancellation
    })
    return New(lazyAPI{l: l})
}

type lazyAPI struct{ l *lazy.Value[*<client type>] }

func (a lazyAPI) ListThings(ctx context.Context, project string) ([]Thing, error) {
    c, err := a.l.Get(ctx)
    if err != nil {
        return nil, err
    }
    return liveAPI{client: c}.ListThings(ctx, project)
}
// ...one forwarding method per API method

func (a liveAPI) ListThings(ctx context.Context, project string) ([]Thing, error) {
    it := a.client.ListThings(ctx, &<req>{Parent: "projects/" + project})
    var out []Thing
    for {
        t, err := it.Next()
        if errors.Is(err, iterator.Done) {
            return out, nil
        }
        if err != nil {
            return nil, err
        }
        out = append(out, fromThing(t))
    }
}

// fromThing maps the Google type onto the api.go struct. Convert timestamps
// (RFC 3339 strings or *timestamppb.Timestamp) to time.Time here; clone maps.
func fromThing(t *<google type>) Thing { /* ... */ }

var _ API = lazyAPI{}
```

For REST clients (`google.golang.org/api/<svc>/v1`), page with `.Pages(ctx, fn)`
instead of an iterator. See `compute/live.go`.

Dependencies: prefer a client package that is already in `go.mod`. After
adding one, run `go mod tidy` and commit `go.mod` and `go.sum`.

### `provider_test.go`: no credentials

```go
type fakeAPI struct {
    things   []Thing
    children map[string][]Child
    err      error // returned from every method when set
}

func (f fakeAPI) ListThings(context.Context, string) ([]Thing, error) {
    if f.err != nil {
        return nil, f.err
    }
    return f.things, nil
}
// ...
```

Write table-driven tests for:

- `List` scope handling: `projects/p` lists; `folders/1`,
  `organizations/1`, and `""` return `nil, nil` without calling the API.
- Mapping: `ID`, `Name`, `Service`, `ScopeID` (`projects/p`), `Location`,
  `State`, `Created`, `Labels`, `Fields` order, `Facets`.
- Status mapping: one row per state word, including unknown → `StatusUnknown`.
- Facets: children carry `ParentID` and `FacetID`. Another service's parent
  or an undeclared facet returns `nil, false, nil`. Truncation is set at the
  limit.
- Error propagation: `errors.Is(err, f.err)` holds after `List` and
  `ListRelated`.

`internal/gcp/adapters/gcs/provider_test.go` and `internal/gcp/adapters/compute/provider_test.go`
show the style.

## 4. Registration

Add the lazy provider to the slice in `liveProviders` in
`internal/gcp/open.go`. Keep the slice in `model.Specs()` order:

```go
<svc>.NewLazy(opts...),
```

If several providers share one API (step 3), have `NewLazy` return the lazy
`API` instead of a `Provider`, as `compute.NewLazy` does. Build it once and
pass it to each provider constructor, as `computeAPI` does. Do not add fake
providers to the live catalog; `FakeCatalog` already picks up the fixtures
from step 2.

## 5. Icon

Add `internal/ui/icons/assets/<service-type>.svg`, a 24×24 viewBox named
exactly after the `ServiceType` string. It is rasterized in memory at
startup. Do not commit PNGs. Draw original artwork in the category's palette
following `internal/ui/icons/assets/README.md`: plain `fill`/`stroke`
attributes only (no `<style>`, `class=`, `<image>`, masks, or clip paths),
both `rx` and `ry` on every rounded `<rect>`, and no exact half-circle arcs.
`TestEveryServiceHasSVG`, `TestEveryServiceSVGRasterizes`, and
`TestAssetConventions` check these rules. Do not add to `missingAssets`.

## 6. Verify

```bash
make check                          # test, vet, Google-import boundary
make race                           # goroutine tests with -race
go build -o seiner ./cmd/seiner
./seiner --demo --icons text       # expand demo-project; the new collection row appears
```

In the TUI, expand `demo-project` and confirm the collection row (`Spanner
Instances`) appears with your fixture under it. Select it to check the
details pane field order, then expand each facet. `--debug` writes a log to
`$XDG_STATE_HOME/seiner/seiner.log` (`~/.local/state/seiner/seiner.log` when
unset) and prints its path on stderr. Never print to stdout.

## 7. Do not

- Do not touch `internal/app` or `internal/ui`. If the new service seems to
  need a UI change, the abstraction leaked. Fix the model (`ServiceSpec`,
  `Resource` fields, facets) instead.
- Do not import Google packages outside `live.go`, and never from
  `internal/app` or `internal/ui`. `make boundary` checks this.
- Do not call a live API in tests, and do not require ADC.
- Do not hard-code console URLs, labels, or abbreviations outside the spec.
- Do not put Kitty escape sequences or terminal-specific data in `Resource`.
- Do not rename existing `ServiceType` values.
- Do not add `Create`/`Update`/`Delete`. Adapters are read-only.
