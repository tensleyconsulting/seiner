package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tensleyconsulting/seiner/internal/app"
	"github.com/tensleyconsulting/seiner/internal/config"
	"github.com/tensleyconsulting/seiner/internal/gcp"
	applog "github.com/tensleyconsulting/seiner/internal/log"
	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/icons"
)

// version is overridden at build time with -ldflags "-X main.version=...".
// When unset (local go build or go install ...@vX.Y.Z), buildVersion reads
// module and VCS metadata embedded by the Go toolchain.
var version = ""

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return resolveVersion(version, nil)
	}
	return resolveVersion(version, info)
}

func resolveVersion(ldflags string, info *debug.BuildInfo) string {
	if ldflags != "" {
		return ldflags
	}
	if info != nil {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
		var rev string
		var dirty bool
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if rev != "" {
			if len(rev) > 7 {
				rev = rev[:7]
			}
			if dirty {
				return "dev (" + rev + "-dirty)"
			}
			return "dev (" + rev + ")"
		}
	}
	return "dev"
}

func main() {
	ver := buildVersion()
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "seiner: %v\n", err)
		os.Exit(2)
	}
	if cfg.ShowVersion {
		fmt.Println(ver)
		return
	}
	if cfg.ListIcons {
		listIcons()
		return
	}

	closeLog, err := applog.Init(cfg.LogFile, cfg.Debug)
	if err != nil {
		// Logging is off for this run; the TUI does not need it.
		fmt.Fprintf(os.Stderr, "seiner: log: %v; continuing without a log\n", err)
	} else {
		defer func() { _ = closeLog() }()
		if cfg.Debug {
			fmt.Fprintf(os.Stderr, "seiner: debug log %s\n", cfg.LogFile)
		}
	}
	if cfg.File != "" {
		slog.Info("config file read", "path", cfg.File)
	}

	openOpts := gcp.OpenOptions{
		Project:         cfg.Project,
		ProjectSet:      cfg.ProjectSet,
		InitialResource: cfg.InitialResource,
		Impersonate:     cfg.Impersonate,
		Demo:            cfg.Demo,
		AWLocations:     cfg.AWLocations,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	opened := gcp.Open(ctx, openOpts)
	cancel()

	cfg.Project = opened.Project
	cfg.DataSource = opened.Source
	if opened.Identity.Err != nil {
		slog.Warn("credentials or live catalog unavailable", "err", opened.Identity.Err, "impersonate", cfg.Impersonate)
	} else {
		slog.Info("catalog opened",
			"account", opened.Identity.Account,
			"source", opened.Source,
			"project", opened.Project,
			"adc_project", opened.Identity.ADCProject,
			"impersonate", opened.Identity.Impersonate,
		)
	}

	// SourceNone has nothing to browse; the TUI shows why instead of a
	// catalog.
	var src model.Catalog
	if opened.Source != gcp.SourceNone {
		src = opened.Catalog
	}
	m := app.New(cfg, ver, src, opened.Identity)
	if !cfg.Demo {
		// Refresh reads credentials again after a sign-in problem, opening
		// the catalog as above.
		m = m.WithReopen(func(ctx context.Context) (model.Catalog, model.Identity, string) {
			r := gcp.Open(ctx, openOpts)
			if r.Source == gcp.SourceNone {
				return nil, r.Identity, r.Project
			}
			return r.Catalog, r.Identity, r.Project
		})
	}
	p := tea.NewProgram(m, m.ProgramOptions()...)
	final, err := p.Run()
	// The model may have swapped in a reopened catalog; close the one it
	// ended with.
	if fm, ok := final.(app.Model); ok {
		if cerr := fm.Close(); cerr != nil {
			slog.Warn("closing clients", "err", cerr)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "seiner: %v\n", err)
		os.Exit(1)
	}
}

// listIcons prints each name an --icons-dir file can use, with what it shows.
func listIcons() {
	for _, name := range icons.Names() {
		label := name.Label()
		switch name {
		case icons.Logo:
			label = "Seiner logo (header and startup intro)"
		case icons.Cursor:
			label = "explorer cursor (graphics only)"
		}
		fmt.Printf("%s\t%s\n", name, label)
	}
}
