# Documentation Update Guide

This is a maintainer-only guide for updating the Eshu docs site.

## Docs structure

Eshu currently has **one** public documentation surface:

- `docs/mkdocs.yml` for site configuration and navigation
- `docs/docs/` for public docs content
- `docs/internal/` for maintainer-only notes
- `docs/archive/` for historical material that should not appear in the public site

## Public docs rules

- public markdown files live under `docs/docs/`
- public filenames use lower-case kebab-case
- public pages must be wired into `docs/mkdocs.yml`
- public docs should not reference removed frontend-hosting flows

## Editing flow

1. update or add Markdown under `docs/docs/`
2. update navigation in `docs/mkdocs.yml`
3. if you changed parser behavior, update the affected language pages and
   parser matrices so they match the Go implementation
4. run the docs tests
5. build the site locally

## Parser documentation ownership

The canonical parser implementation now lives in:

- `go/internal/parser/registry.go`
- `go/internal/parser/*.go`
- `go/internal/parser/*_test.go`

The public parser pages under `docs/docs/languages/`, plus
`feature-matrix.md` and `support-maturity.md`, are checked-in documentation for
that implementation. Update them when the Go parser contract changes.

Common verification:

```bash
cd "$(git rev-parse --show-toplevel)"
cd go
go test ./internal/parser ./internal/collector ./internal/content/shape -count=1
```

## Local verification

```bash
cd docs
mkdocs serve
```

```bash
cd "$(git rev-parse --show-toplevel)"
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

## Build

```bash
cd "$(git rev-parse --show-toplevel)"
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

## GitHub Pages deployment

The `Deploy Docs` workflow always builds the MkDocs site on `main`. It only
publishes to GitHub Pages when the repository variable
`ESHU_GITHUB_PAGES_ENABLED` is set to `true`. Keep that variable unset until the
repository's Pages settings are configured to publish from GitHub Actions.
