# Documentation Layout

This repository keeps durable documentation in three clear homes:

| Home | Purpose |
| --- | --- |
| `docs/public/` | Public MkDocs site. Every `.md` file here must appear in `docs/mkdocs.yml` (`scripts/verify-docs-catalog.sh` blocks orphaned pages). |
| `docs/internal/` | Maintainer handbooks and active design docs. Never commit PR proof logs, shims, probes, or arbiter rulings (`scripts/verify-no-ephemeral-docs.sh`). |
| `go/<pkg>/{doc.go,README.md,AGENTS.md}` | Package-local contracts, architecture, telemetry, and contributor rules. |

Supporting directories under `docs/`:

| Path | Purpose |
| --- | --- |
| `docs/mkdocs.yml` | MkDocs navigation, theme, and strict build configuration. |
| `docs/site/` | Generated MkDocs build output. Do not edit by hand. |
| `docs/openapi/` | OpenAPI contracts and supporting API artifacts. |
| `docs/dashboards/` | Observability dashboard JSON. |

## Required Documentation Skills

Good hygiene alone does not make documentation readable. Before creating or
editing pages in this tree, load the matching skills:

- **Public pages (`docs/public/`):** load `publish` for the plain-English,
  anti-slop writing standard, and follow
  [Documentation Style Guide](public/documentation-style-guide.md) (choose the
  Diátaxis page type and wire `docs/mkdocs.yml`).
- **Maintainer pages (`docs/internal/`):** load `publish` and follow
  [Writing For Agents](internal/writing-for-agents.md). Keep only durable
  handbooks and active designs—never commit PR proof notes or arbiter rulings.
- **Package docs (`go/**/{doc.go,README.md,AGENTS.md}`):** load
  `folder-doc-keeper` and `publish`.

Historical plans and proof notes are not stable documentation. Extract durable
decisions, workflows, diagrams, and performance learnings into `docs/public/`,
`docs/internal/`, or package-local docs, then delete stale working notes.

Build the public docs with:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```
