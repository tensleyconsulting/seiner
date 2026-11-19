# Agent instructions

This repository is Seiner (command `seiner`), a terminal UI for browsing Google Cloud resources
on Linux and macOS, locally or over SSH.
Read `docs/architecture.md` before changing structure or adding features.

## Agent-neutral layout

This repo works with any coding agent. `AGENTS.md` is the one instruction
file. Skills live in `.agents/skills/`;
`.claude/skills`, `.gemini/skills`, and `.opencode/skills` are symlinks to
it. Edit skills only under `.agents/skills/`, and do not name a specific
agent in project docs.

If `AGENTS.local.md` exists in the repository root, read it as well. It
holds instructions for one checkout's environment, such as how to open
pull requests on its remote, and is not committed (`.gitignore`).

## Commands

```bash
go test ./...
go vet ./...
go build -o seiner ./cmd/seiner
```

Or `make check` (test, vet, and the Google-import boundary check), `make
build`, `make race`. See the `Makefile` for all targets.

Do not leave the tree in a non-building state.

## Module and stack

- Module: `github.com/tensleyconsulting/seiner`
- Entry: `cmd/seiner`
- Charm v2 only: `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`
- Go 1.26+

Charm v2 is not v1. In particular:

- `View() tea.View`, not `View() string`
- `tea.KeyPressMsg`, not v1 `tea.KeyMsg` as a struct
- Alt screen is `view.AltScreen = true`, not `tea.WithAltScreen()`
- Space is `"space"`, not `" "`

See `.agents/skills/charm-v2/SKILL.md`.

## Boundaries

- `internal/ui` and `internal/app` must not import Google Cloud client libraries.
- Resource models must not contain Kitty graphics escape sequences.
- UI requests icons by `model.ServiceType` through `icons.Renderer`.
- Theme colors live in `internal/ui/styles`. Do not hard-code ANSI colors in components.
- Keys live in `internal/ui/keys`. Do not scatter key string literals in components.
- Log with `log/slog` to a file. Never write logs to stdout (it corrupts the TUI).
- Unit tests must not require live Google credentials. Inject fake APIs.

## Catalogs and credentials

- Construct catalogs in `cmd/seiner` via `gcp.Open`. `internal/app` depends on
  `model.Catalog` only.
- Google client libraries stay under `internal/gcp/adapters`, one package per product.
- Resolve credentials (ADC, impersonation) in `internal/gcp`. Pass
  `model.Identity` into the TUI; do not put Google types in the UI model.
- Auth errors are a banner. Do not `os.Exit` after the TUI would have started.
- Only `--demo` opens the in-memory catalog. Missing ADC, no project, or a
  failed live catalog show no catalog and a banner or notice; never fall
  back to demo data. Do not mix fake providers into a live catalog.
- Do not silently live-query `demo-project` when the user did not set a project,
  and do not silently switch the demo catalog to the ADC project id.

## Actions

- Adapters are read-only. Do not add destructive mutations.
- Copy uses OSC 52 (`tea.SetClipboard`), not a remote `xclip`/`xdg-open`.
- Console URLs live on `model.Resource`; the UI does not hard-code GCP paths.
- JSON copy uses an explicit field allowlist; `Resource` has no raw API payload.
  A value the user copies but does not read in details (an HSM attestation,
  a PEM chain) goes in `Resource.Copies`, which JSON, search, and logs omit.

## Icons

- Icon sources are SVGs in `internal/ui/icons/assets/`, rasterized in memory at
  startup. Do not commit PNG icons. README screenshots live in `docs/images/`.
- The built-in icons are original artwork. Do not add vendor product icons or
  logos; users load those with `--icons-dir`. Draw new icons to the rules in
  `internal/ui/icons/assets/README.md`.

## Adding a GCP service

Follow `.agents/skills/add-gcp-service/SKILL.md`.

Before building, research what the product's API can list and put the open
decisions to the user: which resource types to show, where they sit in the
tree, and anything the API cannot do directly. Build only after they answer.

## Commits and pull requests

- Work on a `feat/...` branch. Commit, push, and open a PR only when the user
  asks.
- A PR description summarizes the change and ends with a Testing section.
  That section says plainly which paths ran only against fixtures and fakes,
  not a live project.

## Style

- Idiomatic Go, `gofmt`, table-driven tests
- Small focused packages
- Interfaces only at genuine boundaries (GCP ↔ Resource, Resource ↔ UI, UI ↔ icons, UI ↔ capabilities)
