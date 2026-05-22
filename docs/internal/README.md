# Internal Docs

This directory holds maintainer-only documentation for Eshu.

- Files in `docs/internal/` are not part of the public MkDocs site.
- Public docs must live under `docs/public/` and be linked from `docs/mkdocs.yml`.
- Source-tree package orientation lives in package-local docs under `go/`; do
  not re-document that layout here.
- Internal specs and plans must be updated or removed when the active codebase
  changes. Treat them as current guidance only when they still match the repo.
- `docs-change-tally.md` records each cleanup pass: created docs, modified
  docs, deleted docs, and the remaining review backlog.
