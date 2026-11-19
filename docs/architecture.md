# Seiner Architecture

A polished, information-dense, keyboard-driven terminal UI for browsing Google
Cloud resources. It runs on Linux and macOS, locally or on a remote host over
SSH. The main target is a remote Linux host over SSH, displaying in a modern
local terminal such as Ghostty.

This document is the design reference.

---

## 1. Purpose and constraints

**Use**

- Go
- Bubble Tea v2, Bubbles v2, Lip Gloss v2 (`charm.land/.../v2`)
- Official Google Cloud Go client libraries (not `gcloud` for resource retrieval)
- Kitty Graphics Protocol for full-color product icons when the terminal supports it

**Target environment**

```
Local machine
    Ghostty
       │
       │ SSH
       ▼
Remote Linux host
    seiner
```

The remote host does not need X11, Wayland, or a GUI. SSH transports terminal
bytes, including graphics escape sequences. Running `seiner` directly in the
local terminal, on Linux or macOS, works the same way without the SSH hop.

**Non-goals**

- Full Cloud Console replacement
- CRUD for any GCP service (adapters are read-only)
- IAM policy editor, billing dashboard, SQL editor
- Kubernetes shell, full log viewer, infra deployment
- Windows as a supported UI target
- Wrapping `gcloud` for resource retrieval

The product is a fast, **read-only resource browser** with useful details and
a few safe actions (copy name, ID, JSON, or Console URL).

---

## 2. Product concept

Conceptual screen (text icons, a bucket selected, messages shown):

```
[logo] Seiner v0.1.0                                          dev@example.com
[    ] example-org / prod-apps / demo-project
[    ] ID projects/demo-project/buckets/audit-logs
╭─ Resources ────────────────────╮╭─ Details ─────────────────────────────────╮
│   🏢 example-org                  ││ 🪣 audit-logs                             │
│   ▶ 🏫 Workspace               ││    Cloud Storage                          │
│   ▼ 🌐 Cloud Platform          ││ Audit log archive                         │
│     ▶ 🆔 IAM                   ││ ID            projects/demo-project/      │
│     ▼ 📁 prod-apps             ││               buckets/audit-logs          │
│       ▼ 💼 demo-project        ││ Location      us-east1                    │
│         ▶ 🆔 IAM               ││ storageClass  NEARLINE                    │
│         ▶ 🔛 Enabled Services  ││ Browse ────────────────────────────────── │
│         ▼ 🪣 Cloud Storage     ││   Permissions                             │
│ >         ▼ 🪣 audit-logs      ││   Objects                                 │
│             ▶ Permissions      ││ ───────────────────────────────────────── │
│             ▶ Objects          ││ o open console   c copy   enter select    │
│           ▶ 🪣 my-app-bucket   │╰───────────────────────────────────────────╯
│         ▶ 🚢 Kubernetes Engine │╭─ Messages (1) ────────────────────────────╮
│         ▶ 💻 Compute Engine    ││ 14:03:07 ! prod · Kubernetes Engine: miss │
│       ▶ 💼 checkout-project    ││            container.clusters.list        │
╰────────────────────────────────╯╰───────────────────────────────────────────╯
 project:demo-project  dev@example.com  live  icons:text  kitty:no   ? help • q quit
```

The rows between the header and the panes hold an auth banner or a notice
when there is one. The status bar shows project, account, catalog, icon
mode, Kitty support, and the last flash on the left, and key hints on the right.

Preserve these areas even if the visual design evolves:

| Area | Package | Content |
|------|---------|---------|
| Header band | `ui/header` | Three rows beside the 8×3 banner logo: `Seiner <version>` with the account right-aligned, then the selected node's breadcrumb, then `ID` and the selected resource's ID. On a row with no ID of its own (a facet, a collection, an object prefix) the ID line shows the nearest resource above it. The breadcrumb and ID are cut from the left so their ends stay visible. No background fill, so the Kitty logo keeps its colors. The account gives way first on narrow terminals. |
| Navigation pane | `ui/navigation` | Hierarchical resource outline |
| Details | `ui/details` | Selected node's fields and actions |
| Messages | `ui/messages` | Errors and warnings, under details; hidden until `m` or an error |
| Status bar | `ui/status` | Project, account, catalog (`live` / `demo`), icon mode, flash; key hints |

Organizations, folders, and projects are navigation pane nodes. Expanding a project
lists its resources; expanding an org or folder lists child CRM nodes and that
scope's own resources (IAM, org policy). The header breadcrumb follows the
selected CRM node.

---

## 3. Architectural principle

The most important design rule:

```
                SEMANTIC DATA
                     │
                     ▼
             normalized Resource
                     │
          ┌──────────┴──────────┐
          ▼                     ▼
       UI model             Icon identity
          │                     │
          ▼                     ▼
     Lip Gloss view       icons.Renderer
                                │
                   ┌────────────┴────────────┐
                   ▼                         ▼
             Kitty graphics             Text fallback
                   │                         │
                   ▼                         ▼
            Full-color                  Emoji glyph
            SVG artwork
```

The application thinks in terms of `"GKE cluster"`, never in terms of a
particular sequence of terminal escape codes.

Genuine boundaries (use interfaces here):

```
GCP API  ↔  normalized Resource
resource model  ↔  UI
UI  ↔  icons.Renderer
UI  ↔  terminal capabilities
```

Do not create interfaces for every small struct.

**Dependency rule:** `ui` / `app` must not import Google Cloud client libraries.
Terminal graphics code must not leak into resource models.

---

## 4. Repository layout

```
seiner/
├── cmd/seiner/main.go
├── internal/
│   ├── app/                 # top-level Bubble Tea model, layout, key routing
│   ├── config/              # flags, env, defaults
│   ├── log/                 # slog to a file (never stdout)
│   ├── model/               # Resource, ServiceType, ServiceSpec, filter
│   ├── gcp/                 # Catalog, ADC, Open, error classification; see below
│   ├── terminal/            # capability detection; clipboard is OSC 52 via Bubble Tea
│   └── ui/
│       ├── keys/            # centralized key map
│       ├── styles/          # Lip Gloss theme
│       ├── text/            # string helpers (display-width truncation)
│       ├── navigation/      # navigation pane
│       ├── resources/       # navigation pane rows
│       ├── details/
│       ├── search/
│       ├── status/
│       ├── header/          # brand, account, and breadcrumb band
│       ├── messages/        # scrollable error/warning log under details
│       ├── help/
│       ├── actions/         # copy overlay (name / ID / JSON / adapter Copies)
│       ├── splash/          # startup intro driven by provider results
│       └── icons/           # Renderer + text/noop/kitty; assets/*.svg
├── docs/                    # architecture.md, README images
├── .agents/skills/          # agent skills; .claude, .gemini, .opencode symlink here
├── AGENTS.md                # instructions for any coding agent
├── Makefile                 # build, test, vet, race, boundary, check
└── README.md
```

`internal/gcp/` subpackages:

| Kind | Packages |
|------|----------|
| Live adapters (under `adapters/`) | `acm`, `artifactregistry`, `bigquery`, `certmanager`, `clouddns`, `cloudsql`, `compute`, `crm`, `gcs`, `gke`, `iam`, `iap`, `kms`, `logging`, `memorystore`, `networkconnectivity`, `orgpolicy`, `privateca`, `secretmanager`, `serviceusage`, `swp`, `vpcsc`, `workspace`, `workstations` |
| Hierarchy enrichment (under `adapters/`) | `assuredworkloads` (marks Assured Workload folders) |
| Shared support | `cache` (TTL map), `display` (timestamps, self links), `fanout` (per-region calls), `lazy` (deferred client), `regions` (location lists) |
| Fixtures | `fake` |

Module path: `github.com/tensleyconsulting/seiner`. Binary: `seiner`.

---

## 5. Resource model

UI code must not understand raw Google API responses.

```go
type Resource struct {
    ID          string
    Name        string
    Service     ServiceType
    ScopeID     string
    Location    string
    Status      ResourceStatus
    Description string
    Metadata    map[string]string
    ParentID    string // product parent; empty = top-level in this scope
    Facets      []Facet
    FacetID     string
    State       string            // provider's own state word (RUNNING); "" if none
    Created     time.Time         // zero if unknown
    Labels      map[string]string // GCP labels, separate from Metadata
    Fields      []Field           // ordered display fields
    Lists       []List            // value lists shown in full (binding members)
    Badge       string            // short tag drawn at the row's end, colored by Status
    Origin      ServiceType       // node a row is inherited from (org/folder); icon at the row's end
    Copies      []Copy            // extra copy targets (HSM attestation); not in JSON or search
}
```

Each GCP adapter translates its API type into `model.Resource`.

`Status` is the coarse UI bucket; `State` keeps the provider's word so
`TERMINATED` and `STAGING` stay distinguishable. An adapter sets `Status`
only from something the API reports: a state or health field (`RUNNING`,
`READY`, `degraded`), or a problem it can see (a disabled firewall, an
expired secret, route warnings). Healthy is never a default; a resource
the API says nothing about stays Unknown, and the details pane leaves the
status row out (a bare `State`, like a role's launch stage, shows alone). `Metadata` is the lookup map
for internal keys (`MetaKind`, `MetaPrefix`, `MetaPath`) and search. `Fields`
is what the details pane shows, in order: an adapter that cares about order
sets it with `model.FieldsOf(md, "machineType", "internalIP", ...)` (named
keys first, the rest sorted). Otherwise `Normalize` fills it from `Metadata`
in sorted key order, skipping `MetaKind` and `MetaPrefix`. `Matches` searches
labels, state, and fields as well as metadata. `JSON` includes `state`,
`created` (RFC 3339), `labels`, and `fields`. `Resource` has no raw API
payload field. Adapters map everything they need into the fields above.

`Resource.ScopeID` and `HierarchyNode.ID` are always full CRM resource names:
`organizations/1`, `folders/2`, or `projects/p`, never a bare project id.
`Resource.ID` is the product's full relative resource name, and its scope
prefix equals `ScopeID` (`projects/p/zones/z/instances/vm` has `ScopeID`
`projects/p`). The fixture tests enforce this. Bare project ids exist only
at the CLI boundary (`--project`); `model.ProjectScope` converts them once in
`app.New` and `gcp.Open`. `model.ProjectID` and `model.ScopeKind` read a scope,
and `model.ParseName` splits a resource name (`projects/p/zones/z/instances/vm`)
into scope, project, location, collection, and leaf, so adapters and console
links do not hand-roll marker searches.

Everything the app knows about a product is declared once, in the
`ServiceSpec` table in `internal/model/service_spec.go`: label,
abbreviation, text-icon glyph, navigation pane collection label, Service Usage API name,
whether listing is gated on that API, whether it is grouped or a CRM scope,
the product node its collection sits under, and the Cloud Console deep-link
builder. `Label()`, `Abbrev()`, `Glyph()`,
`CollectionLabel()`, `IsScope()`, `ConsoleURL()`, and `ServiceForAPI()` all
read from it, and so do the catalog's API gating (`Gate`, `API`) and the
navigation pane's collection rows (`Grouped`, `Collection`, `Group`). Specs that share an
API resolve `ServiceForAPI` to the first in table order. `TestSpecsComplete`
fails if a spec is incomplete, `TestEveryServiceHasSVG` if its icon is
missing, and the fixture tests in `internal/gcp/fake` if a grouped service
has no demo-project fixture or a declared facet has no children.

To add a service, follow `.agents/skills/add-gcp-service/SKILL.md`. It
covers the spec entry, fixtures, the four-file adapter package, live
registration, the icon, and tests. A new service must not require changes to
`internal/app` or `internal/ui`.

CRM nodes (org / folder / project) are navigation pane rows (`ServiceOrganization`,
`ServiceFolder`, `ServiceProject`). `HierarchyNode` remains the CRM tree;
`HierarchyNode.Resource()` maps a node onto the UI model. Product resources
still nest under the project (or org/folder) that owns them.

A folder that is an Assured Workload carries `HierarchyNode.Workload`. After
the live tree is built, `assuredworkloads.Annotate` lists each org's
workloads at the `--aw-locations` locations (the API cannot list its
locations) and attaches each one to its `CONSUMER_FOLDER`. The folder row
gets an `AW` badge, a warning status when violations are active, and the
workload's compliance regime (data boundary), location, partner, sovereign
controls, KAJ state, violation counts, and keys project as fields. A failed
list is logged and leaves folders plain; it never fails the tree.

### 5.1 Service adapters

A provider is one `ServiceType`. The required interface is just listing.
Everything else is an optional capability that the catalog type-asserts for
(`internal/model/provider.go`):

```go
type ResourceProvider interface {
    Service() ServiceType
    List(ctx context.Context, scopeID string) ([]Resource, error)
}

type RelatedLister interface { // optional; facets
    ListRelated(ctx context.Context, parent Resource, facetID string) (items []Resource, truncated bool, err error)
}

type EnabledLister interface { // optional; Service Usage only
    EnabledAPIs(ctx context.Context, project string) ([]string, error)
}

type PermissionDeclarer interface { // optional; every live lister declares
    ListPermissions(scopeID string) []string
}
```

`ListPermissions` names every IAM permission whose absence makes `List`
fail on that scope (`model.ProjectPermissions` for project-only adapters)
and returns nil for scopes `List` does not serve. Calls that only add detail
and log on failure are left out. Names must be real permissions: one unknown
name fails the whole check, which then gates nothing.
`TestLiveProvidersDeclarePermissions` fails for a live provider without one.

`List` receives a full CRM scope name and returns `nil, nil` for scopes its
API does not serve. `ListRelated` is called on every `RelatedLister` for
every facet expansion. It returns `nil, false, nil` for parents of other
services and for facets it did not declare. Adapters are read-only. Do not
add CRUD.

Adapters format timestamps and read self links through
`internal/gcp/display` (`Stamp`, `RelLink`), so every details pane reads
alike.

Each live adapter under `internal/gcp/adapters/<svc>/` keeps Google types out of its
`API` interface and `provider.go` mappers. `live.go` is the only file that
imports a Google client, and `NewLazy` defers building that client to first
use through `internal/gcp/lazy`. A failed construction is kept for the
session (`sync.Once`): constructors make no network calls, so a failure is
a setup problem that retrying would not fix, and it surfaces as that
service's `ProviderError` on every load. `gcp.liveProviders`
(`internal/gcp/open.go`) lists the lazy providers. `gcp.FakeCatalog` builds one `fake.Static`
provider per service from the fixtures. The two are never mixed.

`gcp.Catalog` implements `model.Catalog` over the providers:

```go
type Catalog interface {
    Hierarchy(ctx context.Context) (HierarchyNode, error)
    ListEach(ctx context.Context, scopeID string, fn func(ProviderResult))
    ListRelatedEach(ctx context.Context, parent Resource, facetID string, fn func(ProviderResult))
}

type ProviderResult struct {
    Service   ServiceType
    Resources []Resource
    Truncated bool
    Err       error    // a ProviderError when from gcp.Catalog
    Missing   []string // permissions the caller lacks; the provider was not called
}
```

`ListEach` and `ListRelatedEach` run providers in
parallel and call `fn` once per provider as each finishes, concurrently, so
`fn` must be goroutine-safe. That includes empty and gated providers, so the
caller can tell when a load is complete. Before reporting,
the catalog runs `Normalize` and `Validate` on every item and drops invalid
items as an error. It wraps every provider error in a `model.ProviderError`
whose `Kind` is one of `ErrorKind` (`unauthenticated`, `permission-denied`,
`api-disabled`, `not-found`, `timeout`, `quota`, `canceled`, `unknown`) and whose `Message`
is short and actionable. `gcp.Classify` produces both from the Google error
chain, so adapters return Google errors unchanged or wrapped with `%w`. A
provider that returns an error contributes no items for that call, unless
it wraps the error with `model.Partial`: then its items are kept, the
`ProviderError` has `Partial` set and reads `partial list: …`, the UI logs
it as a warning, and the result is not cached. Load balancers use this
when one target proxy kind cannot be listed. Per-location adapters (KMS,
Artifact Registry, BigQuery reservations, commitments, transfers, and
connections, Certificate Manager, Secure Web Proxy, Workstations) and
wildcard-location lists (GKE, Memorystore, Network Connectivity, Private CA) ignore partial location or zone
failures, failing only when every location fails or the context ends. Gating,
timeouts, and caching are in §10.

Compute's lists are aggregated (one call covers every zone or region). Many
other APIs need a call per region instead. Adapters for those use
`internal/gcp/fanout.Regions` (bounded concurrency, ordered results, one error
per failed location) together with a `regions.Lister` from `internal/gcp/regions`
(live, Compute-backed, 24h cache; `regions.Static`/`regions.DefaultRegions` as
a fallback when Compute is disabled) rather than rolling a goroutine pool per
adapter.

Never block the Bubble Tea event loop on API calls. Use `tea.Cmd` and messages.
A failure in one provider must not crash the app or hide other providers.

### 5.2 Product containment

GCP has a second hierarchy that is not CRM: cluster → config → workstation,
GKE cluster → node pool, VPC → subnet, and similar parent/child links.

`Resource.ParentID` is that link (empty means top-level in this scope). Do not
build a general resource graph or edge store.

The navigation pane must **not** dump every nested object into the top-level view. Use
an **in-place outline** in the left pane (VS Code-style: triangle, icon, name;
indent children; no box-drawing tees or corners):

1. Default navigation pane: `ParentID == ""` only (VPCs, not every subnet). Items
   under a collection row are sorted by name (case-insensitive, stable), so
   order does not depend on API response order.
2. Enter on a parent with facets expands indented facet rows underneath
   (`Subnets`, `Firewalls`) without replacing siblings.
3. Enter on a facet loads children indented under that facet. Multiple parents
   can be expanded at once (two VPCs’ subnets visible, firewalls still collapsed).
4. Esc collapses the selected expansion. `/` searches what is loaded: while a
   query is set, collections, Enabled Services, and any collapsed CRM scope
   whose resources are already loaded open so their matches show. Search
   never starts a load.
5. GCS object names with `/` become folder nodes in the navigation pane. Enter expands
   a folder; objects show the basename.

The details pane shows the selected navigation pane node compactly. The hero icon
has the name and a subtitle beside it (the service label, or the owning
scope for a collection row). Below are the description and a two-column
key/value table: id, location, status (with `State` beside it; only when
the provider reports one), creation time, then `Fields` in order. Labels follow under a divider. Synthetic rows
(collections, folders) skip the id, location, and status they do not
really have; a collection shows how many resources it groups. Long values
wrap at spaces, commas, and slashes to at most four lines. Facets and a
one-line action bar are pinned to the bottom of the pane; when the fields
do not fit, they are clipped with `…` so the actions stay visible.

Click a row to select it; click the triangle to expand or collapse. Enter/Esc
still expand and collapse from the keyboard.

CRM nodes live in the same navigation pane as product resources. Enter/Esc expand
and collapse both.

Named **facets** on `Resource` (label + id) are outline sections, not a
replacing drill. Adapters implement optional `RelatedLister`. The UI does not
infer "Subnets" from VPC.

`RelatedLister.ListRelated` returns `(items, truncated, err)`. An adapter that
stops at a limit (GCS objects stop at 200) sets `truncated`; the catalog
carries it on `ProviderResult.Truncated`. The navigation pane then ends that facet's children with a
muted, non-selectable `… more not shown (limit N)` row (`RowNote`), where N is
how many items the truncating providers returned.

GCS buckets expose `Permissions` and `Objects` the same way. A
requester-pays bucket has no `Objects` facet, since reading them would bill
a project, and its `requesterPays` field says so. VPC networks
expose `Subnets` and `Firewalls`, and GKE clusters expose `Node Pools`.
A node pool expands inline to its `Nodes`, and a managed instance group
to its `Instances` (both below, under Compute instances).
Artifact Registry repositories expose `Permissions` (the bindings set on
the repository) and `Packages` (labelled `Images` for Docker). Non-Docker
packages expose `Versions` (`Inline`: a resource whose only facet is inline
draws no facet row, and Enter on the resource loads and lists the children
directly beneath it). Docker packages expose `Digests` and `Signature Tags`
(`.sig`-tagged cosign artifacts, showing `signedDigest` and no child facets),
and Docker digests expose `Signatures` (cosign signatures read from the OCI
v2 Referrers API and `.sig` tag manifests) and `Vulnerabilities` from
Artifact Analysis (Container Analysis occurrences).
Vulnerabilities come worst first (severity, then CVSS score, then CVE id):
the navigation pane keeps facet children in provider order, so the adapter sorts
them. A scan can list hundreds, so the navigation pane does not list them flat
under the digest: `model.GroupBySeverity` puts them under one collapsed
synthetic node per severity (`Critical`, `High`, `Medium`, `Low`,
`Minimal`, then `Other` for unknown severities; empty severities are left
out), and each opens on its own. A group node's badge is its count in the
severity's `Status` color (critical and high red, medium and low yellow,
minimal green); its details show the count and how many have a fix. Each
vulnerability still carries its severity as `Resource.Badge`. Esc on a
vulnerability closes its severity group; while searching, every group is
open so a match shows.

A list that can run to thousands, Workspace users, groups, and group
members, is laid out by `model.Buckets` instead of flat. A spec opts in
with `ServiceSpec.Bucketed`, in a collection or a facet alike, and
`ServiceSpec.FolderKey` names the `Metadata` key whose slash path files
each resource in a folder first (a user's org unit, `orgUnit`). A folder
lists its subfolders, then its own resources; the root's resources go in a
`/` folder when other folders sit beside them. Up to `model.BucketMax`
(50) resources at one level list flat. More go in one bucket per first
letter, and a letter with more than that in one per first two letters. A
level with a single bucket is skipped. A bucket node (`KindBucket`) is
synthetic, badged with its count, drawn with a folder icon
(`icons.GroupFolder`), and opens on its own. Esc closes an open
bucket, and on a closed one or a resource in one closes what holds it.
While searching, every bucket opens so a match shows.

A row has a triangle only while it may have something to show. A facet
(inline or not) that has not been loaded yet keeps its triangle, because
nothing is known until it is asked for; once its load finishes with no
items and no errors, the triangle goes away and Enter only flashes
`no <label>`. Children are loaded lazily. Object prefixes
are grouped in the navigation pane from loaded object names.

---

## 6. UI architecture

Use Bubble Tea's Model/Update/View. Avoid one enormous model. `app.Model`
owns the child models (`navigation`, `details`, `search`, `status`, `header`,
`messages`, `splash`; `help` and `actions` are overlays). It coordinates focus,
key routing, pane navigation, async loading, errors, and resize. Components
own local state.

Charm v2 specifics:

- Import `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`
- `View() tea.View` (not `string`); set `AltScreen` on the view
- Handle `tea.KeyPressMsg` (not v1 `tea.KeyMsg` struct)
- Centralize keys with `bubbles/v2/key`
- Theme colors live in `internal/ui/styles`; do not scatter ANSI colors

### 6.1 Layout

Adapt to terminal size via `tea.WindowSizeMsg`.

| Width | Mode | Visible |
|-------|------|---------|
| ≥ 80 | medium / large | Navigation \| Details |
| < 80 | small | Single focused pane |

Each pane's name is set into its top border (`╭─ Details ──`), so it costs
no row: the navigation pane is titled `Resources` (`navigation.Title`), then
`Details` and `Messages`. `styles.TitledPane` draws them all.

Drag the vertical border between the navigation and details panes to resize. Minimum
widths match the layout constants. The right column splits into details over
a messages pane, which starts at a few lines; drag the horizontal border
between them to resize. The messages pane starts hidden: `m` toggles it,
and an error message turns it on. It also stays hidden when the body is too
short for both. In small layout the right column shows when it has focus.
Keyboard (`h`/`l`) stays primary. Mouse tracking steals plain click-drag from
terminal selection (Shift-drag remains); Ghostty pane chrome is outside
the cell grid and keeps working.

Do not overflow horizontally without truncation.

### 6.2 Resource list row

```
ICON  NAME     SERVICE        LOCATION        STATUS
```

List rows are one line so the browser stays information-dense. List icons are
one cell tall (2 columns, approximately square). The details pane uses a 4×2
hero size. The logo is wider than tall (a 32×24 viewBox) and has its own
grids: 8×3 in the header and 24×9 in the startup intro.

The icon is visually separate from semantic text. Service type must remain
readable if graphics are disabled. Resource names stay ordinary terminal text
so copy/paste does not yield a private-use glyph or opaque image id.

### 6.3 Navigation pane

The navigation pane on the left is a hierarchical resource tree, not a category filter.

```
▼ [icon] demo-project
  ▶ [icon] IAM
  ▼ [icon] Cloud Storage
    ▼ [icon] my-app-bucket
      ▶ Permissions
      ▼ Objects
        ▶ [icon] logos
        ▶ [icon] data
          [icon] index.html
    ▶ [icon] audit-logs
  ▶ [icon] VPC Networks
```

Triangles sit to the left of the icon (`▶` collapsed, `▼` expanded, two spaces
for a leaf). Each child level indents by two spaces. Do not draw `├──` / `└──`
/ `│` tree lines.

The first two columns are the cursor: `> ` in text, and with Kitty graphics
the `cursor` icon (`assets/cursor.svg`, a ship seen from above, pointing
right) at list size.

Navigation does not call GCP APIs; it emits selection changes to the app model.
The navigation pane follows the current CRM scope: an organization or folder typically
shows IAM / Policy, not project-local VMs.

### 6.4 Keyboard

Defaults (centralized, not hard-coded in components):

```
↑ / k        Move up
↓ / j        Move down
← / h        Previous pane
→ / l        Next pane (navigation → details → messages)
g / G        Top / bottom
m            Show / hide the messages pane
Enter        Expand / collapse
Esc          Back / cancel
/            Search
r            Refresh
?            Help
c            Copy name / ID / JSON
o            Copy Cloud Console URL
d            Details pane
v            Switch view (Role Bindings: by role / by principal;
             Service Accounts: yours / all, including Google's)
q            Quit
Ctrl+C       Quit
```

In the navigation pane, Enter expands or collapses; Esc collapses the selected node.
Click a row to select; click the triangle to expand or collapse; click a
choice (`○ principal`) to select it. Drag the
pane border to resize. In the copy overlay, Enter copies the selected
field via OSC 52 (works over SSH); Esc cancels. After name, ID, and JSON
the overlay lists the resource's own `Copies`, which an adapter sets for
data too large or too raw for the details pane (an HSM attestation, a PEM
chain). `Copies` stay out of JSON and search, and the log records only the
target and byte count. `o` copies the console URL
rather than exec'ing a browser on the remote host. Esc from details or
messages returns to the navigation pane. With messages focused, ↑/↓ and g/G scroll;
the newest message stays in view unless scrolled back.

### 6.4.1 CRM in the navigation pane

The CRM tree is org → folder → project, shown as ordinary navigation pane rows.
When the top of the tree is an organization (or the root over several), its
row is pinned (`Row.Pinned`): always expanded, with no triangle, and Enter,
Esc, and clicks do not collapse it. Everything under it indents one level
less, so no columns go to a level that never closes.
Expanding a folder reveals child folders and projects plus that folder’s own
resources (IAM, org policy). Expanding a project reveals its product resources.
Under an org or folder, the node's own products all list before its child
projects, so they read as that scope's own, and its child folders list
last, together (`HierarchyNode.ChildrenFoldersLast`), so a project never
sits between two folders. The `IAM` node
comes first under every org, folder, and project, ahead of the other
products and Enabled Services (`ServiceSpec.First`, split out by
`model.SplitFirst`). `Organization
Policies` is also `First` and follows IAM (First products keep table
order). It lists only the policies set directly on that node
(`orgpolicy.Provider` calls the node's own `policies.list`), so the row
appears only on nodes that set a policy; inherited and effective policies
are not shown yet. A policy with allowed or denied values (a list
constraint) expands inline to one `Policy Value` row per value, badged
`ALLOW` or `DENY`, with the rule's condition and number when there are
several; the details pane clips long lists, the rows do not. Org Policy
has no cheaper read, so the expansion lists the node's policies again.
`Logging` (Log Sinks on every CRM node, Log Buckets and Log-based Metrics
on projects) is an ordinary product, so on an org or folder it lists after the
First products and before the child scopes.
Selecting a CRM node updates the details pane and the header breadcrumb.

### 6.5 Search

Local and responsive on already-loaded data. Match name, service, location,
labels, useful metadata. Do not hit the network on each keystroke.

### 6.6 Visual style

Dark- and light-terminal friendly, strong hierarchy, limited palette, colored status
indicators, subtle borders, clear selection. Icons prominent in the details pane, compact in the list.

Semantic colors (defined once in the theme): primary accent, selection,
success, warning, error, muted, heading.

The theme has a dark and a light palette with the same roles. `Init` asks
the terminal for its background (`tea.RequestBackgroundColor`); the reply
calls `styles.SetDark`, and the messages pane and the intro logo, which
keep rendered lines, render again. A terminal that does not answer keeps
the dark palette. Each palette's text colors meet WCAG AA contrast (4.5:1)
on its background, which a test checks. `NO_COLOR` turns colors off
through Bubble Tea's color profile detection, and picks text icons (§8).

---

## 7. Icons and terminal graphics

Do **not** use Nerd Fonts as the source of GCP product icons. Nerd Fonts are
fine for generic UI symbols (arrows, search, warnings).

The built-in artwork is original, drawn for Seiner in one style (see
`internal/ui/icons/assets/README.md`). Google's official icons are for
diagrams and documentation and are not redistributed. A user can load them,
or any other set, with `--icons-dir`: a file named `<service-type>.svg` or
`.png` there replaces that one built-in icon, and `--list-icons` prints the
names.

The rest of the application must not know how icons are rendered.

```go
type Renderer interface { // internal/ui/icons
    RenderAt(service model.ServiceType, size Size, row int) string
    Name() string
    Cols(size Size) int
    Rows(size Size) int
    Upload() string // Kitty transmit sequences, sent once at startup; "" otherwise
}
```

Implementations:

| Renderer | Behavior |
|----------|----------|
| `KittyRenderer` | Full-color embedded PNG via Kitty Graphics Protocol |
| `TextRenderer` | The spec's `Glyph`, one emoji (`🪣`, `🚢`, …) |
| `NoopRenderer` | Empty placeholder |

UI calls `RenderAt(ServiceCloudStorage, size, row)`, never raw Kitty sequences. It picks
a row's icon with `icons.For(resource)`: the resource's service, except a
folder (object prefix) in a bucket, which keeps `ServiceCloudStorage` but
draws `icons.BucketFolder` (`cloud-storage-folder.svg`, the bucket with a
folder tab; `📂` in text).

Text glyphs are emoji with default emoji presentation (`Emoji_Presentation`,
no `U+FE0F` variation selector), so every width table (terminal, tmux,
Lip Gloss) agrees they are two cells: the same 2×1 cell as a Kitty list
icon. Symbols such as `🛡` or `⚙` are one cell unless followed by `U+FE0F`,
and terminals disagree on that sequence's width, so they are not used. A
test checks each glyph is one rune two cells wide. Keep to emoji from
Unicode 13 or earlier so older fonts and width tables have them.

Ship assets with the binary (`go:embed`). Do not download icons at runtime.
Store SVG sources in git (text). Rasterize to PNG in memory at startup so
Kitty still receives PNG payloads. Do not commit rasterized binaries. Custom
icons from `--icons-dir` are read and rasterized the same way at startup; a
custom PNG is fitted into the same square.

Kitty graphics are graphical objects, not Unicode. Copy/paste of a row should
yield the resource name. Transmit each PNG once per display size (list 2×1 and hero 4×2) with
distinct image IDs; Kitty unicode placeholders bind one grid size per ID.
Do not resend identical PNG bytes for every row.

On quit the app frees what it uploaded: `Renderer.Delete` returns one
`a=d,d=I,i=<id>` per transmitted image (wrapped for tmux like the uploads),
sent with `tea.Raw` before `tea.Quit` while the alt screen is still active.
`Renderer.Reset` forgets what was sent so the next `Upload` sends every
image again (about 1 MB for the built-in set). The app re-uploads, after
`icons.UploadDelay` and once per burst of triggers, on the refresh key, and,
under tmux only (where a reattach can drop images), on `tea.FocusMsg`
(`View.ReportFocus` is on then; tmux needs `focus-events on`) and
`tea.WindowSizeMsg`.

Every image has a textual alternative. Never make an icon the only indication
of service type.

---

## 8. Terminal capabilities

```go
type Capabilities struct {
    Color         bool
    TrueColor     bool
    KittyGraphics bool
    Clipboard     bool
    NoColor       bool   // NO_COLOR set
    Multiplexer   string // "tmux", "screen", or ""
}
```

Do not rely exclusively on `$TERM`. Combine environment hints with conservative
defaults. If Kitty graphics are unavailable, fall back to `TextRenderer`.
The app must remain fully usable.

Herdr (`TERM_PROGRAM=herdr`, `HERDR_ENV=1`) parses Kitty graphics and Unicode
placeholders in its pane emulator while setting `TERM=xterm-256color` and
stripping outer terminal variables, so `Detect` sets `KittyGraphics` there.

Inside tmux or screen (`TMUX`, `STY`, `TERM_PROGRAM=tmux`, or a `tmux*` or
`screen*` `TERM`, which also covers SSH from inside a local multiplexer),
`KittyGraphics` is false even when Kitty, Ghostty, or Herdr hints are inherited, so
`auto` picks text. `--icons kitty` under tmux wraps each upload chunk in tmux's
passthrough sequence (`ansi.TmuxPassthrough`), which tmux forwards only with
`allow-passthrough on`. Placeholders are plain text and need no wrapping.

Kitty placeholders carry the image ID in a 24-bit foreground
(`38;2;r;g;b`), which any lower color profile would downsample into a
different ID. So whenever the Kitty renderer is active, `cmd/seiner` starts
the program with `tea.WithColorProfile(colorprofile.TrueColor)`
(`app.Model.ProgramOptions`; `colorProfile` is the one place the profile is
decided), even without `COLORTERM`. `NO_COLOR` (`Capabilities.NoColor`)
picks `TextRenderer`, for `auto` and `--icons kitty` alike, and logs why.
Under tmux the placeholders' truecolor must also survive tmux, which needs
the `RGB` terminal feature for the outer terminal.

Ghostty over SSH is the enhanced-mode target, not a hard dependency.

---

## 9. Authentication and project selection

- Application Default Credentials, resolved in `internal/gcp` (not in `app` / `ui`)
- Optional impersonation via `--impersonate` / `GOOGLE_IMPERSONATE_SERVICE_ACCOUNT`
- Explicit starting resource: `--initial-resource` or `initial-resource` in
  the config file, a resource name (`projects/ID`, `folders/ID`,
  `organizations/ID`). `--project`, `GOOGLE_CLOUD_PROJECT`, `GCP_PROJECT`,
  and `CLOUDSDK_CORE_PROJECT` set it to a project; the highest layer wins.
  A project there also sets `Config.Project` / `ProjectSet`. A folder or
  organization opens the live catalog without a project
  (`OpenOptions.InitialResource`). The navigation pane starts on
  `model.ScopeNode(cfg.InitialResource)` until the real tree arrives; if
  the tree lacks it, the first project is selected, as for a missing project.
- Show active account in the header and status bar
- ADC project is displayed as identity only; it must not silently replace the
  scope of the demo catalog
- Auth errors become UI state (banner), not a hard crash after the TUI starts
- Credentials Google rejects mid-session (HTTP 401, gRPC `Unauthenticated`,
  or a failed token refresh such as `invalid_grant`) classify as
  `unauthenticated`. A token endpoint 5xx, 408, or 429 is an outage, not a
  rejection. The first one raises a persistent banner and one message; later
  ones from other services are not repeated. The banner says how to fix
  the credentials (the gcloud command only for a user sign-in), then press
  `r`.
- Refresh (`r`) reads credentials again when there is no catalog or the
  live credentials were rejected: cmd hands the app a `Reopener` that runs
  `gcp.Open` as at startup (`app.WithReopen`; the demo has none). A new
  catalog replaces the old, which is closed, and every load restarts; the
  app never imports Google types for this.
- User ADC (`authorized_user`) carries no email, so `gcp.ResolveSession`
  reads it with one call to the OpenID `userinfo` endpoint (token in the
  `Authorization` header; gcloud's ADC login grants `userinfo.email`). Any
  failure keeps the `user (ADC)` label.
- OAuth scope: `cloud-platform` (what gcloud ADC grants). IAM still has to
  grant viewer-level roles; the TUI does not call mutating APIs. Google
  Workspace users and groups need a Workspace admin role on the
  principal instead (a service account can be assigned one in the Admin
  console); without one they list nothing, with no error. Domain-wide
  delegation is not supported
- With ADC and a project, cmd opens a live catalog (Cloud Storage first).
  `--demo` opens the fixture catalog instead and is the only way to reach
  it: missing credentials, no project, or a live catalog that fails to open
  return `gcp.SourceNone`, and cmd passes the app a nil catalog. The app
  then starts no loads and no intro; the banner or notice says why.
  When ADC works but no project is set, `model.Identity.Notice` says so and
  the TUI shows it as a muted line under the header (not an error banner).
  Do not mix live buckets with fake GKE/SQL rows.
- Missing credentials still start the TUI, with an empty navigation pane

`internal/app` consumes `model.Identity`. It must not import Google client libraries.

---

## 10. Async, cache, and errors

- All GCP I/O off the UI thread via Bubble Tea commands
- Loading states while fetching
- The CRM tree is never built on the startup path. `app.New` re-roots the
  navigation pane on a single-project stub (`projects/<id>`) and starts loading its
  resources immediately; `Init` also issues `loadHierarchyCmd`, bounded at
  30 s. `gcp.Catalog.Hierarchy` builds the tree lazily on first call through a
  shared, mutex-protected `*hierarchySource` (so value copies of `Catalog` do
  not each rebuild it) and caches the result; fixtures and the live stub are
  prebuilt and return immediately. When `hierarchyLoadedMsg` arrives, the app
  swaps in the real tree, keeps the current scope selected (`Find`, else
  `FirstProject`, else root), and starts loads only for path nodes that are
  not already loaded — the stub project's resources carry over because its
  node id is the same `projects/<id>` in both trees. Only a successful build
  is cached. If the tree fails to load, the navigation pane keeps the stub, the
  messages pane says so, and `r` retries the build along with its usual
  reloads.
- Partial success: one provider failing does not hide others
- Parallel fan-out: `Catalog.ListEach` / `ListRelatedEach` run up to 8
  providers at once (errgroup), each capped at 20 s, and report one
  `ProviderResult` per provider as it finishes — including empty results, so
  the UI can tell when a scope is complete. `List` / `ListRelated` are
  aggregating wrappers that keep provider order.
- The app streams those results through a channel: one `providerLoadedMsg`
  per provider, then `scopeLoadDoneMsg` (facets: `relatedLoadedMsg`,
  `relatedDoneMsg`). The navigation pane cache is keyed by scope or facet and then by
  reporting provider, so each arrival replaces only its own rows. A whole
  load is bounded at 60 s. "loaded N" flashes on completion only.
- Loads are tracked per `loadKey` (a scope, or a parent plus facet; the
  Enabled Services group is a facet load). `Update` creates each load's
  context and keeps at most one in flight per key: expanding a node that is
  already loading starts nothing. `r` cancels and restarts every loaded or
  loading scope and facet. Every message carries the load's generation, so
  results from a canceled load are dropped. Quitting cancels every load.
  The navigation pane marks a row whose load is in flight with a muted `…`.
- Service Usage gating: for a project scope, `ListEach` first asks the first
  `model.EnabledLister` provider (Service Usage) for the enabled API set and
  skips every provider whose spec has `Gate` and an `API` not in that set.
  Skipped providers still report an empty, error-free `ProviderResult`. Org
  and folder scopes are not gated. A failed lookup, or no `EnabledLister`,
  gates nothing (fail open, logged at warn). The Service Usage provider caches
  each project's enabled set for 5 min and shares in-flight calls
  (`cache.Flight`, which runs the shared call detached from any one
  caller's cancellation), so the Enabled Services node reads the same
  result without a second API call. `Catalog.Invalidate` on a project
  drops its enabled set, so `r` picks up a newly enabled API.
- Quota project: Google checks a resource-based API's enablement in the
  listed project, but a client-based API's in the caller's quota project
  (`GOOGLE_CLOUD_QUOTA_PROJECT`, else ADC's `quota_project_id`, else a
  service account's own project; `Session.quotaProject`). Google does not
  publish which APIs are which; `ServiceSpec.ClientBased` records the ones
  observed (IAM service accounts and custom roles), and those are never
  gated on the listed project. When `List` still fails with API disabled,
  the catalog reads the project the error names (`ProviderError.Project`,
  an id or a number matched through the built tree): the listed project
  lists nothing and reports no error, cached like a gated result; the
  quota project, or both when the API is client-based, is an error marked
  `QuotaProject`. The app reports that once per API as a warning naming
  the project and the fix, and does not mark scopes with it. Any other
  project stays an ordinary error.
- Permission gating: on the first cache miss in a `ListEach`, the catalog
  collects the `ListPermissions` of every provider not gated off by Service
  Usage and asks its `model.PermissionTester` once which the caller holds
  (Resource Manager `testIamPermissions` on the org, folder, or project; it
  needs no permission itself). A provider missing any is not called; it
  reports a `ProviderResult` with `Missing` set and no error, cached like
  any result, so `r` checks again. The scope's details pane shows a "No
  access" section naming the permissions; the navigation pane adds no row, and
  nothing goes to Messages. A failed check, or no tester, gates nothing (fail open, logged at warn). It is a hint, not a guarantee:
  VPC Service Controls or a condition on the resource can still refuse a
  call, which then reports as a provider error. The fixture catalog denies
  Cloud SQL and Secrets on `checkout-project`.
- Provider errors are typed: the catalog wraps every failure in a
  `model.ProviderError` built by `gcp.Classify`, which reads
  `*apierror.APIError`, `*googleapi.Error`, gRPC status, token-fetch errors
  (`*auth.Error`, `*oauth2.RetrieveError`), and context errors into an
  `ErrorKind` (unauthenticated, permission-denied, api-disabled, not-found,
  timeout, quota, canceled, unknown) and a short `Message`. Permission errors are
  actionable (`missing compute.instances.list`); disabled APIs read
  `enable container.googleapis.com in project p`. The UI reads only `Kind`
  and `Message` and never imports Google error types.
- The app keeps the latest errors per scope, facet, or group key (keyed like
  the navigation pane cache); a provider's next report replaces its entry. The
  navigation pane does not render errors as rows: the owning scope, facet, or group
  row gets a small `!` mark, and the details pane lists its errors under
  "Problems". Each error as it arrives is also logged to the messages pane
  (`<scope> · <collection>: <message>`, consecutive repeats counted). The
  completion flash names a single error or counts several
  (`3 provider errors; see messages`).
- Session cache: `internal/gcp/cache` is a small generic TTL map, mutex
  protected, expiring on read with no background goroutine. `gcp.Catalog`
  holds one as a `*cache.Cache[model.ProviderResult]` (a pointer, so value
  copies of `Catalog` share it) and wraps every provider call in
  `ListEach`/`ListRelatedEach`: key `scope|service` for `List`,
  `parent|facet|service` for `ListRelated`, 60s TTL. Only successful results
  are cached; a cache hit still reports through the same `fn` callback as a
  live `ProviderResult`. This is "avoid the network" and is separate from the
  navigation pane's `outline.cache`, which is "what is on screen" and never expires
  on its own.
- `model.Invalidator` is an optional `Catalog` capability
  (`Invalidate(scopeID string)`, a prefix delete). `gcp.Catalog` implements
  it. The app's refresh path (`r`) type-asserts for it and invalidates each
  scope it is about to reload before restarting the load, so refresh always
  bypasses the session cache; a plain re-expand within the TTL does not.
- Regional fan-out inside an adapter uses `fanout.Regions` (§5.1)
- `slog` to a file only (`--log-file`); never stdout

| Cache | TTL | Where |
|-------|-----|-------|
| Provider lists | 60 s | `gcp.Catalog` (`listCacheTTL`) |
| Compute list calls per project | 30 s | `compute.Memo` (shared, single-flight; dropped by `Invalidate` via `Catalog.WithInvalidators`) |
| Enabled APIs per project | 5 min | Service Usage provider |
| Role permissions by role name | 5 min | `iam.Roles` |
| Regions and zones | 24 h | `regions` |

Manual refresh (`r`) invalidates the current view's prefix.

---

## 11. Configuration

Precedence: flags > env > config file > defaults, where env is only Google's
project and impersonation variables; Seiner has no variables of its own.
The full flag, environment, and file key table is in the
[README](README.md#configuration);
`internal/config` is the source of truth.

The config file is YAML at `$XDG_CONFIG_HOME/seiner/config.yaml`
(`~/.config` when unset, on macOS too), or the path in `--config`. Its keys
are the long flag names. A missing default file is not an error; a missing
named file, an unknown key, or a bad value stops Seiner before the TUI
starts. Only a flag the user passed overrides the file (`flag.Visit`), so a
flag's default never hides a file value. `google-service-accounts` has no
flag; it sets where each Service Accounts row's toggle starts.

---

## 12. Testing

| Layer | Approach |
|-------|----------|
| Model / filter / keys / capabilities / icons | Table-driven unit tests |
| UI | Update() tests; golden frames where deterministic |
| GCP adapters | Fake `API`s for providers; wire tests for `live.go` against `internal/gcp/pagetest` (REST, an `httptest` server that serves pages and errors and records each request's query) or an in-process gRPC server (CRM, IAM); no live credentials for `go test ./...` |
| E2E | Optional, tagged, explicit credentials |

After each major stage:

```bash
go test ./...
go vet ./...
go build ./...
```

---

## 13. Navigation pane content by product

How the navigation pane groups and presents each product. The outline mechanics are
in §5.2.

**Live CRM tree.** Resource Manager `SearchProjects` plus ancestor folder/org
lookups populate the navigation pane. Ancestors are read a level at a time, up to 8
concurrently. A folder or org the principal cannot read becomes a stub named by
its number. Any other failed read (unavailable, quota, timeout) also leaves a
stub, but the tree comes back with a `model.Partial` error: the app shows it
with a "Resource hierarchy may be incomplete" warning, and the catalog does
not cache it, so `r` rebuilds it. If the build's context ends before every lookup has run, `crm.Build`
fails instead of returning a tree with unread folders as stray roots. Empty
folders the principal cannot see are omitted.
Permission errors on GetIamPolicy are provider errors, not a crash.

**Enabled APIs.** A project’s navigation pane children include an **Enabled Services**
node (Service Usage `state:ENABLED`, its APIs sorted by name) plus one
collection node per resource type
that actually has items (`Cloud Storage`, `Cloud Functions`, …). Some
collection and product nodes take the Console's navigation name instead of
the resource type's (`Cloud Storage`, `Kubernetes Engine`, `Cloud SQL`,
`VPC Networks`). Empty collections are
omitted. CRM folders and projects stay ungrouped.

Products with several resource types put their collections under one product
node instead of directly under the project. The Compute Engine API is split
into the products the Console shows. In navigation pane order (the demo catalog;
a collection appears only when it has items):

```
Organization
├─ Workspace                 Users (org unit folders, letter buckets) ·
│                            Groups (inline: members)
└─ Cloud Platform            (starts open)
   ├─ IAM · Organization Policies · Access Context Manager · Logging · …
   └─ folders and projects   (folders last)

Project
├─ IAM (first)               Role Bindings · Service Accounts (inline: keys) ·
│                            Custom Roles
├─ Organization Policies     (first; only where a policy is set; inline: values)
├─ Enabled Services
├─ Cloud Storage             buckets (facets: Permissions, Objects)
├─ Kubernetes Engine         clusters (facet: Node Pools → inline: nodes)
├─ Cloud SQL                 instances (facets: Databases, Users, Backups)
├─ Memorystore               Redis Instances · Valkey Instances
├─ BigQuery                  Datasets (facets: Tables, Routines, Models,
│                            Permissions) · Reservations (inline:
│                            assignments) · Capacity Commitments ·
│                            Data Transfers · Connections
├─ Cloud Run Services        (fixtures only)
├─ Pub/Sub Topics            (fixtures only)
├─ Firestore Databases       (fixtures only)
├─ Compute Engine            GCE Instances · Disks · Instance Templates ·
│                            Instance Groups (managed; inline: instances) ·
│                            Health Checks
├─ VPC Networks              VPC Networks (facets: Subnets, Firewalls, Routes) ·
│                            IP Addresses
├─ Network Services          Load Balancers (facets: Frontends, Backends,
│  │                         Health Checks) · Forwarding Rules ·
│  │                         DNS Zones (inline: record sets)
│  └─ Private Service Connect   Connected Endpoints · Published Services
├─ Network Connectivity      Cloud Routers (facet: NAT Gateways)
├─ Network Security          Cloud Armor Policies · SSL Policies ·
│  │                         TLS Inspection Policies · IAP-Secured Apps
│  ├─ Secure Web Proxy       Web Proxies · SWP Policies · URL Lists
│  └─ Certificate Manager    Certificates · Certificate Maps (inline: entries) ·
│                            DNS Authorizations · Issuance Configs · Trust Configs
├─ Access Context Manager
│  └─ VPC Service Controls   Service Perimeters (those protecting the project)
├─ Secrets                   (inline: versions)
├─ Key Rings                 (facets: Keys → Versions, Import Jobs, Permissions)
├─ Logging                   Log Sinks · Log Buckets (projects only; facets:
│                            Views, Linked Datasets) · Log-based Metrics
├─ Cloud Functions           (fixtures only)
├─ Cloud Workstations        clusters (inline: configurations →
│                            inline: workstations)
└─ Artifact Registry         repositories (facets: Permissions, Packages →
                             Digests / Signature Tags → Signatures,
                             Vulnerabilities)
```

Notes on the grouping:

- An organization's children are two nodes: `Workspace` (when it has
  Workspace resources) and `Cloud Platform`, which holds the organization's
  IAM, policies, products, folders, and projects, so that a reader who
  wants only Workspace can close Cloud Platform (it starts open).
  `Cloud Platform` is a `GroupOnly` node with no adapter. Folders and
  projects have no such split. When there is no single organization, the
  synthetic `GCP` root holds any organizations directly and puts the
  projects and folders outside them in a `Cloud Platform` node of its own
  (it also starts open), so the tree reads the same with or without an
  organization.

- `Cloud Workstations` is the cluster collection: clusters are its only
  top-level resource, so there are no `Clusters` or `Configurations` rows.
  The API has no all-locations list; `workstations.Provider` lists the
  API's locations (cached per session) and fans out with `fanout.Regions`.
  A workstation user may hold only `workstations.workstations.use` on their
  own workstation and no list permission. Where a list is denied (403),
  each level instead calls `listUsable` under its parent, with `-` for any
  cluster or configuration (gcloud `workstations list-usable` defaults to
  all of them), and builds the clusters and configurations from the
  workstations' names; those rows carry a `listing` field and no details.
  For that reason the provider declares no `ListPermissions`. Clusters and
  configurations are an error while `degraded` and a warning while
  `reconciling`; workstations are healthy when running, a warning while
  starting, stopping, or suspending, and have no status when stopped or
  suspended. Environment variables (workstation and container) are shown.

- The `VPC Networks` product node and its first collection share a name, as
  in the Console. `ServiceSpec.Node` names a product node when it differs from
  the singular `Label`.
- `Cloud Armor Policies`, `SSL Policies`, and `DNS Zones` (Cloud DNS) sit
  where the Console puts them, not under their API's product.
- `Network Security` follows the Console's Network Security menu. Its
  collections span three APIs: Compute (Cloud Armor, SSL policies), Network
  Security (TLS inspection policies, SWP policies, URL lists), and Network
  Services (web proxies). `swp` wraps both of the latter in one lazy API.
  Neither API has a documented all-locations list, so each provider lists
  its API's locations (cached per session) and fans out with
  `fanout.Regions`. The gateway list also returns Traffic Director
  (`OPEN_MESH`) gateways; only `SECURE_WEB_GATEWAY` ones are kept.
- `IAP-Secured Apps` are load balancer backend services with IAP on. The
  IAP API cannot list what it secures, so `iap.Apps` reads the backend
  services (aggregated Compute list), resolves the project number through
  Resource Manager (IAP names start with it; cached per session), and reads
  each app's own IAP settings, not inherited ones. An app's id mirrors its
  IAP name with the project id in place of the number
  (`projects/p/iap_web/compute-REGION/services/NAME`). Its `Access` facet
  lists its IAP policy as role binding rows, then what it inherits under
  one `From …` node per level, nearest first, as the IAM node does: IAP's
  own levels above it (`Backend Services`, regional or global, then `All
  Web Services`) in full, then the project, folders, and organization,
  whose policies are filtered to `roles/iap.*` web roles (tunnel roles and
  basic roles do not grant app access). It reuses `iam.Ancestors` and the
  IAM node's Resource Manager client. Only the app's own policy must load;
  an ancestor that fails is logged and skipped. OAuth client secrets are
  input-only in both APIs and are never copied.
- `Certificate Manager` (`certificatemanager.googleapis.com`) nests under
  Network Security like Secure Web Proxy. `certmanager` lists every type
  per location (the API's locations, cached per session, through
  `fanout.Regions`), since it has no documented all-locations list. A
  certificate's status is the worse of its managed state (`PROVISIONING`
  warning, `FAILED` error) and its expiry: expired is an error with an
  `EXPIRED` badge, and within 30 days a warning badged with the days
  left. A map expands inline to its entries, each named after its
  hostname (or `PRIMARY`). PEM bodies and private keys are never copied;
  trust configs show only certificate counts. Classic Compute SSL
  certificates are not listed. Every row links to the Certificate
  Manager list page.
- A web proxy and its SWP policy both expand inline to the policy's rules,
  so the same rule id appears under both, with `Row.Via` keeping each tree
  branch's row key unique. A rule whose `sessionMatcher` or
  `applicationMatcher` calls `inUrlList(...)` expands inline to the URL
  lists it references, and every URL list expands to its pattern entries so
  allowlisted hosts can be browsed in place without leaving the proxy's
  sub-tree.
- A PSC endpoint is a forwarding rule that targets a service attachment or a
  Google APIs bundle, or carries a PSC connection. `compute.PSCEndpoints`
  lists those and `compute.ForwardingRules` the rest, so a rule appears in
  one collection only.
- A NAT's id extends its router's (`.../routers/r/nats/n`) because the API
  keeps NAT gateways on the router.
- `Redis Instances` (`redis.googleapis.com`) and `Valkey Instances`
  (`memorystore.googleapis.com`) use separate clients, so `memorystore.Redis`
  and `memorystore.Valkey` each wrap their own lazy API.
- `Log Buckets` have a `Views` facet (`ServiceLogView`; every bucket has
  `_AllLogs`) and, when Log Analytics is on, `Linked Datasets`
  (`ServiceLogLink`, the BigQuery dataset each links, healthy while
  active). A bucket shows its CMEK key version, indexed fields, and
  restricted fields (lists). `Log-based Metrics` (`logging.Metrics`,
  projects only) show the filter, metric kind and value type, unit, value
  extractor, label extractors (a list), and the bucket a bucket-scoped
  metric counts from; a disabled metric is a warning badged `off`, as a
  disabled sink is.
- An organization policy with a dry-run spec is badged `DRY-RUN`; its
  `dryRun` field summarizes what the spec would enforce, as `enforcement`
  does for the live spec, and a `Dry-run rules` list spells out its rules
  when they need more than one line. A managed constraint's rule
  parameters show as compact JSON in the rule's line.
- Artifact Registry repositories spell out their cleanup policies
  (`id  ACTION  condition`) and a virtual repository's upstreams (by
  priority) as lists. A remote repository shows its source (a public
  preset or a custom URI) and its upstream credentials by username and the
  Secret Manager version that holds the password, which is never read.
  Docker packages split `.sig`-tagged cosign artifacts into `Signature Tags`
  and image digests into `Digests`. A digest's `Signatures` facet queries
  the registry's OCI Distribution v2 endpoint (`/v2/.../referrers/<digest>`
  with OCI 1.1 referrer-tag fallback, and `/v2/.../manifests/<tag>.sig`)
  for Sigstore bundles and simple-signing layers, exposing signer identity,
  OIDC issuer, Rekor metadata, and raw signature/PEM/bundle targets in
  `Resource.Copies`; Seiner does not cryptographically verify signatures.
- Compute instances read only the `enable-oslogin` key from instance
  metadata: other keys can hold startup scripts and secrets.
- On an org or folder, `Logging` lists before the child scopes, like every
  product there.
- `Access Context Manager` is organization-wide. On an organization it
  holds `Access Bindings`, then `VPC Service Controls` nested inside it,
  whose `Access Policies` collection lists the org-level policy and every
  scoped one (`SCOPED` badge, `scope` field). Each policy expands to
  `Perimeters` and `Access Levels` facets. On a project the node holds only
  VPC Service Controls. Access policies and bindings never list under a
  project, so they are the grouped services demoed from
  `organizations/example-org` rather than `demo-project`. An id puts the API's
  name under the scope it lists in
  (`organizations/O/accessPolicies/N/servicePerimeters/P`).
- `Google Workspace` is the directory of the customer that owns an
  organization (`organizations.get` `directoryCustomerId`), so it lists
  only on organizations. `workspace.Users` reads the Admin SDK Directory
  API `users.list`, and `workspace.Groups` reads Cloud Identity
  `groups.list` and, inline, a group's direct members
  (`memberships.list`). These are the reads that accept the
  `cloud-platform` scope, which is the only scope the clients request:
  org units, admin roles, domains, and devices each need an
  `admin.directory.*` scope, so they are not listed. A user's org unit
  comes with the user and files it in an org unit folder (§5.2). A user
  shows its org unit, admin status, 2-Step Verification, and last
  sign-in. The Directory partial response asks for account state only,
  so phone numbers, addresses, and recovery details are never read.
  Access is a Workspace admin role, not Cloud IAM, so the providers
  declare no permissions. Most callers have no such role, so a 403 (other
  than a rate limit) or 404 lists nothing, logged and not reported: no
  error, and no Workspace node when nothing listed. Console links open
  the Admin console's user and group lists, since Google documents no
  per-user link. The demo directory is mapped by the live providers from
  static data (`fake/workspace.go`).
- An access binding (`acm.Bindings`, GCP user access binding) holds a
  Google group, a service account, or every service account of a project
  to access levels for the Google Cloud console and APIs, with session
  controls and settings scoped to one client app or project. The list
  returns group bindings by default, so the adapter also lists with the
  service account filter and merges by id; a failed second list is logged.
  v1 has no filter for federated-principal bindings. The API names a group
  by its Directory id, not its email, so each group is looked up once per
  session through Cloud Identity (`groups/ID`); a group that cannot be read
  lists by its id. That Cloud Identity uses the same id is assumed, not
  documented. A binding with dry-run levels, at the top or in a scoped
  setting, gets a `DRY-RUN` badge and a warning status. Its row is named by
  the group email, group id, or principal. A binding expands inline to the
  access levels it names, each with `usedBy` (`binding`, `dry-run`, `app
  Google Cloud SDK`, `project projects/42 dry-run`); `acm` reads them
  through the shared `vpcsc` API and maps them with `vpcsc.LevelResource`,
  so they read as they do under a policy.
- The API cannot say which perimeters protect a project. `vpcsc.Perimeters`
  resolves the project's number and organization through Resource Manager,
  lists every policy's perimeters in that organization (one index per org,
  kept as long as a catalog list result), and keeps those whose resources
  name `projects/NUMBER` or one of the project's VPC networks, in the
  enforced status, the dry-run spec, or both (`membership`, `via`). Their
  ids carry the project as scope (`projects/p/accessPolicies/N/...`).
  Access Context Manager roles are granted on the organization or on a
  scoped policy, never on a project, so the project's permission check
  covers only `resourcemanager.projects.get`; a caller who cannot read the
  organization's policies gets no rows rather than an error, and a scoped
  policy whose perimeters cannot be read is logged and skipped.
- A perimeter's fields are its enforced status. When it has an explicit
  dry-run spec that differs, the row gets a `DRY-RUN` badge and a warning
  status, a `dryRun` summary of the change, and `dryRun*` fields and lists
  for what differs (rules are compared by content: `ingress rules +1 −0`).
  A perimeter's facets are `Access Levels` (each with `usedBy`: enforced,
  dry-run), `Ingress Rules`, and `Egress Rules`, each present only when
  the status or spec has some; with Access Levels alone it expands inline.
- An ingress or egress rule is a row (`ServiceIngressRule`,
  `ServiceEgressRule`) named by its title, or by a summary when it has none
  (`any identity from corp_network → bigquery`). Its fields are who it
  admits, sources (access levels, projects, VPC networks, PSC endpoints),
  egress source restriction, resources, external resources, and roles; an
  `Operations` list gives each service with its methods and permissions.
  Rules have no id, so the enforced rules list in order
  (`.../ingressRules/1`), each marked by `mode`, then the rules only the
  dry-run spec has (`.../dryRunIngressRules/2`, `DRY-RUN` badge). An
  enforced rule the spec drops gets a `NOT IN DRY-RUN` badge. Both carry a
  warning status. Rules are matched by content, so an edited rule reads as
  one removed and one added.
- Access levels are read `AS_DEFINED`, so basic levels keep their
  conditions instead of CEL. A condition reads as text, its device policy
  spelled out (`device encrypted, managed COMPLETE, DESKTOP_MAC ≥ 14.0,
  screen lock`). A custom level's CEL is an `Expression` list,
  `vpcsc.FormatCEL` breaking it into lines at `&&` and `||`, indented by
  parenthesis depth; it reads only brackets and quotes and does not parse
  CEL.

**Inline children.** A Cloud DNS zone expands straight to its record sets
(an `Inline` facet). A
record set's id is its zone's plus `rrsets/{name}/{type}`, since one name
holds a record set per type; the type is the row's `Badge`. A zone lists at
most 500 record sets before the "more not shown" row. A Secret Manager
secret expands to its versions (state, and the CMEK key versions that
encrypt each one; payloads are never read). KMS has no all-locations list,
so `kms.Provider` lists the project's KMS locations and fans out with
`fanout.Regions`.

**Cloud SQL.** An instance has three facets: `Databases`, `Users`
(`ServiceCloudSQLUser`), and `Backups` (`ServiceCloudSQLBackup`); a read
replica takes no backups of its own, so it has no `Backups` facet. A user
is named `name@host` when the engine reports a host (MySQL), its id
escaping the name; IAM users are badged (`IAM`, `IAM SA`, `IAM GROUP`), and
a locked or disabled user is a warning. Passwords are never read. Backups
are listed newest first, as the API returns them, up to 100; each is named
by its start time (or queue time) and badged with its status: successful
is healthy, failed an error, skipped a warning. The instance itself shows
public IP on or off with its authorized networks (a list), SSL mode, CMEK
key, maintenance window (the API's `{day: 0}` means none, so only the
update track shows then), next scheduled maintenance and deny periods,
backup window, retention, and log retention, Query Insights, and database
flags (a list).

**BigQuery.** Four APIs, one product node. The BigQuery API lists a
project's datasets in every location in one call, but only those the caller
can read (`bigquery.datasets.get`, often granted on the dataset alone), so
`bigquery.Datasets` declares no `ListPermissions`; a project-level check
would hide them. The list is thin, so each dataset is read as well, eight
at a time; one that cannot be read keeps the list's fields and the failure
is logged. A dataset shows its friendly name, type (linked and external are
badged `LINK` and `EXT`), source, default table and partition expiration,
CMEK key, time travel window, and storage billing model; authorized views,
routines, and datasets are a list on it. It has four facets: `Tables`
(`ServiceBigQueryTable`, up to 500, badged `VIEW`, `MV`, `EXT`, `SNAP`, or
`CLONE`, with partitioning and clustering from the list call; no size or
row count, which would take a read per table), `Routines` and `Models` (up
to 200 each), and `Permissions`. Permissions read the dataset's access
list: `READER`, `WRITER`, and `OWNER` become `roles/bigquery.dataViewer`,
`dataEditor`, and `dataOwner`, and `projectReaders` (and the other project
groups) become `projectViewer:p` members, one binding per role and
condition. Table data and query history are never read.

The Reservation and Connection APIs cannot list their locations, so
`bigquery.Locations` lists the US and EU multi-regions plus the project's
Compute regions (`regions.DefaultRegions` when Compute is off) and the
adapters fan out over them. Each API is gated on its own Service Usage
name, so a project without reservations or connections makes no calls.
Reservations show baseline slots, autoscaling, idle-slot sharing, and
concurrency, and expand inline to their assignments (named by assignee,
badged by job type; active is healthy, pending a warning). Capacity
commitments are badged by plan (active healthy, pending a warning, failed
an error with its reason). Data Transfer configs fan out over the
locations the Data Transfer API reports; each is named by its display name
and takes its status from the latest run (succeeded healthy, failed an
error with its message, cancelled a warning); a disabled config is badged
`OFF`. Connections show their type, the identity they act as (service
account, AWS role, Azure identity), and what they reach; credentials are
never read.

**Cloud KMS.** A key ring has three facets: `Keys`, `Import Jobs`
(`ServiceKMSImportJob`), and `Permissions` (the bindings set on the ring,
built with `model.BindingResource`, as bucket permissions are). A key has
`Versions` (`ServiceKMSKeyVersion`) and `Permissions`. Versions list
newest first (by version number), up to 500; the primary is badged
`PRIMARY` and the rest with their state, and each shows its algorithm,
protection level, generation, import, and destruction times, import job,
failure reason, and external key URI. Key material is never read. An HSM
version expands (an `Inline` `Attestation` facet) to its attestation, which
one `Get` of that version reads only then; a version list would need its
`FULL` view, fetching every version's attestation. The expansion is an
attestation row (`ServiceKMSAttestation`: format, size, certificate counts)
and a row per certificate chain (`ServiceKMSCertChain`: Cavium, Google
card, Google partition), each certificate a list of its subject, issuer,
serial, and validity. A chain with an expired certificate is a warning;
nothing else is checked, and seiner does not verify the attestation. The
attestation row copies the attestation as base64 and every chain as one
PEM bundle, and each chain row copies its own PEM, to check offline with
Google's `verify_attestation_chains.py`. An attestation holds no key
material: the HSM's signed statement of the key's attributes (and an
asymmetric key's public key) and public certificates. A key without a primary (asymmetric, MAC, raw) has no single
state, so listing a ring's keys also lists those keys' versions (up to
100 each, 8 keys at a time) and summarizes them (`1 enabled, 1 destroy
scheduled`): such a key is healthy while any version is enabled and
otherwise takes the worst state among them. A summary cut off at 100
says so (`(first 100 versions)`), and without an enabled version among
those read the key has no status. A ring with more than 20 such keys
summarizes none, so it still opens with one call. A key whose versions
cannot be listed is logged and keeps no summary; a context that ends
during the summaries fails the list instead. A key also shows its version template when it differs
from the primary, and its Key Access Justifications reasons as a list.
Import jobs are healthy while active and a warning while pending; an
expired job (they expire after three days by design) has no status.

**Compute instances.** A managed instance group carries its autoscaler's
limits (`2–10, CPU 60%`, the mode when not `ON`, the recommended size),
read from one aggregated autoscaler list next to the group list; a failed
autoscaler list is logged and leaves the groups without limits. It also
shows its update limits (max surge and unavailable), minimal action,
replacement method, autohealing delay, and a regional group's zones. The
group expands inline to its instances: `compute.ManagedInstances`
(`ServiceManagedInstance`, a provider that lists nothing at a scope)
serves both a group's `Instances` and a GKE node pool's `Nodes`. A node
pool carries its instance groups as full relative names in
`model.ListInstanceGroups`, so GKE stays free of Compute; a pool without
groups (Autopilot before scale-up) has no `Nodes` facet. A managed
instance's id nests under the row it lists under
(`…/instanceGroupManagers/g/instances/vm`, `…/nodePools/np/instances/vm`),
so it never collides with the same VM's GCE Instance row; its `vm` field
is the VM's own name. An instance not created yet, with no instance
link, is skipped; one with no zone yet takes the group's location. It shows the instance status, the group's
current action (also its badge), health per health check, template and
version, and the last attempt's errors (a list). Errors or an unhealthy
check are an error, an action under way a warning. A node pool lists at
most 500 nodes across its groups.

**Load balancers.** The API has no load balancer resource. Like the
Console, `compute.LoadBalancers` assembles one from URL maps, backend
services, target proxies, and forwarding rules: each forwarding rule is
resolved through its target proxy to a URL map (an application load
balancer) or a backend service (a proxy or passthrough network load
balancer). A backend service a URL map routes to is that map's backend,
not a load balancer of its own, and one no rule reaches is not listed. A
load balancer's id is its URL map's or backend service's name. Its
`Frontends`, `Backends`, and `Health Checks` facets list forwarding rule,
backend service, and health check rows, so the same forwarding rule or
health check can appear both there and in its own collection. Those rows
declare no facets, which keeps navigation pane state keyed by id unambiguous.
Target pools (legacy network load balancers) and backend buckets as rows
are not covered yet; a URL map's buckets show in its `backends` field.

**Grouping rules.**

- `ServiceSpec.Group` names the product. The node takes that product's label
  and icon, sits where its first collection would, and appears only when at
  least one of its collections has items. Specs with no `Group` keep a
  collection row directly under the CRM node.
- To group another product, set `Group` on its specs; `internal/app` needs
  no change.
- A product that is only a node and lists nothing itself sets `GroupOnly` (it
  is then not `Grouped`): `Network Services`, `Network Connectivity`,
  `Network Security`, `Memorystore`, `Logging`, `IAM`, `Private Service
  Connect`, `Secure Web Proxy`, `Access Context Manager`, `VPC Service
  Controls`, and `Google Workspace`.
- Nesting: only a `GroupOnly` product may nest, only under a top-level
  `GroupOnly` product, by setting `Group`. Nested product nodes list after
  the outer product's own collections.

**IAM products.** Service accounts and custom roles (projects; custom roles
also on organizations) come from the IAM Admin API (`iam.googleapis.com`,
gated); one lazy `iam.AdminAPI` serves both providers. A custom role's
details show its permission count; the permissions themselves list under
it (role permissions, below).

IAM Admin lists only user-managed accounts, which include the default
Compute Engine and App Engine accounts. Service agents cannot be listed, so
`iam.ServiceAccounts` also reads the project's IAM policy (sharing the IAM
provider's lazy Resource Manager clients) and adds each Google-created
`serviceAccount:` member not already listed, with its roles in that project.
Accounts in other projects are not added. A policy it cannot read only drops
the agents. `model.GoogleServiceAccount` decides what Google created, by
email: any `*.gserviceaccount.com` domain other than `*.iam`, a `gcp-sa-*`
domain, or a `service-N` local part. The navigation pane hides these unless the
Service Accounts row's `all` choice is on. Each row keeps its own choice by
collection ID, like Role Bindings rows; a row not toggled yet follows
`Config.GoogleServiceAccounts`. The collection and product counts follow
what is shown.

A listed account expands inline to its keys
(`ServiceServiceAccountKey`, `ListServiceAccountKeys`), user-managed
first (badged `USER`), then system-managed, each newest first. A key shows
its type, origin, algorithm, and expiry (`never` for 9999-12-31); its
creation time is its valid-after time. A disabled key is a warning
(`DISABLED`), and so is an expired one (badged `EXPIRED`); other keys are
healthy. Key material, public or private, is never read. Service agents
have no keys facet: their keys are Google's.

**IAM role bindings.** Each binding is one `ServiceIAMBinding` resource
built by `model.BindingResource`, shared by the IAM adapter and GCS bucket
permissions. Its members are a `Resource.Lists` entry (`model.ListMembers`),
which the details pane prints in full, one per line, under `Members (N)`.
A binding's id is `<resource>/iam/bindings/<role>`, plus
`/conditions/<title>` when conditional, so the same role bound under two
conditions stays two resources.

A folder or project also lists the bindings it inherits. The IAM provider
walks the scope's ancestors through Resource Manager (`GetProject`,
`GetFolder`, `GetOrganization`) and reads each ancestor's policy.
`model.InheritedBindingResource` builds those rows: the id is
`<scope>/inherited/<escaped ancestor>/iam/bindings/<role>`, so it stays in
the scope; `Metadata["inheritedFrom"]` is the ancestor's CRM name; a last
`inheritedFrom` field names it; and `Resource.Origin` is its
`ServiceOrganization` or `ServiceFolder`; `inheritedLevel` (1 for the
parent) orders ancestors. The navigation pane lists the scope's own bindings
first, then one collapsed node per ancestor, nearest first
(`model.SplitInherited`, `KindInherited`): `From prod (12)` with `↑` and
the origin's icon right-aligned (the abbreviation when the renderer draws
no icon). The
node expands to that ancestor's bindings in the current view, by role or
by principal, so the long inherited lists stay out of the way of the
scope's own. Rows inside a group do not repeat the origin mark. Only the
scope's own policy must load: an ancestor that cannot be read or whose
policy is denied is logged and skipped. The fixture catalog lists
bindings through the same provider over fixture policies and the fixture
hierarchy.

The `Role Bindings` row carries two mutually exclusive choices drawn
after its name (`◉ role ○ principal`, `resources.Row.Choices`). `v` on the
row, or a click on a choice, switches them; the choice is remembered per
collection. By role, each binding expands to its members. By principal,
`model.GroupByPrincipal` inverts the loaded bindings into synthetic
principal nodes (`KindPrincipal`, no icon) that expand to their bindings.
Switching never calls an API. A resource listed under several parents (a
binding under each of its principals) sets `Row.Via` to its parent row's
key so row keys stay unique; Esc on such a row closes that parent. Bucket
`Permissions` use the by-role layout. While searching, bindings and
principals open so a member or role match shows.

**Role permissions.** A custom role, and a role under its principal in
the by-principal view, expand to the permissions the role grants. By role,
a binding still expands to its members only. Permissions are a facet,
`model.FacetRolePermissions` (`model.RolePermissionsFacet`, inline), served
by `iam.RolePermissions` (`ServiceIAMPermission`), which lists nothing at a
scope. It reads the role named by `model.RoleOf` (a binding's role, or the
custom role itself) with IAM Admin `roles.get`, so predefined roles are
queried, not compiled in. `iam.Roles` keeps each role's permissions by name
for 5 minutes and is shared with `iam.CustomRoles`, whose `roles.list`
(`FULL` view) seeds it: a role bound under many principals is read once,
and a custom role's expansion makes no call. Each permission is one
`ServiceIAMPermission` resource (`model.PermissionResource`), which also
carries the role's title and description in `Metadata` (not `Fields`).
Once a role under its principal has loaded, `model.WithRoleDetails` adds
them to that row's details: the title after the role field, and the
description, which a binding otherwise lacks.

A role can grant thousands of permissions (`roles/owner`), so the navigation pane
lays them out with `model.GroupPermissions`: up to 20 list flat; more are
grouped by service prefix (`storage`), and a service with more than 20 by
resource (`storage.buckets`). A level with only one group is skipped. A
group node (`KindPermissionGroup`) is badged with its count, and its
details list its permissions. By principal, the role's load is keyed by
the binding, so every principal's row shares it, but each row opens on
its own: a row with `Via` opens under its own row key (`openKey`), and
group ids start with that row's position. Esc closes an open group or
role, and on a closed one or a permission closes its parent. While
searching, every permission group of an open role opens, and by principal
a role whose permissions are loaded opens too, so a permission match
shows; search never starts a load. The fixture
catalog reads custom roles from fixtures and predefined roles from a small
sample table (`fake/roles.go`), not their full permission sets.

- Enabled Services lists APIs even when a product has zero resources.
- A type group appears only when List returned at least one resource.
- Disabled products do not get a type group.
- Service Usage is project-scoped. At org/folder, do not use one project’s
  enabled set; show CRM-scoped IAM/org policy only.
- IAM and Resource Manager stay available at every CRM node; they are not
  “enable this API” products in the same way. IAM is client-based:
  service accounts and custom roles list in projects whose enabled
  services lack `iam.googleapis.com` when the quota project has it, so all
  of IAM is ungated. Org Policy is: its adapter
  calls `orgpolicy.googleapis.com`, so a project without that API shows no
  Organization Policies rather than an error.
- The navigation pane should hide disabled products rather than show empty category
  rows. Do not restore a category list.
- Service Usage gating in the catalog filters disabled products (§10).
- Do not replace adapters with Cloud Asset Inventory `searchAllResources`.
---


## 14. Coding principles

1. Smallest clean architecture that satisfies the requirements.
2. No frameworks beyond Charm + Google Cloud libraries without a concrete reason.
3. UI never constructs GCP clients.
4. UI never embeds Kitty escape sequences.
5. Every enhanced feature has a useful fallback.
6. Idiomatic Go: `context.Context`, `errors.Is`/`As`, small packages, table-driven tests, `gofmt`.
7. Keep the tree buildable after each stage.
