# Documentation Update Guide

This maintainer-only guide defines the required workflow for changing Eshu's
documentation site.

## Surfaces

| Surface | Purpose |
| --- | --- |
| `docs/public/` | Public product, operator, guide, and reference docs. |
| `docs/internal/` | Maintainer-only cleanup tracking and workflow notes. |
| `docs/mkdocs.yml` | Public site navigation and build configuration. |
| `go/**/README.md` | Package-local ownership and maintainer context. |
| `go/**/AGENTS.md` | Harness-loaded scoped agent rules. |

Public pages must live under `docs/public/`, use lower-case kebab-case names,
and be wired into `docs/mkdocs.yml` when they are part of the site.

## Editing Flow

1. Read the local docs that own the surface before editing.
2. Compare claims against code, manifests, scripts, tests, or generated specs.
3. Remove duplicated or stale prose instead of adding another layer.
4. Update navigation when public pages are added, moved, or deleted.
5. Update `docs/internal/docs-change-tally.md` after each cleanup pass.
6. Run the smallest focused verifier for the touched surface.
7. Run the broad acceptance gates before commit.

Parser docs are owned by `go/internal/parser/registry.go`, parser package code,
and parser tests. Update `docs/public/languages/`, `feature-matrix.md`, and
`support-maturity.md` when parser behavior changes.

## Required Gates

Run the focused gate first:

```bash
cd go
go run ./cmd/eshu docs verify ../docs/public/<path> \
  --limit 1400 --fail-on contradicted,missing_evidence
```

For package docs, also run:

```bash
scripts/verify-package-docs.sh
```

Before calling a docs branch ready, run:

```bash
cd go
go run ./cmd/eshu docs verify ../docs/public --limit 1400 \
  --fail-on contradicted,missing_evidence
go run ./cmd/eshu docs verify .. --limit 2400 \
  --fail-on contradicted,missing_evidence
cd ..
git diff --check
cmp -s AGENTS.md CLAUDE.md
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

Unsupported shell-command claim types from the docs verifier are expected for
some Helm, kubectl, and Terraform examples. Contradicted or missing-evidence
claims must be fixed.

## Deployment

The `Deploy Docs` workflow builds the MkDocs site on `main`. It publishes to
GitHub Pages only when the repository Pages opt-in variable is enabled. Keep
that variable disabled until the repository Pages settings publish from GitHub
Actions.
