package bigquery

import (
	"context"
	"errors"
	"strings"
	"time"

	bq "google.golang.org/api/bigquery/v2"
	connection "google.golang.org/api/bigqueryconnection/v1"
	transfer "google.golang.org/api/bigquerydatatransfer/v1"
	reservation "google.golang.org/api/bigqueryreservation/v1"
	"google.golang.org/api/option"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
	"github.com/tensleyconsulting/seiner/internal/gcp/regions"
)

// NewLocations is where the Reservation and Connection APIs are listed:
// the project's Compute Engine regions plus the US and EU multi-regions.
// Share one between NewLazyReservationAPI and NewLazyConnections.
func NewLocations(opts ...option.ClientOption) regions.Lister {
	return Locations(regions.NewLive(opts...))
}

// NewLazyDatasets defers client construction to the first call, so a slow
// or failing constructor cannot block startup; the failure surfaces as this
// service's ProviderError instead.
func NewLazyDatasets(opts ...option.ClientOption) Datasets {
	return NewDatasets(datasetAPI{l: lazy.New(func(ctx context.Context) (*bq.Service, error) {
		return bq.NewService(ctx, opts...)
	})})
}

// NewLazyReservationAPI is the Reservation API, built on first use and
// listed at locs. Reservations and Commitments share one.
func NewLazyReservationAPI(locs regions.Lister, opts ...option.ClientOption) ReservationAPI {
	return reservationAPI{locs: locs, l: lazy.New(func(ctx context.Context) (*reservation.Service, error) {
		return reservation.NewService(ctx, opts...)
	})}
}

// NewLazyTransfers lists transfer configs in the locations the Data
// Transfer API reports, cached for the session.
func NewLazyTransfers(opts ...option.ClientOption) Transfers {
	l := lazy.New(func(ctx context.Context) (*transfer.Service, error) {
		return transfer.NewService(ctx, opts...)
	})
	locs := regions.Cached(func(ctx context.Context, project string) ([]string, error) {
		svc, err := l.Get(ctx)
		if err != nil {
			return nil, err
		}
		return transferLocations(ctx, svc, project)
	})
	return NewTransfers(transferAPI{l: l, locs: locs})
}

// NewLazyConnections lists connections at locs.
func NewLazyConnections(locs regions.Lister, opts ...option.ClientOption) Connections {
	return NewConnections(connectionAPI{locs: locs, l: lazy.New(func(ctx context.Context) (*connection.Service, error) {
		return connection.NewService(ctx, opts...)
	})})
}

// errStop ends paging once a limit is reached.
var errStop = errors.New("stop")

type datasetAPI struct{ l *lazy.Value[*bq.Service] }

func (a datasetAPI) ListDatasets(ctx context.Context, project string) ([]Dataset, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	var out []Dataset
	err = svc.Datasets.List(project).Pages(ctx, func(resp *bq.DatasetList) error {
		for _, d := range resp.Datasets {
			out = append(out, fromListedDataset(d))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a datasetAPI) GetDataset(ctx context.Context, project, dataset string) (Dataset, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return Dataset{}, err
	}
	d, err := svc.Datasets.Get(project, dataset).Context(ctx).Do()
	if err != nil {
		return Dataset{}, err
	}
	return fromDataset(d), nil
}

func (a datasetAPI) ListTables(ctx context.Context, project, dataset string, limit int) ([]Table, bool, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	var out []Table
	truncated := false
	err = svc.Tables.List(project, dataset).Pages(ctx, func(resp *bq.TableList) error {
		for _, t := range resp.Tables {
			if len(out) == limit {
				truncated = true
				return errStop
			}
			out = append(out, fromTable(t))
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return nil, false, err
	}
	return out, truncated, nil
}

func (a datasetAPI) ListRoutines(ctx context.Context, project, dataset string, limit int) ([]Routine, bool, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	var out []Routine
	truncated := false
	err = svc.Routines.List(project, dataset).Pages(ctx, func(resp *bq.ListRoutinesResponse) error {
		for _, r := range resp.Routines {
			if len(out) == limit {
				truncated = true
				return errStop
			}
			out = append(out, fromRoutine(r))
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return nil, false, err
	}
	return out, truncated, nil
}

func (a datasetAPI) ListModels(ctx context.Context, project, dataset string, limit int) ([]Model, bool, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	var out []Model
	truncated := false
	err = svc.Models.List(project, dataset).Pages(ctx, func(resp *bq.ListModelsResponse) error {
		for _, m := range resp.Models {
			if len(out) == limit {
				truncated = true
				return errStop
			}
			out = append(out, fromModel(m))
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return nil, false, err
	}
	return out, truncated, nil
}

type reservationAPI struct {
	l    *lazy.Value[*reservation.Service]
	locs regions.Lister
}

func (a reservationAPI) Locations(ctx context.Context, project string) ([]string, error) {
	return a.locs.Regions(ctx, project)
}

func (a reservationAPI) ListReservations(ctx context.Context, project, location string) ([]Reservation, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	var out []Reservation
	err = svc.Projects.Locations.Reservations.List(locationName(project, location)).Pages(ctx, func(resp *reservation.ListReservationsResponse) error {
		for _, r := range resp.Reservations {
			out = append(out, fromReservation(r))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a reservationAPI) ListAssignments(ctx context.Context, project, location, name string) ([]Assignment, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	var out []Assignment
	parent := locationName(project, location) + "/reservations/" + name
	err = svc.Projects.Locations.Reservations.Assignments.List(parent).Pages(ctx, func(resp *reservation.ListAssignmentsResponse) error {
		for _, as := range resp.Assignments {
			out = append(out, Assignment{Name: lastSeg(as.Name), Assignee: as.Assignee, JobType: as.JobType, State: as.State})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a reservationAPI) ListCommitments(ctx context.Context, project, location string) ([]Commitment, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	var out []Commitment
	err = svc.Projects.Locations.CapacityCommitments.List(locationName(project, location)).Pages(ctx, func(resp *reservation.ListCapacityCommitmentsResponse) error {
		for _, c := range resp.CapacityCommitments {
			out = append(out, fromCommitment(c))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type transferAPI struct {
	l    *lazy.Value[*transfer.Service]
	locs regions.Lister
}

func (a transferAPI) Locations(ctx context.Context, project string) ([]string, error) {
	return a.locs.Regions(ctx, project)
}

func transferLocations(ctx context.Context, svc *transfer.Service, project string) ([]string, error) {
	var out []string
	err := svc.Projects.Locations.List("projects/"+project).Pages(ctx, func(resp *transfer.ListLocationsResponse) error {
		for _, l := range resp.Locations {
			out = append(out, l.LocationId)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a transferAPI) ListTransfers(ctx context.Context, project, location string) ([]Transfer, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	var out []Transfer
	err = svc.Projects.Locations.TransferConfigs.List(locationName(project, location)).Pages(ctx, func(resp *transfer.ListTransferConfigsResponse) error {
		for _, t := range resp.TransferConfigs {
			out = append(out, fromTransfer(t))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type connectionAPI struct {
	l    *lazy.Value[*connection.Service]
	locs regions.Lister
}

func (a connectionAPI) Locations(ctx context.Context, project string) ([]string, error) {
	return a.locs.Regions(ctx, project)
}

// connectionPage is the page size ListConnections must send.
const connectionPage = 100

func (a connectionAPI) ListConnections(ctx context.Context, project, location string) ([]Connection, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	var out []Connection
	err = svc.Projects.Locations.Connections.List(locationName(project, location)).PageSize(connectionPage).Pages(ctx, func(resp *connection.ListConnectionsResponse) error {
		for _, c := range resp.Connections {
			out = append(out, fromConnection(c))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

var (
	_ DatasetAPI     = datasetAPI{}
	_ ReservationAPI = reservationAPI{}
	_ TransferAPI    = transferAPI{}
	_ ConnectionAPI  = connectionAPI{}
)

func fromListedDataset(d *bq.DatasetListDatasets) Dataset {
	out := Dataset{FriendlyName: d.FriendlyName, Location: d.Location, Type: d.Type, Labels: d.Labels}
	if d.DatasetReference != nil {
		out.ID = d.DatasetReference.DatasetId
	}
	if e := d.ExternalDatasetReference; e != nil {
		out.ExternalSource = e.ExternalSource
	}
	return out
}

func fromDataset(d *bq.Dataset) Dataset {
	out := Dataset{
		FriendlyName:        d.FriendlyName,
		Location:            d.Location,
		Type:                d.Type,
		Labels:              d.Labels,
		Detailed:            true,
		Description:         d.Description,
		Created:             millis(d.CreationTime),
		Modified:            millis(d.LastModifiedTime),
		TableExpiration:     time.Duration(d.DefaultTableExpirationMs) * time.Millisecond,
		PartitionExpiration: time.Duration(d.DefaultPartitionExpirationMs) * time.Millisecond,
		TimeTravel:          time.Duration(d.MaxTimeTravelHours) * time.Hour,
		StorageBilling:      d.StorageBillingModel,
		CaseInsensitive:     d.IsCaseInsensitive,
	}
	if d.DatasetReference != nil {
		out.ID = d.DatasetReference.DatasetId
	}
	if e := d.DefaultEncryptionConfiguration; e != nil {
		out.KMSKey = e.KmsKeyName
	}
	if l := d.LinkedDatasetSource; l != nil && l.SourceDataset != nil {
		out.LinkedSource = l.SourceDataset.ProjectId + "." + l.SourceDataset.DatasetId
	}
	if e := d.ExternalDatasetReference; e != nil {
		out.ExternalSource = e.ExternalSource
	}
	project := ""
	if d.DatasetReference != nil {
		project = d.DatasetReference.ProjectId
	}
	for _, a := range d.Access {
		if a != nil {
			out.Access = append(out.Access, fromAccess(a, project))
		}
	}
	return out
}

// projectGroups are the access list's project group words as the IAM
// member prefixes that name the dataset's project.
var projectGroups = map[string]string{
	"projectOwners":  "projectOwner:",
	"projectWriters": "projectEditor:",
	"projectReaders": "projectViewer:",
}

// fromAccess maps an access entry of a dataset in project.
func fromAccess(a *bq.DatasetAccess, project string) Access {
	out := Access{Role: a.Role}
	if a.Condition != nil {
		out.Condition = a.Condition.Title
		if out.Condition == "" {
			out.Condition = a.Condition.Expression
		}
	}
	switch {
	case a.UserByEmail != "":
		kind := "user:"
		if strings.HasSuffix(a.UserByEmail, ".gserviceaccount.com") {
			kind = "serviceAccount:"
		}
		out.Member = kind + a.UserByEmail
	case a.GroupByEmail != "":
		out.Member = "group:" + a.GroupByEmail
	case a.Domain != "":
		out.Member = "domain:" + a.Domain
	case a.IamMember != "":
		out.Member = a.IamMember
	case a.SpecialGroup != "":
		out.Member = a.SpecialGroup // allAuthenticatedUsers
		if prefix, ok := projectGroups[a.SpecialGroup]; ok {
			out.Member = prefix + project
		}
	case a.View != nil:
		out.Authorized = "view " + a.View.ProjectId + "." + a.View.DatasetId + "." + a.View.TableId
	case a.Routine != nil:
		out.Authorized = "routine " + a.Routine.ProjectId + "." + a.Routine.DatasetId + "." + a.Routine.RoutineId
	case a.Dataset != nil && a.Dataset.Dataset != nil:
		out.Authorized = "dataset " + a.Dataset.Dataset.ProjectId + "." + a.Dataset.Dataset.DatasetId
	}
	return out
}

func fromTable(t *bq.TableListTables) Table {
	out := Table{
		Type:            t.Type,
		FriendlyName:    t.FriendlyName,
		Created:         millis(t.CreationTime),
		Expires:         millis(t.ExpirationTime),
		Labels:          t.Labels,
		PartitionFilter: t.RequirePartitionFilter,
	}
	if t.TableReference != nil {
		out.ID = t.TableReference.TableId
	}
	if p := t.TimePartitioning; p != nil {
		out.Partitioning = p.Type
		if p.Field != "" {
			out.Partitioning += " on " + p.Field
		}
		out.PartitionFilter = out.PartitionFilter || p.RequirePartitionFilter
	}
	if p := t.RangePartitioning; p != nil {
		out.Partitioning = "RANGE on " + p.Field
	}
	if t.Clustering != nil {
		out.Clustering = t.Clustering.Fields
	}
	if t.View != nil {
		out.LegacySQL = t.View.UseLegacySql
	}
	return out
}

func fromRoutine(r *bq.Routine) Routine {
	out := Routine{Type: r.RoutineType, Language: r.Language, Created: millis(r.CreationTime), Modified: millis(r.LastModifiedTime)}
	if r.RoutineReference != nil {
		out.ID = r.RoutineReference.RoutineId
	}
	return out
}

func fromModel(m *bq.Model) Model {
	out := Model{
		Type:         m.ModelType,
		FriendlyName: m.FriendlyName,
		Created:      millis(m.CreationTime),
		Modified:     millis(m.LastModifiedTime),
		Expires:      millis(m.ExpirationTime),
		Labels:       m.Labels,
	}
	if m.ModelReference != nil {
		out.ID = m.ModelReference.ModelId
	}
	return out
}

func fromReservation(r *reservation.Reservation) Reservation {
	out := Reservation{
		Name:              lastSeg(r.Name),
		Location:          nameLocation(r.Name),
		Edition:           r.Edition,
		Slots:             r.SlotCapacity,
		IgnoreIdle:        r.IgnoreIdleSlots,
		Concurrency:       r.Concurrency,
		SecondaryLocation: r.SecondaryLocation,
		Created:           parseTime(r.CreationTime),
		Updated:           parseTime(r.UpdateTime),
		Labels:            r.Labels,
	}
	if a := r.Autoscale; a != nil {
		out.MaxSlots = a.MaxSlots
		out.CurrentSlots = a.CurrentSlots
	}
	return out
}

func fromCommitment(c *reservation.CapacityCommitment) Commitment {
	out := Commitment{
		Name:        lastSeg(c.Name),
		Location:    nameLocation(c.Name),
		Edition:     c.Edition,
		Slots:       c.SlotCount,
		Plan:        c.Plan,
		RenewalPlan: c.RenewalPlan,
		State:       c.State,
		Start:       parseTime(c.CommitmentStartTime),
		End:         parseTime(c.CommitmentEndTime),
	}
	if c.FailureStatus != nil {
		out.Failure = c.FailureStatus.Message
	}
	return out
}

func fromTransfer(t *transfer.TransferConfig) Transfer {
	out := Transfer{
		ID:          lastSeg(t.Name),
		Location:    nameLocation(t.Name),
		DisplayName: t.DisplayName,
		DataSource:  t.DataSourceId,
		Dataset:     t.DestinationDatasetId,
		Schedule:    t.Schedule,
		Disabled:    t.Disabled,
		State:       t.State,
		NextRun:     parseTime(t.NextRunTime),
		Updated:     parseTime(t.UpdateTime),
		Topic:       t.NotificationPubsubTopic,
	}
	if o := t.ScheduleOptions; o != nil && o.DisableAutoScheduling {
		out.Schedule = "on demand"
	}
	if t.OwnerInfo != nil {
		out.Owner = t.OwnerInfo.Email
	}
	if t.Error != nil {
		out.Error = t.Error.Message
	}
	if e := t.EncryptionConfiguration; e != nil {
		out.KMSKey = e.KmsKeyName
	}
	return out
}

func fromConnection(c *connection.Connection) Connection {
	out := Connection{
		ID:           lastSeg(c.Name),
		Location:     nameLocation(c.Name),
		FriendlyName: c.FriendlyName,
		Description:  c.Description,
		Created:      millis(c.CreationTime),
		Modified:     millis(c.LastModifiedTime),
		KMSKey:       c.KmsKeyName,
	}
	switch {
	case c.CloudSql != nil:
		out.Kind = strings.TrimSpace("Cloud SQL " + c.CloudSql.Type)
		out.Identity = c.CloudSql.ServiceAccountId
		out.Target = c.CloudSql.InstanceId
		if c.CloudSql.Database != "" {
			out.Target += " / " + c.CloudSql.Database
		}
	case c.CloudSpanner != nil:
		out.Kind = "Cloud Spanner"
		out.Target = c.CloudSpanner.Database
		if c.CloudSpanner.DatabaseRole != "" {
			out.Identity = "database role " + c.CloudSpanner.DatabaseRole
		}
	case c.CloudResource != nil:
		out.Kind = "Cloud resource"
		out.Identity = c.CloudResource.ServiceAccountId
	case c.Spark != nil:
		out.Kind = "Spark"
		out.Identity = c.Spark.ServiceAccountId
	case c.Aws != nil:
		out.Kind = "AWS"
		if r := c.Aws.AccessRole; r != nil {
			out.Identity = r.Identity
			out.Target = r.IamRoleId
		}
	case c.Azure != nil:
		out.Kind = "Azure"
		out.Identity = c.Azure.Identity
		if c.Azure.CustomerTenantId != "" {
			out.Target = "tenant " + c.Azure.CustomerTenantId
		}
	case c.SalesforceDataCloud != nil:
		out.Kind = "Salesforce Data Cloud"
		out.Identity = c.SalesforceDataCloud.Identity
		out.Target = c.SalesforceDataCloud.InstanceUri
	case c.Configuration != nil:
		out.Kind = "Connector"
	}
	return out
}

func locationName(project, location string) string {
	return "projects/" + project + "/locations/" + location
}

// nameLocation reads the location from projects/p/locations/l/....
func nameLocation(name string) string {
	_, rest, ok := strings.Cut(name, "/locations/")
	if !ok {
		return ""
	}
	loc, _, _ := strings.Cut(rest, "/")
	return loc
}

// millis reads the API's milliseconds since the epoch; 0 is unknown.
func millis(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func lastSeg(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}
