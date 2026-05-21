# Documentation Layout

This directory separates public documentation from maintainer notes and support
artifacts.

| Path | Purpose |
| --- | --- |
| `docs/public/` | Public MkDocs content. Pages in this directory can appear on the published documentation site. |
| `docs/mkdocs.yml` | MkDocs configuration, navigation, theme, and build settings. |
| `docs/site/` | Generated MkDocs build output. Do not edit this directory by hand. |
| `docs/internal/` | Maintainer-only notes that are not part of the public site. |
| `docs/openapi/` | OpenAPI contracts and supporting API artifacts. |
| `docs/dashboards/` | Observability dashboard JSON. |

Public docs should live under `docs/public/` and be linked from
`docs/mkdocs.yml` when they should appear in the site navigation.

Historical plans and proof notes are not a stable documentation surface. Extract
durable decisions, workflows, diagrams, and performance learnings into
`docs/public/`, `docs/internal/`, or package-local docs, then delete the stale
working note.

Build the public docs with:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```
