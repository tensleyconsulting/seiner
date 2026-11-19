# Contributing to Seiner

Thanks for your interest. Read [docs/architecture.md](docs/architecture.md)
before changing structure or adding a feature; [AGENTS.md](AGENTS.md) lists
the conventions the code follows, for people as much as for coding agents.

## Build and test

```bash
make check   # go test, go vet, and the Google-import boundary check
make race    # adapter and app goroutine tests with -race
make build   # ./seiner
```

Run `gofmt` on what you change. `./seiner --demo` browses a built-in catalog
without credentials, which is the quickest way to see a UI change.

## Ground rules

- Seiner is read-only. Do not add anything that changes or deletes a cloud
  resource.
- Unit tests must not need live Google credentials. Inject fake APIs.
- Google client libraries stay under `internal/gcp/adapters`; `internal/ui`
  and `internal/app` must not import them. `make boundary` checks this.
- The built-in icons are original artwork. Do not add vendor product icons
  or logos; draw new icons to the rules in
  `internal/ui/icons/assets/README.md`.
- Do not put real project IDs, account emails, or credentials in fixtures,
  tests, or docs.

## Pull requests

- Work on a `feat/...` branch.
- Describe the change, and end the description with a Testing section that
  says plainly which paths ran only against fixtures and fakes, not a live
  project.
- For a new Google Cloud product, open an issue first to agree which
  resource types to show and where they sit in the navigation pane.

## License

Contributions are accepted under the [Apache License 2.0](LICENSE), the
project's license.
