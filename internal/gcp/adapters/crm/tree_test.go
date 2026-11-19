package crm

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

type fakeAPI struct {
	projects []Project
	folders  map[string]Folder
	orgs     map[string]Org
	err      error
	// lookupErr is what GetFolder and GetOrg return for a name not in
	// folders or orgs; nil means errDenied.
	lookupErr error
}

// errDenied stands in for a permission-denied read; isDenied expects it.
var errDenied = errors.New("permission denied")

func isDenied(err error) bool { return errors.Is(err, errDenied) }

func (f fakeAPI) missing(string) error {
	if f.lookupErr != nil {
		return f.lookupErr
	}
	return errDenied
}

func (f fakeAPI) SearchProjects(context.Context) ([]Project, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.projects, nil
}

func (f fakeAPI) GetFolder(_ context.Context, name string) (Folder, error) {
	if fol, ok := f.folders[name]; ok {
		return fol, nil
	}
	return Folder{}, f.missing(name)
}

func (f fakeAPI) GetOrg(_ context.Context, name string) (Org, error) {
	if o, ok := f.orgs[name]; ok {
		return o, nil
	}
	return Org{}, f.missing(name)
}

func TestBuildSingleOrg(t *testing.T) {
	t.Parallel()
	api := fakeAPI{
		projects: []Project{
			{ID: "prj-a", DisplayName: "Alpha", Parent: "folders/1"},
			{ID: "prj-b", DisplayName: "Beta", Parent: "folders/1"},
		},
		folders: map[string]Folder{
			"folders/1": {Name: "folders/1", DisplayName: "platform", Parent: "organizations/9"},
		},
		orgs: map[string]Org{
			"organizations/9": {Name: "organizations/9", DisplayName: "example"},
		},
	}
	root, err := Build(context.Background(), api, isDenied)
	if err != nil {
		t.Fatal(err)
	}
	if root.Kind != model.KindOrg || root.Name != "example" {
		t.Fatalf("root %+v", root)
	}
	if _, ok := root.FindScope("prj-a"); !ok {
		t.Fatal("prj-a")
	}
	if _, ok := root.Find("projects/prj-b"); !ok {
		t.Fatal("prj-b")
	}
	path := root.PathTo("projects/prj-a")
	if len(path) != 3 {
		t.Fatalf("path %d", len(path))
	}
}

func TestBuildOrphanProjects(t *testing.T) {
	t.Parallel()
	api := fakeAPI{
		projects: []Project{
			{ID: "one", DisplayName: "One"},
			{ID: "two", DisplayName: "Two"},
		},
	}
	root, err := Build(context.Background(), api, isDenied)
	if err != nil {
		t.Fatal(err)
	}
	if root.Kind != model.KindRoot {
		t.Fatalf("kind %s", root.Kind)
	}
	if len(root.Children) != 2 {
		t.Fatalf("children %d", len(root.Children))
	}
}

func TestBuildEmpty(t *testing.T) {
	t.Parallel()
	_, err := Build(context.Background(), fakeAPI{}, isDenied)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildCarriesProjectDetails(t *testing.T) {
	t.Parallel()
	created := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)
	fcreated := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	api := fakeAPI{
		projects: []Project{{ID: "prj-a", Number: "111", DisplayName: "Alpha", Parent: "folders/1",
			Created: created, Labels: map[string]string{"env": "dev"}}},
		folders: map[string]Folder{"folders/1": {Name: "folders/1", DisplayName: "platform", Created: fcreated}},
	}
	root, err := Build(context.Background(), api, isDenied)
	if err != nil {
		t.Fatal(err)
	}
	if !root.Created.Equal(fcreated) {
		t.Errorf("folder created %v", root.Created)
	}
	p, ok := root.Find("projects/prj-a")
	if !ok {
		t.Fatal("prj-a")
	}
	if p.Number != "111" || !p.Created.Equal(created) || p.Labels["env"] != "dev" {
		t.Errorf("project %+v", p)
	}
}

func TestBuildDeepFolderChain(t *testing.T) {
	t.Parallel()
	api := fakeAPI{
		projects: []Project{{ID: "prj-a", Parent: "folders/3"}},
		folders: map[string]Folder{
			"folders/3": {Name: "folders/3", DisplayName: "team", Parent: "folders/2"},
			"folders/2": {Name: "folders/2", DisplayName: "prod", Parent: "folders/1"},
			"folders/1": {Name: "folders/1", DisplayName: "platform", Parent: "organizations/9"},
		},
		orgs: map[string]Org{"organizations/9": {Name: "organizations/9", DisplayName: "example"}},
	}
	root, err := Build(context.Background(), api, isDenied)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, n := range root.PathTo("projects/prj-a") {
		names = append(names, n.Name)
	}
	if want := []string{"example", "platform", "prod", "team", "prj-a"}; !slices.Equal(names, want) {
		t.Fatalf("path %v, want %v", names, want)
	}
}

// An unreadable folder is a stub either way. A denied read is expected and
// quiet; any other failure returns the tree with a partial error.
func TestBuildUnreadableFolderIsStub(t *testing.T) {
	t.Parallel()
	errUnavailable := errors.New("503 service unavailable")
	tests := []struct {
		name        string
		lookupErr   error
		expected    func(error) bool
		wantPartial bool
	}{
		{name: "denied", lookupErr: errDenied, expected: isDenied},
		{name: "transient", lookupErr: errUnavailable, expected: isDenied, wantPartial: true},
		{name: "nil expected reports all", lookupErr: errDenied, wantPartial: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			api := fakeAPI{
				projects:  []Project{{ID: "prj-a", DisplayName: "Alpha", Parent: "folders/1"}},
				lookupErr: tt.lookupErr,
			}
			root, err := Build(context.Background(), api, tt.expected)
			if got := model.IsPartial(err); got != tt.wantPartial || (!tt.wantPartial && err != nil) {
				t.Fatalf("err = %v, want partial %v", err, tt.wantPartial)
			}
			if tt.wantPartial && !errors.Is(err, tt.lookupErr) {
				t.Fatalf("err = %v, want it to wrap %v", err, tt.lookupErr)
			}
			if root.ID != "folders/1" || root.Name != "1" || root.Kind != model.KindFolder {
				t.Fatalf("root %+v", root)
			}
			if _, ok := root.Find("projects/prj-a"); !ok {
				t.Fatal("prj-a")
			}
		})
	}
}

// cancelingAPI ends the build's context during the first folder lookup, as
// a timeout partway through a large org would.
type cancelingAPI struct {
	fakeAPI
	cancel context.CancelFunc
}

func (c cancelingAPI) GetFolder(ctx context.Context, _ string) (Folder, error) {
	c.cancel()
	return Folder{}, ctx.Err()
}

func TestBuildFailsWhenContextEndsDuringLookups(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api := cancelingAPI{
		fakeAPI: fakeAPI{projects: []Project{
			{ID: "prj-a", Parent: "folders/1"},
			{ID: "prj-b", Parent: "folders/2"},
		}},
		cancel: cancel,
	}
	if root, err := Build(ctx, api, isDenied); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v root=%+v; want context.Canceled and no tree", err, root)
	}
}
