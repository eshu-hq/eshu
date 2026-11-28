# Internal Docs

This directory holds maintainer-only documentation for Eshu.

- Files in `docs/internal/` are not part of the public MkDocs site.
- Public docs must live under `docs/public/` and be linked from `docs/mkdocs.yml`.
- Source-tree package orientation lives in package-local docs under `go/`; do
  not re-document that layout here.
- Before adding or editing any page here, load the **`publish`** skill and
  follow [Writing For Agents](writing-for-agents.md) so internal guides stay
  concise, legible, and free of session shorthand.
- Never commit ephemeral PR proof notes, shims, probes, or arbiter rulings here
  (`scripts/verify-no-ephemeral-docs.sh`); put PR proof in GitHub PR descriptions
  or CI artifacts, and arbiter rulings in GitHub issue comments.
- Internal specs and plans must be updated or removed when the active codebase
  changes. Treat them as current guidance only when they still match the repo.
- The [Correlation Atlas](correlation-atlas/README.md) maps which collector
  feeds which reducer correlation, what starts a run, and what starts it again
  when another source changes.
