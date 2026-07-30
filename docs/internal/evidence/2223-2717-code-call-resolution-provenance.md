# Code-Call Resolution Provenance And Cross-Repo Go Package-Export Resolution (#2223, #2717)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Code-call resolution provenance (#2223)

Each materialized code-call, reference, and Python metaclass row carries a
`resolution_method` from the closed
[ADR #2222](../../../docs/internal/design/2222-resolution-provenance-code-edges.md)
vocabulary (`go/internal/codeprovenance`). `resolveGenericCallee` returns the
resolver branch that produced the match; SCIP rows carry `scip`, metaclass rows
carry `declared`, and the secondary constructor edge carries `type_inferred`.
The method is descriptive, never admissive. Graph persistence of the tiered
confidence derived from this method is owned by #2224.

No-Regression Evidence: `go test ./internal/reducer -run 'ResolutionMethod|MetaclassRowsCarry' -count=1`
and `go test ./internal/codeprovenance -count=1` fail before the resolution
method is threaded out of the resolver and pass after. The change adds one
string key (`resolution_method`) to each already-emitted row and one return
value to `resolveGenericCallee`; it introduces no new fact scan, graph query,
queue domain, lease, worker, or per-call loop. Dedupe keys, row counts, and
intent volume are unchanged because the new field rides on existing rows, so
reducer extraction cost and Postgres intent-write cost are unchanged. No graph
backend write changes land in this PR, so no backend/version timing applies.

No-Observability-Change: this change adds no route, graph query, queue domain,
worker, lease, runtime knob, metric instrument, or metric label. The existing
`code call materialization completed` completion log (fact, repository, and row
counts plus per-phase timings) stays the operator signal; the new field appears
inside existing durable intent payloads.

### Cross-repo Go package-export resolution (#2717)

`resolveGenericCallee` gains a final Go-only branch that resolves a
package-qualified call to a function exported by another repository, emitting the
`cross_repo_export_package` resolution method (tier 0.70). A generation-level
index (`goExportByImportPath`, built once in `buildCodeEntityIndex`) maps each
Go package import path — anchored on the defining repo's declared `go.mod`
module path plus the file directory — to the exported top-level functions defined
for it. Accuracy is first: a call resolves only when the caller's exact import
path joins to exactly one exported (capitalized) function in a different repo;
zero, unexported, ambiguous (two repos exporting the same name for one import
path), or same-repo candidates stay unresolved rather than guess. The durable
callee id is the function's generation-independent `content.CanonicalEntityID`
`uid`, so the resolved edge is stable across reprojection. The SCIP-moniker
cross-repo tier is a follow-up (function definition facts carry no SCIP symbol
today).

No-Regression Evidence: `go test ./internal/reducer -run 'CrossRepoExport'
-count=1` and `go test ./internal/codeprovenance ./internal/resolutionparity
-count=1` fail before the branch/method exist and pass after. The branch runs
only after all same-repo branches return empty, and the export index is built
once per generation by reusing the file facts `buildCodeEntityIndex` already
scans — it adds no new fact scan, graph query, queue domain, lease, worker, or
per-call loop, and row/dedupe/intent volume is unchanged because the field rides
on existing rows.

No-Observability-Change: #2717 adds no route, graph query, queue domain, worker,
lease, runtime knob, metric, or metric label; the existing `code call
materialization completed` completion log stays the operator signal.
