# Internal Docs

This directory holds maintainer-only documentation for Eshu.

- Files in `docs/internal/` are not part of the public MkDocs site.
- Public docs must live under `docs/public/` and be linked from `docs/mkdocs.yml`.
- Source-tree package orientation now lives in the Go module under `go/internal/`; internal docs should avoid re-documenting that layout unless a decision record is needed.
- Internal specs and plans should be updated or removed when the active codebase changes. Treat them as current guidance only when they still match the repo.
- `docs-change-tally.md` records each cleanup pass: created docs, modified
  docs, deleted docs, and the remaining review backlog.
