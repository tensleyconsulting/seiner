package app

import (
	"context"
	"io"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/messages"
)

// Reopener reads credentials again and reopens the catalog, as cmd/seiner
// did at startup. It returns the new catalog (nil when there is still none
// to browse; ident says why), the identity, and the project it opened on.
type Reopener func(ctx context.Context) (src model.Catalog, ident model.Identity, project string)

// WithReopen lets refresh read credentials again when there is no catalog
// or Google has rejected the live credentials, so a fresh sign-in needs no
// restart. The demo catalog has no reopener.
func (m Model) WithReopen(f Reopener) Model {
	m.reopen = f
	return m
}

// reopenTimeout bounds reading credentials and opening the catalog, as the
// startup open is bounded in cmd/seiner.
const reopenTimeout = 30 * time.Second

type reopenedMsg struct {
	src     model.Catalog
	ident   model.Identity
	project string
}

// needsReopen reports whether refresh should read credentials again rather
// than reload: the live credentials were rejected, or there is no catalog.
func (m Model) needsReopen() bool {
	return m.reopen != nil && (m.authErr != nil || m.src == nil)
}

func (m *Model) reopenCmd() tea.Cmd {
	if m.reopening {
		return nil
	}
	m.reopening = true
	m.status.SetFlash("reading credentials…")
	reopen := m.reopen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reopenTimeout)
		defer cancel()
		src, ident, project := reopen(ctx)
		return reopenedMsg{src: src, ident: ident, project: project}
	}
}

// applyReopened takes the result of reading credentials again. With a new
// catalog it swaps it in, closes the old one, clears the auth banner, and
// reloads; without one it shows why.
func (m *Model) applyReopened(msg reopenedMsg) tea.Cmd {
	m.reopening = false
	m.identity = msg.ident
	m.status.Account = msg.ident.DisplayAccount()
	if msg.src == nil {
		slog.Warn("credentials still unavailable", "err", msg.ident.Err, "notice", msg.ident.Notice)
		switch {
		case msg.ident.Err != nil:
			m.status.SetFlash(msg.ident.Banner())
		case msg.ident.Notice != "":
			m.status.SetFlash(msg.ident.Notice)
		}
		m.syncLayout()
		return nil
	}
	slog.Info("catalog reopened", "account", msg.ident.Account, "source", msg.ident.Source, "project", msg.project)
	old := m.src
	m.src = msg.src
	m.authErr = nil
	m.hierFailed = false
	m.status.Catalog = "live"
	if m.scope.ID == "" && msg.project != "" {
		// Started with no project; the reopened credentials name one.
		m.cfg.Project = msg.project
		m.scope = startScope(m.cfg)
		m.tree = m.scope
		m.revealPath(m.scope.ID)
		m.status.Project = m.scope.Name
	}
	m.logf(messages.Info, "Signed in as %s", msg.ident.DisplayAccount())
	m.status.SetFlash("signed in as " + msg.ident.DisplayAccount())

	// A refresh restarts every load with a new generation, so results from
	// the old catalog still in flight are dropped.
	load := m.refreshLoads()
	if old == nil {
		load = m.startupLoads()
	}
	m.rebuildOutline()
	m.syncLayout()
	return tea.Batch(load, m.loadHierarchyCmd(), closeCatalog(old))
}

// Close closes the catalog's clients, if it has any. cmd/seiner calls it
// on the final model once the program exits.
func (m Model) Close() error {
	if c, ok := m.src.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// closeCatalog closes a replaced catalog off the event loop.
func closeCatalog(src model.Catalog) tea.Cmd {
	c, ok := src.(io.Closer)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		if err := c.Close(); err != nil {
			slog.Warn("closing replaced catalog", "err", err)
		}
		return nil
	}
}
