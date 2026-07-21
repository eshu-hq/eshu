# ghactionsref

## Purpose

`ghactionsref` is the single implementation of GitHub Actions `uses:`
reference splitting and full-commit-SHA pin classification. Issue #5372
exposes the GitHub Actions action/workflow `@ref` as a normalized pin signal
(`ref_value` + `ref_pinned`) on the deployment-evidence artifact surface
instead of leaving it unstructured inside `matched_value`. Two independent
call sites need to agree on exactly how a ref splits and exactly which refs
count as pinned: the reducer/graph-projection path
(`go/internal/relationships`, `go/internal/reducer`,
`go/internal/storage/cypher`) and the query/read-model path
(`go/internal/query`). This package is the one place that logic lives so the
two paths cannot re-diverge.

## Ownership boundary

Owns `Parse` (ref-string splitting) and `Pinned` (full-hex classification).
Nothing else. It has no knowledge of evidence facts, graph nodes, Postgres
rows, or HTTP/MCP response shapes -- those stay owned by the packages that
call in.

## Exported surface

- `Parse(raw string) (repo, path, refValue string)` -- splits a `uses:` value
  or local reusable-workflow path into its repository slug, in-repo path, and
  `@ref` value. Returns empty strings for any component that is not present;
  never fabricates a ref when none exists.
- `Pinned(refValue string) bool` -- true if and only if `refValue` is a
  full-length commit SHA: exactly 40 or exactly 64 hexadecimal characters
  (case-insensitive). Everything else -- branch, tag, abbreviated SHA, or an
  empty string -- is `false`.

See `doc.go` for the full godoc contract.

## Dependencies

Standard library only (`strings`). Zero imports from `go/internal/*` --
confirmed by `go list -deps` showing no repository-internal package in this
package's dependency graph. This is deliberate: it is what lets both the
reducer/graph-projection path and the query/read-model path import it without
creating an import cycle between packages that do not otherwise depend on
each other.

## Telemetry

This package emits no metrics, spans, or logs. It is a pure string-parsing
library; the callers that use its output to write graph properties or HTTP/MCP
response fields own their own telemetry.

## Gotchas / invariants

- `Pinned` deliberately does not classify branch vs. tag. Both are ref strings
  that Eshu's static extraction cannot statically distinguish (resolving which
  one a name refers to requires calling GitHub), and a tag is mutable
  regardless of which one it is. Full-commit-SHA immutability is the only
  claim this package makes. Do not add a `ref_kind` classification without
  re-reading issue #5372's rationale first.
- An abbreviated/short SHA (fewer than 40 hex characters) is conservatively
  `Pinned() == false`, not `true`. A short SHA is not guaranteed unique and
  can be reassigned across a mirror or fork; treating it as pinned would
  fabricate a safety guarantee the string does not prove.
- `Parse` on a value with no `@` segment returns an empty `refValue`, not a
  fabricated one. Local `./` reusable workflows and Docker actions have no
  ref at all -- callers must omit `ref_value`/`ref_pinned` entirely for those,
  never default them.
- This package must stay import-free of `go/internal/*`. Adding an import
  here reopens the exact import-cycle risk the package exists to avoid.

## Related docs

- `docs/public/reference/relationship-mapping-evidence.md` -- GitHub Actions
  evidence family and the `ref_value`/`ref_pinned` contract
- `docs/public/reference/http-api/evidence-and-supply-chain.md` -- the
  `deployment_evidence.artifacts[]` response shape these fields appear on
