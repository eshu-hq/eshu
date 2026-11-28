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
