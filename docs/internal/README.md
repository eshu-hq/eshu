# Internal Docs

This directory holds maintainer-only documentation for Eshu.

- Files in `docs/internal/` are not part of the public MkDocs site.
- Files in `docs/archive/` are historical reference material, not active public documentation.
- Public docs must live under `docs/docs/` and be linked from `docs/mkdocs.yml`.
- Source-tree package orientation now lives in the Go module under `go/internal/`; internal docs should avoid re-documenting that layout unless a decision record is needed.
- Internal specs and plans should be updated or removed when the active codebase changes. Treat them as current guidance only when they still match the repo.
