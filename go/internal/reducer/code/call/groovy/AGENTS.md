# groovy — agent instructions (issue #6061)

Read `code/call/shared/AGENTS.md` first for the `EntityIndex` invariant this
package depends on.

## Invariants

- Never import `code/call` or a sibling language leaf, including
  `code/call/jvm` — Groovy's resolver is repo-scoped type inference only, not
  import-bound receiver resolution.
- `groovyClassQualifiedCandidateNames` returns `type_inferred` provenance
  only; do not widen it to claim `import_binding` without adding real
  import-path evidence.
- Prove changes with `go test ./internal/reducer/code/call/... -count=1`
  and the Groovy resolver tests in `code/call`.
