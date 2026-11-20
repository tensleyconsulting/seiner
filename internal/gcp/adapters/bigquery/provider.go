package bigquery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tensleyconsulting/seiner/internal/gcp/display"
	"github.com/tensleyconsulting/seiner/internal/gcp/fanout"
	"github.com/tensleyconsulting/seiner/internal/gcp/regions"
	"github.com/tensleyconsulting/seiner/internal/model"
)

const (
	// datasetConcurrency caps the dataset reads List makes for details.
	datasetConcurrency  = 8
	locationConcurrency = 24
	maxTables           = 500
	maxRoutines         = 200
	maxModels           = 200
)

// multiRegions hold reservations, commitments, and connections alongside
// the regions.
var multiRegions = []string{"US", "EU"}

// Locations reports where the regional BigQuery APIs keep resources: the
// US and EU multi-regions, then the regions r lists. When r fails (Compute
// Engine is off in the project), the regions are regions.DefaultRegions.
func Locations(r regions.Lister) regions.Lister { return locations{r: r} }

type locations struct{ r regions.Lister }

func (l locations) Regions(ctx context.Context, project string) ([]string, error) {
	regs, err := l.r.Regions(ctx, project)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		slog.Debug("bigquery: region list failed; using defaults", "project", project, "err", err)
		regs = regions.DefaultRegions
	}
	return append(slices.Clone(multiRegions), regs...), nil
}

// Datasets implements model.ResourceProvider for BigQuery datasets. A
// dataset expands to its tables, routines, models, and permissions.
//
// It declares no list permission: listing needs none of its own and
// returns the datasets the caller may read, which is often granted on the
// dataset rather than the project, so a project-level check would hide
// them.
type Datasets struct{ api DatasetAPI }

func NewDatasets(api DatasetAPI) Datasets { return Datasets{api: api} }

func (p Datasets) Service() model.ServiceType { return model.ServiceBigQueryDataset }

func (p Datasets) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("bigquery: no API")
	}
	listed, err := p.api.ListDatasets(ctx, project)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(listed))
	for i, d := range listed {
		ids[i] = d.ID
	}
	// The list carries no details, so read each dataset. One that cannot
	// be read keeps what the list said.
	full, failed := fanout.Regions(ctx, ids, datasetConcurrency, func(ctx context.Context, id string) ([]Dataset, error) {
		d, err := p.api.GetDataset(ctx, project, id)
		if err != nil {
			return nil, err
		}
		return []Dataset{d}, nil
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, f := range failed {
		slog.Warn("bigquery: dataset details failed", "project", project, "dataset", f.Location, "err", f.Err)
	}
	byID := make(map[string]Dataset, len(full))
	for _, d := range full {
		byID[d.ID] = d
	}
	out := make([]model.Resource, 0, len(listed))
	for _, d := range listed {
		if f, ok := byID[d.ID]; ok {
			d = f
		}
		out = append(out, datasetResource(project, d))
	}
	return out, nil
}

// ListRelated lists a dataset's tables, routines, models, and access
// list, and a table's or routine's IAM bindings.
func (p Datasets) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	switch {
	case parent.Service == model.ServiceBigQueryDataset &&
		(facetID == model.FacetTables || facetID == model.FacetRoutines || facetID == model.FacetModels || facetID == model.FacetPermissions):
	case (parent.Service == model.ServiceBigQueryTable || parent.Service == model.ServiceBigQueryRoutine) && facetID == model.FacetPermissions:
	default:
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Kind != "datasets" {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("bigquery: no API")
	}
	if parent.Service == model.ServiceBigQueryTable {
		if len(n.Rest) != 2 || n.Rest[0] != "tables" || n.Rest[1] == "" {
			return nil, false, nil
		}
		bindings, err := p.api.GetTableIAMPolicy(ctx, n.Project, n.Name, n.Rest[1])
		if err != nil {
			return nil, false, err
		}
		return bindingResources(parent, bindings), false, nil
	}
	if parent.Service == model.ServiceBigQueryRoutine {
		if len(n.Rest) != 2 || n.Rest[0] != "routines" || n.Rest[1] == "" {
			return nil, false, nil
		}
		bindings, err := p.api.GetRoutineIAMPolicy(ctx, n.Project, n.Name, n.Rest[1])
		if err != nil {
			return nil, false, err
		}
		return bindingResources(parent, bindings), false, nil
	}
	if len(n.Rest) != 0 {
		return nil, false, nil
	}
	var (
		out       []model.Resource
		truncated bool
	)
	switch facetID {
	case model.FacetTables:
		tables, more, err := p.api.ListTables(ctx, n.Project, n.Name, maxTables)
		if err != nil {
			return nil, false, err
		}
		truncated = more
		for _, t := range tables {
			out = append(out, tableResource(parent, t))
		}
	case model.FacetRoutines:
		routines, more, err := p.api.ListRoutines(ctx, n.Project, n.Name, maxRoutines)
		if err != nil {
			return nil, false, err
		}
		truncated = more
		for _, r := range routines {
			out = append(out, routineResource(parent, r))
		}
	case model.FacetModels:
		models, more, err := p.api.ListModels(ctx, n.Project, n.Name, maxModels)
		if err != nil {
			return nil, false, err
		}
		truncated = more
		for _, m := range models {
			out = append(out, modelResource(parent, m))
		}
	case model.FacetPermissions:
		d, err := p.api.GetDataset(ctx, n.Project, n.Name)
		if err != nil {
			return nil, false, err
		}
		out = accessBindings(parent, d.Access)
	}
	return out, truncated, nil
}

func datasetID(project, dataset string) string {
	return "projects/" + project + "/datasets/" + url.PathEscape(dataset)
}

func datasetResource(project string, d Dataset) model.Resource {
	md := map[string]string{}
	set(md, "friendlyName", d.FriendlyName)
	if d.Type != "" && d.Type != "DEFAULT" {
		md["type"] = d.Type
	}
	set(md, "linkedSource", d.LinkedSource)
	set(md, "externalSource", d.ExternalSource)
	set(md, "tableExpiration", span(d.TableExpiration))
	set(md, "partitionExpiration", span(d.PartitionExpiration))
	set(md, "kmsKey", d.KMSKey)
	set(md, "timeTravel", span(d.TimeTravel))
	set(md, "storageBilling", d.StorageBilling)
	if d.CaseInsensitive {
		md["caseInsensitive"] = "on"
	}
	set(md, "modified", display.Stamp(d.Modified))
	var authorized []string
	for _, a := range d.Access {
		if a.Authorized != "" {
			authorized = append(authorized, a.Authorized)
		}
	}
	if len(authorized) > 0 {
		md["authorized"] = strconv.Itoa(len(authorized))
	}
	r := model.Resource{
		ID:          datasetID(project, d.ID),
		Name:        d.ID,
		Service:     model.ServiceBigQueryDataset,
		ScopeID:     "projects/" + project,
		Location:    d.Location,
		Description: d.Description,
		Created:     d.Created,
		Labels:      maps.Clone(d.Labels),
		Badge:       map[string]string{"LINKED": "LINK", "EXTERNAL": "EXT"}[d.Type],
		Metadata:    md,
		Fields: model.FieldsOf(md, "friendlyName", "type", "linkedSource", "externalSource", "tableExpiration",
			"partitionExpiration", "kmsKey", "timeTravel", "storageBilling", "caseInsensitive", "authorized", "modified"),
		Facets: []model.Facet{
			{ID: model.FacetTables, Label: "Tables"},
			{ID: model.FacetRoutines, Label: "Routines"},
			{ID: model.FacetModels, Label: "Models"},
			{ID: model.FacetPermissions, Label: "Permissions", Optional: true},
		},
	}
	if len(authorized) > 0 {
		r.Lists = []model.List{{ID: "authorized", Label: "Authorized views, routines, and datasets", Items: authorized}}
	}
	return r
}

// child is a dataset's facet item of kind (tables, routines, models).
func child(parent model.Resource, facetID, kind, name string, svc model.ServiceType) model.Resource {
	return model.Resource{
		ID:       parent.ID + "/" + kind + "/" + url.PathEscape(name),
		Name:     name,
		Service:  svc,
		ScopeID:  parent.ScopeID,
		Location: parent.Location,
		ParentID: parent.ID,
		FacetID:  facetID,
	}
}

var tableBadges = map[string]string{
	"VIEW":              "VIEW",
	"MATERIALIZED_VIEW": "MV",
	"EXTERNAL":          "EXT",
	"SNAPSHOT":          "SNAP",
	"CLONE":             "CLONE",
}

func tableResource(parent model.Resource, t Table) model.Resource {
	md := map[string]string{}
	set(md, "type", t.Type)
	set(md, "friendlyName", t.FriendlyName)
	set(md, "partitioning", t.Partitioning)
	if t.PartitionFilter {
		md["partitionFilter"] = "required"
	}
	set(md, "clustering", strings.Join(t.Clustering, ", "))
	if t.LegacySQL {
		md["sql"] = "legacy"
	}
	set(md, "expires", display.Stamp(t.Expires))
	r := child(parent, model.FacetTables, "tables", t.ID, model.ServiceBigQueryTable)
	r.Badge = tableBadges[t.Type]
	r.Created = t.Created
	r.Labels = maps.Clone(t.Labels)
	r.Metadata = md
	r.Fields = model.FieldsOf(md, "type", "friendlyName", "partitioning", "partitionFilter", "clustering", "sql", "expires")
	r.Facets = []model.Facet{{ID: model.FacetPermissions, Label: "Permissions", Optional: true}}
	return r
}

var routineBadges = map[string]string{
	"PROCEDURE":             "PROC",
	"SCALAR_FUNCTION":       "UDF",
	"TABLE_VALUED_FUNCTION": "TVF",
	"AGGREGATE_FUNCTION":    "UDAF",
}

func routineResource(parent model.Resource, rt Routine) model.Resource {
	md := map[string]string{}
	set(md, "type", rt.Type)
	set(md, "language", rt.Language)
	set(md, "modified", display.Stamp(rt.Modified))
	r := child(parent, model.FacetRoutines, "routines", rt.ID, model.ServiceBigQueryRoutine)
	r.Badge = routineBadges[rt.Type]
	r.Created = rt.Created
	r.Metadata = md
	r.Fields = model.FieldsOf(md, "type", "language", "modified")
	r.Facets = []model.Facet{{ID: model.FacetPermissions, Label: "Permissions", Optional: true}}
	return r
}

func modelResource(parent model.Resource, m Model) model.Resource {
	md := map[string]string{}
	set(md, "type", m.Type)
	set(md, "friendlyName", m.FriendlyName)
	set(md, "modified", display.Stamp(m.Modified))
	set(md, "expires", display.Stamp(m.Expires))
	r := child(parent, model.FacetModels, "models", m.ID, model.ServiceBigQueryModel)
	r.Created = m.Created
	r.Labels = maps.Clone(m.Labels)
	r.Metadata = md
	r.Fields = model.FieldsOf(md, "type", "friendlyName", "modified", "expires")
	return r
}

// basicRoles are the dataset roles the access list names by their legacy
// words, as the IAM roles they are.
var basicRoles = map[string]string{
	"READER": "roles/bigquery.dataViewer",
	"WRITER": "roles/bigquery.dataEditor",
	"OWNER":  "roles/bigquery.dataOwner",
}

// accessBindings folds a dataset's access entries into one binding per
// role and condition, in the order the roles first appear. Authorized
// views, routines, and datasets are not principals; the dataset lists
// them itself.
func accessBindings(parent model.Resource, access []Access) []model.Resource {
	type key struct{ role, condition string }
	var order []key
	members := map[key][]string{}
	for _, a := range access {
		if a.Member == "" {
			continue
		}
		role := a.Role
		if r, ok := basicRoles[role]; ok {
			role = r
		}
		k := key{role, a.Condition}
		if _, ok := members[k]; !ok {
			order = append(order, k)
		}
		if !slices.Contains(members[k], a.Member) {
			members[k] = append(members[k], a.Member)
		}
	}
	out := make([]model.Resource, 0, len(order))
	for _, k := range order {
		r := model.BindingResource(parent.ID, parent.ScopeID, k.role, members[k], k.condition)
		r.ParentID = parent.ID
		r.FacetID = model.FacetPermissions
		out = append(out, r)
	}
	return out
}

// Reservations implements model.ResourceProvider for slot reservations,
// which expand to their assignments.
type Reservations struct{ api ReservationAPI }

func NewReservations(api ReservationAPI) Reservations { return Reservations{api: api} }

func (p Reservations) Service() model.ServiceType { return model.ServiceBigQueryReservation }

// ListPermissions is what List needs.
func (p Reservations) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "bigquery.reservations.list")
}

func (p Reservations) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if p.api == nil {
		return nil, errors.New("bigquery: no Reservation API")
	}
	return listRegional(ctx, scopeID, p.api.Locations, p.api.ListReservations, reservationResource)
}

// ListRelated lists a reservation's assignments or IAM bindings, and an
// assignment's IAM bindings.
func (p Reservations) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	switch {
	case parent.Service == model.ServiceBigQueryReservation && (facetID == model.FacetAssignments || facetID == model.FacetPermissions):
	case parent.Service == model.ServiceBigQueryAssignment && facetID == model.FacetPermissions:
	default:
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Location == "" || n.Kind != "reservations" {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("bigquery: no Reservation API")
	}
	if parent.Service == model.ServiceBigQueryAssignment {
		if len(n.Rest) != 2 || n.Rest[0] != "assignments" || n.Rest[1] == "" {
			return nil, false, nil
		}
		bindings, err := p.api.GetAssignmentIAMPolicy(ctx, n.Project, n.Location, n.Name, n.Rest[1])
		if err != nil {
			return nil, false, err
		}
		return bindingResources(parent, bindings), false, nil
	}
	if len(n.Rest) != 0 {
		return nil, false, nil
	}
	if facetID == model.FacetPermissions {
		bindings, err := p.api.GetReservationIAMPolicy(ctx, n.Project, n.Location, n.Name)
		if err != nil {
			return nil, false, err
		}
		return bindingResources(parent, bindings), false, nil
	}
	as, err := p.api.ListAssignments(ctx, n.Project, n.Location, n.Name)
	if err != nil {
		return nil, false, err
	}
	out := make([]model.Resource, 0, len(as))
	for _, a := range as {
		out = append(out, assignmentResource(parent, a))
	}
	return out, false, nil
}

// Commitments implements model.ResourceProvider for capacity commitments.
type Commitments struct{ api ReservationAPI }

func NewCommitments(api ReservationAPI) Commitments { return Commitments{api: api} }

func (p Commitments) Service() model.ServiceType { return model.ServiceBigQueryCommitment }

// ListPermissions is what List needs.
func (p Commitments) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "bigquery.capacityCommitments.list")
}

func (p Commitments) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if p.api == nil {
		return nil, errors.New("bigquery: no Reservation API")
	}
	return listRegional(ctx, scopeID, p.api.Locations, p.api.ListCommitments, commitmentResource)
}

// Transfers implements model.ResourceProvider for Data Transfer configs.
type Transfers struct{ api TransferAPI }

func NewTransfers(api TransferAPI) Transfers { return Transfers{api: api} }

func (p Transfers) Service() model.ServiceType { return model.ServiceBigQueryTransfer }

// ListPermissions is what List needs: listing transfer configs checks
// bigquery.transfers.get.
func (p Transfers) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "bigquery.transfers.get")
}

func (p Transfers) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if p.api == nil {
		return nil, errors.New("bigquery: no Data Transfer API")
	}
	return listRegional(ctx, scopeID, p.api.Locations, p.api.ListTransfers, transferResource)
}

// Connections implements model.ResourceProvider for external connections.
type Connections struct{ api ConnectionAPI }

func NewConnections(api ConnectionAPI) Connections { return Connections{api: api} }

func (p Connections) Service() model.ServiceType { return model.ServiceBigQueryConnection }

// ListPermissions is what List needs.
func (p Connections) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "bigquery.connections.list")
}

func (p Connections) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if p.api == nil {
		return nil, errors.New("bigquery: no Connection API")
	}
	return listRegional(ctx, scopeID, p.api.Locations, p.api.ListConnections, connectionResource)
}

// ListRelated lists a connection's IAM bindings.
func (p Connections) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceBigQueryConnection || facetID != model.FacetPermissions {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Location == "" || n.Kind != "connections" || len(n.Rest) != 0 {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("bigquery: no Connection API")
	}
	bindings, err := p.api.GetConnectionIAMPolicy(ctx, n.Project, n.Location, n.Name)
	if err != nil {
		return nil, false, err
	}
	return bindingResources(parent, bindings), false, nil
}

func bindingResources(parent model.Resource, bindings []Binding) []model.Resource {
	out := make([]model.Resource, 0, len(bindings))
	for _, b := range bindings {
		r := model.BindingResource(parent.ID, parent.ScopeID, b.Role, b.Members, b.Condition)
		r.ParentID = parent.ID
		r.FacetID = model.FacetPermissions
		out = append(out, r)
	}
	return out
}

// listRegional lists one regional resource type in every location of the
// project scopeID names. A failed location is ignored so restricted or
// unreachable regions never surface as errors; the list fails only when
// every location fails or ctx ends.
func listRegional[T any](ctx context.Context, scopeID string,
	locations func(context.Context, string) ([]string, error),
	fetch func(context.Context, string, string) ([]T, error),
	toResource func(string, T) model.Resource,
) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil, nil
	}
	locs, err := locations(ctx, project)
	if err != nil {
		return nil, err
	}
	items, locErrs := fanout.Regions(ctx, locs, locationConcurrency, func(ctx context.Context, loc string) ([]T, error) {
		return fetch(ctx, project, loc)
	})
	if len(locErrs) > 0 {
		if len(locErrs) == len(locs) {
			return nil, fmt.Errorf("bigquery: every location failed: %w", locErrs[0])
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		slog.Debug("bigquery: some locations failed", "project", project, "failed", len(locErrs), "locations", len(locs), "err", locErrs[0])
	}
	out := make([]model.Resource, 0, len(items))
	for _, it := range items {
		out = append(out, toResource(project, it))
	}
	return out, nil
}

func locatedID(project, location, kind, name string) string {
	return "projects/" + project + "/locations/" + url.PathEscape(location) + "/" + kind + "/" + url.PathEscape(name)
}

func reservationResource(project string, rv Reservation) model.Resource {
	md := map[string]string{}
	set(md, "edition", rv.Edition)
	md["slots"] = strconv.FormatInt(rv.Slots, 10)
	if rv.MaxSlots > 0 {
		md["autoscale"] = fmt.Sprintf("up to %d more (%d in use)", rv.MaxSlots, rv.CurrentSlots)
	}
	if rv.IgnoreIdle {
		md["idleSlots"] = "not used"
	} else {
		md["idleSlots"] = "used"
	}
	if rv.Concurrency > 0 {
		md["concurrency"] = strconv.FormatInt(rv.Concurrency, 10)
	}
	set(md, "secondaryLocation", rv.SecondaryLocation)
	set(md, "updated", display.Stamp(rv.Updated))
	return model.Resource{
		ID:       locatedID(project, rv.Location, "reservations", rv.Name),
		Name:     rv.Name,
		Service:  model.ServiceBigQueryReservation,
		ScopeID:  "projects/" + project,
		Location: rv.Location,
		Created:  rv.Created,
		Labels:   maps.Clone(rv.Labels),
		Metadata: md,
		Fields:   model.FieldsOf(md, "edition", "slots", "autoscale", "idleSlots", "concurrency", "secondaryLocation", "updated"),
		Facets: []model.Facet{
			{ID: model.FacetAssignments, Label: "Assignments"},
			{ID: model.FacetPermissions, Label: "Permissions", Optional: true},
		},
	}
}

func assignmentResource(parent model.Resource, a Assignment) model.Resource {
	md := map[string]string{"assignee": a.Assignee}
	set(md, "jobType", a.JobType)
	set(md, "assignmentId", a.Name)
	return model.Resource{
		ID:       parent.ID + "/assignments/" + url.PathEscape(a.Name),
		Name:     a.Assignee,
		Service:  model.ServiceBigQueryAssignment,
		ScopeID:  parent.ScopeID,
		Location: parent.Location,
		Status:   assignmentStatus(a.State),
		State:    a.State,
		Badge:    a.JobType,
		ParentID: parent.ID,
		FacetID:  model.FacetAssignments,
		Metadata: md,
		Fields:   model.FieldsOf(md, "assignee", "jobType", "assignmentId"),
		Facets:   []model.Facet{{ID: model.FacetPermissions, Label: "Permissions", Optional: true}},
	}
}

func commitmentResource(project string, c Commitment) model.Resource {
	md := map[string]string{}
	set(md, "edition", c.Edition)
	md["slots"] = strconv.FormatInt(c.Slots, 10)
	set(md, "plan", c.Plan)
	set(md, "renewalPlan", c.RenewalPlan)
	set(md, "start", display.Stamp(c.Start))
	set(md, "end", display.Stamp(c.End))
	set(md, "failure", c.Failure)
	return model.Resource{
		ID:       locatedID(project, c.Location, "capacityCommitments", c.Name),
		Name:     c.Name,
		Service:  model.ServiceBigQueryCommitment,
		ScopeID:  "projects/" + project,
		Location: c.Location,
		Status:   commitmentStatus(c.State),
		State:    c.State,
		Badge:    c.Plan,
		Metadata: md,
		Fields:   model.FieldsOf(md, "failure", "edition", "slots", "plan", "renewalPlan", "start", "end"),
	}
}

func transferResource(project string, t Transfer) model.Resource {
	md := map[string]string{"configId": t.ID}
	set(md, "error", t.Error)
	set(md, "dataSource", t.DataSource)
	set(md, "destination", t.Dataset)
	set(md, "schedule", t.Schedule)
	if t.Disabled {
		md["schedule"] = strings.TrimSpace(md["schedule"] + " (disabled)")
	}
	set(md, "nextRun", display.Stamp(t.NextRun))
	set(md, "owner", t.Owner)
	set(md, "notifications", t.Topic)
	set(md, "kmsKey", t.KMSKey)
	set(md, "updated", display.Stamp(t.Updated))
	name := t.DisplayName
	if name == "" {
		name = t.ID
	}
	r := model.Resource{
		ID:       locatedID(project, t.Location, "transferConfigs", t.ID),
		Name:     name,
		Service:  model.ServiceBigQueryTransfer,
		ScopeID:  "projects/" + project,
		Location: t.Location,
		Status:   transferStatus(t.State),
		State:    t.State,
		Metadata: md,
		Fields:   model.FieldsOf(md, "error", "dataSource", "destination", "schedule", "nextRun", "owner", "notifications", "kmsKey", "configId", "updated"),
	}
	if t.Disabled {
		r.Badge = "OFF"
		r.Status = model.StatusWarning
	}
	return r
}

func connectionResource(project string, c Connection) model.Resource {
	md := map[string]string{}
	set(md, "friendlyName", c.FriendlyName)
	set(md, "type", c.Kind)
	set(md, "identity", c.Identity)
	set(md, "target", c.Target)
	set(md, "kmsKey", c.KMSKey)
	set(md, "modified", display.Stamp(c.Modified))
	return model.Resource{
		ID:          locatedID(project, c.Location, "connections", c.ID),
		Name:        c.ID,
		Service:     model.ServiceBigQueryConnection,
		ScopeID:     "projects/" + project,
		Location:    c.Location,
		Description: c.Description,
		Created:     c.Created,
		Metadata:    md,
		Fields:      model.FieldsOf(md, "friendlyName", "type", "identity", "target", "kmsKey", "modified"),
		Facets:      []model.Facet{{ID: model.FacetPermissions, Label: "Permissions", Optional: true}},
	}
}

func assignmentStatus(s string) model.ResourceStatus {
	switch s {
	case "ACTIVE":
		return model.StatusHealthy
	case "PENDING":
		return model.StatusWarning
	default:
		return model.StatusUnknown
	}
}

func commitmentStatus(s string) model.ResourceStatus {
	switch s {
	case "ACTIVE":
		return model.StatusHealthy
	case "PENDING":
		return model.StatusWarning
	case "FAILED":
		return model.StatusError
	default:
		return model.StatusUnknown
	}
}

// transferStatus reads the latest run's state.
func transferStatus(s string) model.ResourceStatus {
	switch s {
	case "SUCCEEDED":
		return model.StatusHealthy
	case "CANCELLED":
		return model.StatusWarning
	case "FAILED":
		return model.StatusError
	default:
		return model.StatusUnknown
	}
}

// span renders d in whole days or hours when it is a multiple of one, and
// "" for zero.
func span(d time.Duration) string {
	const day = 24 * time.Hour
	switch {
	case d <= 0:
		return ""
	case d%day == 0:
		return plural(int64(d/day), "day")
	case d%time.Hour == 0:
		return plural(int64(d/time.Hour), "hour")
	default:
		return d.String()
	}
}

func plural(n int64, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.FormatInt(n, 10) + " " + unit + "s"
}

// set stores v under k when v is not empty.
func set(md map[string]string, k, v string) {
	if v != "" {
		md[k] = v
	}
}

var (
	_ model.ResourceProvider   = Datasets{}
	_ model.RelatedLister      = Datasets{}
	_ model.ResourceProvider   = Reservations{}
	_ model.RelatedLister      = Reservations{}
	_ model.PermissionDeclarer = Reservations{}
	_ model.PermissionDeclarer = Commitments{}
	_ model.PermissionDeclarer = Transfers{}
	_ model.PermissionDeclarer = Connections{}
	_ model.RelatedLister      = Connections{}
	_ regions.Lister           = locations{}
)
