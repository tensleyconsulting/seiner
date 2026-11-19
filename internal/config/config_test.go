package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestMain points the default config file at an empty directory, so a
// developer's own config file cannot change the results.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "seiner-config")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// writeConfig writes body as a config file and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// clearEnv unsets the environment Load reads.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"GOOGLE_CLOUD_PROJECT", "GCP_PROJECT", "CLOUDSDK_CORE_PROJECT", "GOOGLE_IMPERSONATE_SERVICE_ACCOUNT"} {
		t.Setenv(k, "")
	}
}

const fullConfig = `# seiner
initial-resource: projects/file-project
impersonate: file@x.iam.gserviceaccount.com
icons: text
log-file: ~/seiner.log
aw-locations: [us, europe-west3]
no-intro: true
google-service-accounts: true
`

func TestLoadFile(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, fullConfig)
	cfg, err := Load([]string{"--config", path})
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	want := Config{
		Project:               "file-project",
		ProjectSet:            true,
		InitialResource:       "projects/file-project",
		Impersonate:           "file@x.iam.gserviceaccount.com",
		IconMode:              IconText,
		LogFile:               filepath.Join(home, "seiner.log"),
		AWLocations:           []string{"us", "europe-west3"},
		GoogleServiceAccounts: true,
		Intro:                 false,
		File:                  path,
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("got  %+v\nwant %+v", cfg, want)
	}
}

func TestLoadFilePrecedence(t *testing.T) {
	path := writeConfig(t, fullConfig)
	cases := []struct {
		name    string
		env     map[string]string
		args    []string
		project string
		icons   IconMode
		intro   bool
		aw      []string
	}{
		{"file", nil, nil, "file-project", IconText, false, []string{"us", "europe-west3"}},
		{"env project over file", map[string]string{"GOOGLE_CLOUD_PROJECT": "env-project"}, nil,
			"env-project", IconText, false, []string{"us", "europe-west3"}},
		{"flags over env and file", map[string]string{"GOOGLE_CLOUD_PROJECT": "env-project"},
			[]string{"--project", "flag-project", "--icons", "kitty", "--no-intro=false", "--aw-locations", "asia"},
			"flag-project", IconKitty, true, []string{"asia"}},
		{"flag default does not override file", nil, []string{"--demo"}, "file-project", IconText, false, []string{"us", "europe-west3"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			cfg, err := Load(append([]string{"--config", path}, tc.args...))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Project != tc.project || cfg.IconMode != tc.icons || cfg.Intro != tc.intro || !slices.Equal(cfg.AWLocations, tc.aw) {
				t.Fatalf("project %q icons %q intro %v aw %v", cfg.Project, cfg.IconMode, cfg.Intro, cfg.AWLocations)
			}
		})
	}
}

func TestLoadDefaultFile(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if cfg, err := Load(nil); err != nil || cfg.File != "" {
		t.Fatalf("no default file: %q %v", cfg.File, err)
	}
	path := filepath.Join(dir, "seiner", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("initial-resource: projects/xdg-project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(nil)
	if err != nil || cfg.File != path || cfg.Project != "xdg-project" {
		t.Fatalf("default file: %+v %v", cfg, err)
	}
}

func TestLoadFileErrors(t *testing.T) {
	clearEnv(t)
	missing := filepath.Join(t.TempDir(), "nope.yaml")
	if _, err := Load([]string{"--config", missing}); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing --config file: %v", err)
	}
	for _, body := range []string{"projcet: typo\n", "icons: emoji\n", "aw-locations: [us/x]\n", "project: [a, b]\n", "a: b: c\n"} {
		if _, err := Load([]string{"--config", writeConfig(t, body)}); err == nil {
			t.Errorf("%q: expected error", body)
		}
	}
	for _, body := range []string{"", "# only a comment\n"} {
		if _, err := Load([]string{"--config", writeConfig(t, body)}); err != nil {
			t.Errorf("%q: %v", body, err)
		}
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GCP_PROJECT", "")
	t.Setenv("CLOUDSDK_CORE_PROJECT", "")
	t.Setenv("GOOGLE_IMPERSONATE_SERVICE_ACCOUNT", "")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project != "" {
		t.Errorf("project=%q, want none outside --demo", cfg.Project)
	}
	if cfg.LogFile != defaultLogFile() {
		t.Errorf("log=%q", cfg.LogFile)
	}
	if cfg.IconMode != IconAuto {
		t.Errorf("icons=%q", cfg.IconMode)
	}
	if cfg.ProjectSet {
		t.Fatal("default project is not explicit")
	}
	if cfg.Demo {
		t.Fatal("demo")
	}
}

func TestLoadFlagsOverrideEnv(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "from-env")
	cfg, err := Load([]string{"--project", "from-flag", "--icons", "none", "--log-file", "/tmp/x.log"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project != "from-flag" {
		t.Errorf("project=%q", cfg.Project)
	}
	if cfg.IconMode != IconNone {
		t.Errorf("icons=%q", cfg.IconMode)
	}
	if cfg.LogFile != "/tmp/x.log" {
		t.Errorf("log=%q", cfg.LogFile)
	}
}

func TestLoadInvalidIcons(t *testing.T) {
	_, err := Load([]string{"--icons", "emoji"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadKittyIcons(t *testing.T) {
	cfg, err := Load([]string{"--icons", "kitty"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IconMode != IconKitty {
		t.Errorf("icons=%q", cfg.IconMode)
	}
}

func TestLoadIconsDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fileDir := filepath.Join(home, "icons")
	flagDir := t.TempDir()
	if err := os.Mkdir(fileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := writeConfig(t, "icons-dir: ~/icons\n")
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unset", nil, ""},
		{"file expands ~", []string{"--config", path}, fileDir},
		{"flag over file", []string{"--config", path, "--icons-dir", flagDir}, flagDir},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			cfg, err := Load(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.IconsDir != tc.want {
				t.Fatalf("IconsDir %q, want %q", cfg.IconsDir, tc.want)
			}
		})
	}
}

func TestLoadIconsDirErrors(t *testing.T) {
	clearEnv(t)
	file := filepath.Join(t.TempDir(), "logo.svg")
	if err := os.WriteFile(file, []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(t.TempDir(), "missing"), file} {
		if _, err := Load([]string{"--icons-dir", dir}); err == nil || !strings.Contains(err.Error(), "icons-dir") {
			t.Errorf("%s: err %v, want an icons-dir error", dir, err)
		}
	}
}

func TestLoadListIcons(t *testing.T) {
	clearEnv(t)
	cfg, err := Load([]string{"--list-icons"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ListIcons {
		t.Fatal("ListIcons not set")
	}
}

func TestLoadDebug(t *testing.T) {
	cfg, err := Load([]string{"--debug", "--log-file", "/tmp/seiner-debug.log"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Debug {
		t.Fatal("debug")
	}
	if cfg.LogFile != "/tmp/seiner-debug.log" {
		t.Errorf("log=%q", cfg.LogFile)
	}
}

func TestLoadProjectFromEnv(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "env-project")
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project != "env-project" {
		t.Errorf("project=%q", cfg.Project)
	}
	if !cfg.ProjectSet {
		t.Fatal("env project is explicit")
	}
}

func TestLoadDemoFlag(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		project string
	}{
		{"starts on the demo project", []string{"--demo"}, demoProject},
		{"keeps an explicit project", []string{"--demo", "--project", "p"}, "p"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			cfg, err := Load(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if !cfg.Demo || cfg.Project != tc.project {
				t.Fatalf("demo %v project %q", cfg.Demo, cfg.Project)
			}
		})
	}
}

func TestLoadFixturesFlagIsGone(t *testing.T) {
	if _, err := Load([]string{"--fixtures"}); err == nil {
		t.Fatal("--fixtures was renamed to --demo")
	}
}

func TestLoadImpersonateFlagOverridesEnv(t *testing.T) {
	t.Setenv("GOOGLE_IMPERSONATE_SERVICE_ACCOUNT", "env@x.iam.gserviceaccount.com")
	cfg, err := Load([]string{"--impersonate", "flag@x.iam.gserviceaccount.com"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Impersonate != "flag@x.iam.gserviceaccount.com" {
		t.Errorf("impersonate=%q", cfg.Impersonate)
	}
}

func TestLoadImpersonateFromEnv(t *testing.T) {
	t.Setenv("GOOGLE_IMPERSONATE_SERVICE_ACCOUNT", "env@x.iam.gserviceaccount.com")
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Impersonate != "env@x.iam.gserviceaccount.com" {
		t.Errorf("impersonate=%q", cfg.Impersonate)
	}
}

func TestLoadIntro(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"default on", nil, true},
		{"flag off", []string{"--no-intro"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Intro != tc.want {
				t.Fatalf("intro=%v want %v", cfg.Intro, tc.want)
			}
		})
	}
}

func TestLoadAWLocations(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"unset", nil, nil},
		{"flag", []string{"--aw-locations", " US, europe-west3,,us "}, []string{"us", "europe-west3"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(cfg.AWLocations, tc.want) {
				t.Fatalf("AWLocations %q, want %q", cfg.AWLocations, tc.want)
			}
		})
	}
}

func TestLoadInvalidAWLocations(t *testing.T) {
	for _, v := range []string{"us/../x", "europe west3", "us?x=1"} {
		if _, err := Load([]string{"--aw-locations", v}); err == nil {
			t.Errorf("%q: expected error", v)
		}
	}
}

func TestLoadInitialResource(t *testing.T) {
	folderFile := writeConfig(t, "initial-resource: folders/123\n")
	cases := []struct {
		name     string
		file     string
		env      string // GOOGLE_CLOUD_PROJECT
		args     []string
		resource string
		project  string
		explicit bool // ProjectSet
	}{
		{"none", "", "", nil, "", "", false},
		{"flag folder", "", "", []string{"--initial-resource", "folders/123"}, "folders/123", "", false},
		{"flag organization", "", "", []string{"--initial-resource", "organizations/456"}, "organizations/456", "", false},
		{"flag project", "", "", []string{"--initial-resource", "projects/prj-ops"}, "projects/prj-ops", "prj-ops", true},
		{"domain-scoped project", "", "", []string{"--initial-resource", "projects/example.com:p"}, "projects/example.com:p", "example.com:p", true},
		{"--project", "", "", []string{"--project", "p"}, "projects/p", "p", true},
		{"file", folderFile, "", nil, "folders/123", "", false},
		{"env project over file", folderFile, "env-project", nil, "projects/env-project", "env-project", true},
		{"flag over env project", "", "env-project", []string{"--initial-resource", "organizations/456"}, "organizations/456", "", false},
		{"--project over file", folderFile, "", []string{"--project", "flag-project"}, "projects/flag-project", "flag-project", true},
		{"empty --project is unset", folderFile, "", []string{"--project", ""}, "folders/123", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("GOOGLE_CLOUD_PROJECT", tc.env)
			args := tc.args
			if tc.file != "" {
				args = append([]string{"--config", tc.file}, args...)
			}
			cfg, err := Load(args)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.InitialResource != tc.resource || cfg.Project != tc.project || cfg.ProjectSet != tc.explicit {
				t.Fatalf("resource %q project %q set %v", cfg.InitialResource, cfg.Project, cfg.ProjectSet)
			}
		})
	}
}

func TestLoadInitialResourceErrors(t *testing.T) {
	clearEnv(t)
	for _, args := range [][]string{
		{"--project", "p", "--initial-resource", "folders/1"},
		{"--initial-resource", "123"},
		{"--initial-resource", "prj-ops"},
		{"--initial-resource", "billingAccounts/1"},
		{"--initial-resource", "folders/"},
		{"--initial-resource", "folders/1/x"},
		{"--initial-resource", "projects/My Project"},
		{"--config", writeConfig(t, "project: p\n")},
	} {
		if _, err := Load(args); err == nil {
			t.Errorf("%q: expected error", args)
		}
	}
	_, err := Load([]string{"--config", writeConfig(t, "project: p\n")})
	if err == nil || !strings.Contains(err.Error(), "initial-resource: projects/p") {
		t.Errorf("old project key should say what replaced it: %v", err)
	}
}

// The default config file follows XDG_CONFIG_HOME on every OS, macOS
// included.
func TestPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	tests := []struct {
		name, config, want string
	}{
		{"XDG_CONFIG_HOME", "/srv/config", "/srv/config/seiner/config.yaml"},
		{"unset", "", filepath.Join(home, ".config", "seiner", "config.yaml")},
		{"relative is ignored", "config", filepath.Join(home, ".config", "seiner", "config.yaml")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", tt.config)
			if got := Path(); got != tt.want {
				t.Errorf("Path() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The default log sits in the XDG state directory, not a shared /tmp.
func TestDefaultLogFile(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	tests := []struct {
		name, state, want string
	}{
		{"XDG_STATE_HOME", "/srv/state", "/srv/state/seiner/seiner.log"},
		{"unset", "", filepath.Join(home, ".local", "state", "seiner", "seiner.log")},
		{"relative is ignored", "state", filepath.Join(home, ".local", "state", "seiner", "seiner.log")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", tt.state)
			if got := defaultLogFile(); got != tt.want {
				t.Errorf("defaultLogFile() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadFileRejectsNonRegularAndOversized(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadFile(dir); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("ReadFile(dir) = %v, want not a regular file", err)
	}
	huge := filepath.Join(dir, "huge.yaml")
	if err := os.WriteFile(huge, []byte("no-intro: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(huge, maxConfigFile+1); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(huge); err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("ReadFile(huge) = %v, want byte limit error", err)
	}
}
