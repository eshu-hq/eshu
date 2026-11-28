# Internal Docs

This directory holds maintainer-only documentation for Eshu.

- Files in `docs/internal/` are not part of the public MkDocs site.
- Public docs live under `docs/public/` and every public `.md` file must be
  linked from `docs/mkdocs.yml` (enforced by `scripts/verify-docs-catalog.sh`).
- Machine-readable validation catalogs live under `specs/` (for example,
  `specs/remote-validation-proofs.v1.yaml`), not as per-slug Markdown stubs.
- Source-tree package orientation lives in package-local docs under `go/`; do
  not re-document that layout here.
- Before adding or editing any page here, load the **`publish`** skill and
  follow [Writing For Agents](writing-for-agents.md) so internal guides stay
  concise, legible, and free of session shorthand.
- Never commit ephemeral PR proof notes, raw `.sql` packets, arbiter fork
  rulings, PR review limits, agent implementation plans, extraction-prep
  drafts, or dated point-in-time reviews here or under `go/`
  (`scripts/verify-no-ephemeral-docs.sh`). Do not append `## Evidence`,
  `## Evidence Notes`, `## Move evidence`, or `## ... (#NNNN)` PR-changelog
  sections to `go/**/*.md` or `docs/public/**/*.md`. Put PR proof in GitHub PR
  descriptions or CI artifacts, and arbiter rulings or extraction checklists in
  GitHub issue comments.
- Internal specs and plans must be updated or removed when the active codebase
  changes. Treat them as current guidance only when they still match the repo.
- The [Correlation Atlas](correlation-atlas/README.md) maps which collector
  feeds which reducer correlation, what starts a run, and what starts it again
  when another source changes.
