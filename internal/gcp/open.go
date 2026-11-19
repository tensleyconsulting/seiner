package gcp

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/acm"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/artifactregistry"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/assuredworkloads"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/bigquery"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/certmanager"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/clouddns"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/cloudsql"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/compute"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/crm"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/gcs"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/gke"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/iam"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/iap"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/kms"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/logging"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/memorystore"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/networkconnectivity"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/orgpolicy"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/privateca"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/secretmanager"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/serviceusage"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/swp"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/vpcsc"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/workspace"
	"github.com/tensleyconsulting/seiner/internal/gcp/adapters/workstations"
	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
	"github.com/tensleyconsulting/seiner/internal/model"
)

// OpenResult.Source values. SourceNone has no catalog to browse: credentials,
// a project, or the live catalog were missing, and Identity says which.
const (
	SourceDemo = "demo"
	SourceLive = "live"
	SourceNone = "none"
)

// OpenOptions selects the demo or the live catalog.
type OpenOptions struct {
	Project    string
	ProjectSet bool
	// InitialResource is the resource name to start on. A folder or
	// organization opens a live catalog on it without a project; a project
	// here is also Project, with ProjectSet.
	InitialResource string
	Impersonate     string
	// Demo opens the fixture catalog and never calls Google for resources.
	Demo bool
	// AWLocations are the Assured Workloads locations to list (the API
	// cannot list them). Empty skips Assured Workloads.
	AWLocations []string
}

// OpenResult is Google-free except for the catalog's unexported clients.
// Catalog is the zero Catalog when Source is SourceNone.
type OpenResult struct {
	Catalog  Catalog
	Identity model.Identity
	Source   string
	Project  string
}

// liveFunc opens a live catalog that starts on scope: a CRM name, or a
// bare project id.
type liveFunc func(ctx context.Context, sess Session, scope string) (Catalog, error)

// Open resolves ADC and returns the demo catalog with --demo, else a live
// catalog. Missing credentials, a missing project, or a live catalog that
// fails to open return SourceNone and say why in Identity; they never fall
// back to the demo catalog.
func Open(ctx context.Context, opts OpenOptions) OpenResult {
	live := func(ctx context.Context, sess Session, scope string) (Catalog, error) {
		return liveCatalog(ctx, sess, scope, opts.AWLocations)
	}
	return open(ctx, opts, ResolveSession(ctx, opts.Impersonate), live)
}

func open(ctx context.Context, opts OpenOptions, sess Session, live liveFunc) OpenResult {
	out := OpenResult{
		Identity: sess.Identity,
		Source:   SourceNone,
		Project:  opts.Project,
	}
	if opts.Demo {
		out.Catalog = FakeCatalog()
		out.Source = SourceDemo
		return out
	}
	if !sess.Ready() {
		return out
	}
	if k := model.ScopeKind(opts.InitialResource); k == model.KindOrg || k == model.KindFolder {
		cat, err := live(ctx, sess, opts.InitialResource)
		if err != nil {
			slog.Warn("live catalog failed", "err", err, "initial_resource", opts.InitialResource)
			out.Identity.Err = fmt.Errorf("live catalog: %w", err)
			return out
		}
		out.Catalog = cat
		out.Source = SourceLive
		return out
	}
	project := opts.Project
	if !opts.ProjectSet {
		project = sess.Identity.ADCProject
	}
	if project == "" {
		slog.Info("no project set and adc has none")
		out.Identity.Notice = model.NoticeNoProject
		return out
	}

	cat, err := live(ctx, sess, project)
	if err != nil {
		slog.Warn("live catalog failed", "err", err, "project", project)
		out.Identity.Err = fmt.Errorf("live catalog: %w", err)
		return out
	}
	out.Catalog = cat
	out.Source = SourceLive
	out.Project = project
	return out
}

// liveProviders returns the live providers, ordered to match model.Specs()
// among the services that have a live adapter. Each one wraps its Google
// client in a lazy.Value: the client is built on first use, not here, so a
// slow or failing constructor cannot block startup, and a failure surfaces as
// that service's ProviderError instead of a silently dropped provider. Adding
// a live service is one more line in this slice. rm is the Resource Manager
// client set the tree also uses; g closes the clients that need closing. The
// invalidators are the shared adapter caches a manual refresh must drop
// (Catalog.WithInvalidators).
func liveProviders(g *lazy.Group, rm *lazy.Value[crm.Clients], opts []option.ClientOption) ([]model.ResourceProvider, []model.Invalidator) {
	// Shared so the client builds once, and memoized so the providers of one
	// project load share each list call.
	computeAPI := compute.NewMemo(compute.NewLazy(opts...))
	iamAdmin := iam.NewLazyAdmin(g, opts...)      // shared so the client builds once
	iamPolicy := iam.NewLazyAPI(rm)               // likewise
	iamRoles := iam.NewRoles(iamAdmin)            // custom roles and role permissions share its cache
	loggingAPI := logging.NewLazy(opts...)        // likewise
	swpAPI := swp.NewLazy(opts...)                // likewise
	vpcscAPI := vpcsc.NewLazy(opts...)            // likewise
	certAPI := certmanager.NewLazy(opts...)       // likewise
	privatecaAPI := privateca.NewLazy(opts...)    // likewise
	ncAPI := networkconnectivity.NewLazy(opts...) // likewise
	workspaceAPI := workspace.NewLazy(g, opts...) // likewise

	// The Reservation and Connection APIs cannot list their locations, so
	// both look in the Compute regions plus US and EU.
	bqLocs := bigquery.NewLocations(opts...)
	bqReservations := bigquery.NewLazyReservationAPI(bqLocs, opts...) // reservations and commitments
	providers := []model.ResourceProvider{
		gcs.NewLazy(g, opts...),
		gke.NewLazy(opts...),
		cloudsql.NewLazy(opts...),
		memorystore.NewLazyRedis(opts...),
		memorystore.NewLazyValkey(g, opts...),
		bigquery.NewLazyDatasets(opts...),
		bigquery.NewReservations(bqReservations),
		bigquery.NewCommitments(bqReservations),
		bigquery.NewLazyTransfers(opts...),
		bigquery.NewLazyConnections(bqLocs, opts...),
		compute.NewInstances(computeAPI),
		compute.NewNetworks(computeAPI),
		compute.NewAddresses(computeAPI),
		compute.NewLoadBalancers(computeAPI),
		compute.NewForwardingRules(computeAPI),
		compute.NewPSCEndpoints(computeAPI),
		compute.NewServiceAttachments(computeAPI),
		networkconnectivity.NewRegionalEndpoints(ncAPI),
		networkconnectivity.NewConnectionPolicies(ncAPI),
		compute.NewRouters(computeAPI),
		clouddns.NewLazy(opts...),
		compute.NewDisks(computeAPI),
		compute.NewInstanceTemplates(computeAPI),
		compute.NewInstanceGroups(computeAPI),
		compute.NewManagedInstances(computeAPI),
		compute.NewHealthChecks(computeAPI),
		compute.NewSecurityPolicies(computeAPI),
		compute.NewSSLPolicies(computeAPI),
		swp.NewTLSInspectionPolicies(swpAPI),
		iap.NewLazy(iamPolicy, opts...),
		swp.NewWebProxies(swpAPI),
		swp.NewPolicies(swpAPI),
		swp.NewURLLists(swpAPI),
		certmanager.NewCertificates(certAPI),
		certmanager.NewMaps(certAPI),
		certmanager.NewDNSAuthorizations(certAPI),
		certmanager.NewIssuanceConfigs(certAPI),
		certmanager.NewTrustConfigs(certAPI),
		acm.NewLazy(vpcscAPI, opts...),
		vpcsc.NewPolicies(vpcscAPI),
		vpcsc.NewPerimeters(vpcscAPI),
		secretmanager.NewLazy(opts...),
		kms.NewLazy(opts...),
		privateca.NewPools(privatecaAPI),
		privateca.NewTemplates(privatecaAPI),
		logging.NewSinks(loggingAPI),
		logging.NewBuckets(loggingAPI),
		logging.NewMetrics(loggingAPI),
		workstations.NewLazy(opts...),
		artifactregistry.NewLazy(g, opts...),
		artifactregistry.NewLazyVulnerabilities(opts...),
		iam.New(iamPolicy),
		iam.NewServiceAccounts(iamAdmin, iamPolicy),
		iam.NewCustomRoles(iamRoles),
		iam.NewRolePermissions(iamRoles),
		orgpolicy.NewLazy(opts...),
		workspace.NewUsers(workspaceAPI),
		workspace.NewGroups(workspaceAPI),
		serviceusage.NewLazy(opts...),
	}
	return providers, []model.Invalidator{computeAPI}
}

func liveCatalog(ctx context.Context, sess Session, scope string, awLocations []string) (Catalog, error) {
	opts, err := sess.clientOptions(ctx)
	if err != nil {
		return Catalog{}, err
	}

	// The single-project stub is the immediate fallback; the real tree is
	// built lazily (asynchronously, from internal/app) on the first
	// Hierarchy call, so a large org's SearchProjects/GetFolder/GetOrg calls
	// never block startup.
	// One Resource Manager client set serves the tree, permission checks,
	// and IAM policy reads. Catalog.Close closes every client built.
	g := &lazy.Group{}
	rmClients := crm.NewLazyClients(g, opts...)
	rm := crm.NewLazy(rmClients)
	providers, invalidators := liveProviders(g, rmClients, opts)
	cat := NewCatalog(providers...).
		WithInvalidators(invalidators...).
		WithHierarchy(model.ScopeNode(scope)).
		WithLiveHierarchy(rm).
		WithPermissionTester(rm).
		WithQuotaProject(sess.quotaProject()).
		withClients(g)
	if len(awLocations) > 0 {
		cat = cat.WithAssuredWorkloads(assuredworkloads.NewLazy(opts...), awLocations)
	}
	return cat, nil
}
