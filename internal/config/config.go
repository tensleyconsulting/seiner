package config

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// IconMode selects how product icons are requested from the renderer factory.
type IconMode string

const (
	IconAuto  IconMode = "auto"
	IconText  IconMode = "text"
	IconNone  IconMode = "none"
	IconKitty IconMode = "kitty"
)

// Config is process configuration. Flags override Google's project and
// impersonation environment variables, which override the config file,
// which overrides defaults.
type Config struct {
	Project    string
	ProjectSet bool
	// InitialResource is the project, folder, or organization the explorer
	// starts on, by resource name (projects/p, folders/123,
	// organizations/456); "" when none was set. A project here also sets
	// Project and ProjectSet. --project and the project environment
	// variables set it too.
	InitialResource string
	Impersonate     string
	// Demo shows the built-in demo catalog instead of live data. Without
	// it, nothing falls back to the demo catalog.
	Demo bool
	// AWLocations are the Assured Workloads locations to list, e.g. "us".
	// The API cannot list them; empty skips Assured Workloads.
	AWLocations []string
	DataSource  string
	LogFile     string
	IconMode    IconMode
	// IconsDir holds custom icons, <name>.svg or <name>.png, that replace
	// the built-in artwork one name at a time; "" uses only the built-in
	// set. ListIcons prints the names.
	IconsDir    string
	ListIcons   bool
	ShowVersion bool
	Debug       bool
	// GoogleServiceAccounts lists the service accounts Google created
	// (default accounts and service agents) alongside yours at startup.
	// The explorer toggles it.
	GoogleServiceAccounts bool
	// Intro plays the startup animation while the first scope loads. The
	// zero value is off so tests and embedders opt in; Load turns it on.
	Intro bool
	// File is the config file that was read; "" when there was none.
	File string
}

// defaultLogFile is seiner.log under $XDG_STATE_HOME/seiner, or
// ~/.local/state/seiner when that is unset or relative, as the XDG base
// directory spec says. A per-user directory keeps the log, which holds
// account emails and Console URLs, away from other users of a shared host.
// It is "" when there is no home directory, which leaves logging off.
func defaultLogFile() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(dir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "seiner", "seiner.log")
}

// demoProject is the demo catalog's project, where --demo starts when no
// project was set.
const demoProject = "demo-project"

// Load parses flags from args (typically os.Args[1:]), Google's project and
// impersonation environment variables, and the config file (see Path).
func Load(args []string) (Config, error) {
	var f struct {
		config, project, initial, impersonate, logFile, icons, iconsDir, awLocations string
		demo, version, debug, noIntro, listIcons                                     bool
	}
	fset := flag.NewFlagSet("seiner", flag.ContinueOnError)
	fset.SetOutput(os.Stderr)
	fset.StringVar(&f.config, "config", "", "config file (default $XDG_CONFIG_HOME/seiner/config.yaml)")
	fset.StringVar(&f.project, "project", "", "GCP project id")
	fset.StringVar(&f.initial, "initial-resource", "", "project, folder, or organization to start on: projects/ID, folders/ID, or organizations/ID")
	fset.StringVar(&f.impersonate, "impersonate", "", "service account to impersonate")
	fset.StringVar(&f.logFile, "log-file", defaultLogFile(), "log file path (never stdout)")
	fset.StringVar(&f.icons, "icons", string(IconAuto), "icon mode: auto, kitty, text, none")
	fset.StringVar(&f.iconsDir, "icons-dir", "", "directory of custom icons (NAME.svg or NAME.png) that replace the built-in ones")
	fset.BoolVar(&f.listIcons, "list-icons", false, "print the icon names --icons-dir can replace, and exit")
	fset.BoolVar(&f.demo, "demo", false, "browse the built-in demo catalog instead of live data")
	fset.BoolVar(&f.version, "version", false, "print version and exit")
	fset.BoolVar(&f.debug, "debug", false, "write a fresh debug log (never stdout) and print its path on stderr")
	fset.StringVar(&f.awLocations, "aw-locations", "", "comma-separated Assured Workloads locations to list, e.g. us,europe-west3")
	fset.BoolVar(&f.noIntro, "no-intro", false, "skip the startup animation")
	if err := fset.Parse(args); err != nil {
		return Config{}, err
	}
	set := map[string]bool{}
	fset.Visit(func(fl *flag.Flag) { set[fl.Name] = true })

	path, explicit := f.config, set["config"]
	if !explicit {
		path = Path()
	}
	file, err := ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist) && !explicit:
		path = ""
	case err != nil:
		return Config{}, err
	}

	if file.Project != "" {
		return Config{}, fmt.Errorf("%s: project is now initial-resource: projects/%s", path, file.Project)
	}
	cfg := Config{
		Impersonate:           file.Impersonate,
		LogFile:               defaultLogFile(),
		IconMode:              IconAuto,
		AWLocations:           file.AWLocations,
		GoogleServiceAccounts: file.GoogleServiceAccounts,
		Intro:                 !file.NoIntro,
		File:                  path,
	}
	start := file.InitialResource
	if file.LogFile != "" {
		cfg.LogFile = expandHome(file.LogFile)
	}
	if file.Icons != "" {
		if cfg.IconMode, err = parseIconMode(file.Icons); err != nil {
			return Config{}, fmt.Errorf("%s: %w", path, err)
		}
	}
	cfg.IconsDir = expandHome(file.IconsDir)

	if v := firstEnv("GOOGLE_CLOUD_PROJECT", "GCP_PROJECT", "CLOUDSDK_CORE_PROJECT"); v != "" {
		start = "projects/" + v
	}
	cfg.Impersonate = getEnv("GOOGLE_IMPERSONATE_SERVICE_ACCOUNT", cfg.Impersonate)
	awLocations := strings.Join(cfg.AWLocations, ",")

	if set["project"] && set["initial-resource"] {
		return Config{}, errors.New("set --project or --initial-resource, not both")
	}
	if set["project"] && f.project != "" {
		start = "projects/" + f.project
	}
	if set["initial-resource"] {
		start = f.initial
	}
	if set["impersonate"] {
		cfg.Impersonate = f.impersonate
	}
	if set["log-file"] {
		cfg.LogFile = f.logFile
	}
	if set["icons"] {
		if cfg.IconMode, err = parseIconMode(f.icons); err != nil {
			return Config{}, err
		}
	}
	if set["icons-dir"] {
		cfg.IconsDir = f.iconsDir
	}
	if set["aw-locations"] {
		awLocations = f.awLocations
	}
	if set["no-intro"] {
		cfg.Intro = !f.noIntro
	}
	cfg.Demo = f.demo
	cfg.ListIcons = f.listIcons
	cfg.ShowVersion = f.version
	cfg.Debug = f.debug

	if start != "" {
		if cfg.InitialResource, err = parseResource(start); err != nil {
			return Config{}, err
		}
		cfg.Project, _ = strings.CutPrefix(cfg.InitialResource, "projects/")
		if cfg.Project == cfg.InitialResource {
			cfg.Project = ""
		}
	}
	cfg.ProjectSet = cfg.Project != ""
	if cfg.Project == "" && cfg.Demo {
		cfg.Project = demoProject
	}
	if cfg.AWLocations, err = parseLocations(awLocations); err != nil {
		return Config{}, err
	}
	if err := checkIconsDir(cfg.IconsDir); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// checkIconsDir fails when an icons directory is set but is not a readable
// directory, so a typo is reported before the TUI starts instead of quietly
// showing the built-in icons.
func checkIconsDir(dir string) error {
	if dir == "" {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("icons-dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("icons-dir: %s is not a directory", dir)
	}
	return nil
}

// parseLocations splits a comma-separated location list, dropping blanks and
// repeats. A location is lowercase letters, digits, and hyphens.
func parseLocations(s string) ([]string, error) {
	var out []string
	for loc := range strings.SplitSeq(s, ",") {
		loc = strings.ToLower(strings.TrimSpace(loc))
		if loc == "" || slices.Contains(out, loc) {
			continue
		}
		if strings.Trim(loc, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
			return nil, fmt.Errorf("invalid --aw-locations entry %q (want a location such as us or europe-west3)", loc)
		}
		out = append(out, loc)
	}
	return out, nil
}

// parseResource checks a project, folder, or organization resource name:
// projects/ID, folders/ID, or organizations/ID. A project id may be
// domain-scoped (example.com:my-proj).
func parseResource(s string) (string, error) {
	s = strings.TrimSpace(s)
	coll, id, _ := strings.Cut(s, "/")
	switch coll {
	case "projects", "folders", "organizations":
	default:
		id = ""
	}
	if id == "" || strings.Trim(id, "abcdefghijklmnopqrstuvwxyz0123456789-.:") != "" {
		return "", fmt.Errorf("invalid initial resource %q (want projects/ID, folders/ID, or organizations/ID)", s)
	}
	return s, nil
}

func parseIconMode(s string) (IconMode, error) {
	switch IconMode(strings.ToLower(strings.TrimSpace(s))) {
	case IconAuto, IconText, IconNone, IconKitty:
		return IconMode(strings.ToLower(strings.TrimSpace(s))), nil
	default:
		return "", fmt.Errorf("invalid --icons %q (want auto, kitty, text, or none)", s)
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok && v != "" {
			return v
		}
	}
	return ""
}

// expandHome replaces a leading ~/ in path with the home directory.
func expandHome(path string) string {
	rest, ok := strings.CutPrefix(path, "~/")
	if !ok {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, rest)
}
