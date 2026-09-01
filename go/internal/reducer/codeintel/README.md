# Reducer code intel

## Purpose

`codeintel` builds the materialized code-reachability read model and the
#5376/#5494/#5500 code-root verdicts that gate it, for one repository
generation at a time. It owns two questions the dead-code query surface
depends on: which entities are reachable from a root set (and how strongly),
and whether a per-file, parser-rooted Ruby-on-Rails controller action is a
genuine dead-code root or a false positive that the repo's full class registry
and route surface can rule out.

It exists as its own package (issue #6061, epic #6053) because the two source
files it grew from — `code_reachability_projection.go` and
`code_root_verdicts.go` — depend on each other in both directions. A trial move
of either file alone produced a cycle (five symbols needed the other direction,
three needed this one); moving both together turns that inter-file dependency
into an ordinary intra-package one. The [package-restructure design
doc](../../../../docs/internal/design/package-restructure.md) has the general
seam.

Four of the five symbols crossing that boundary (`BuildCodeRootVerdicts`,
`CodeRootVerdictRow`, `RubyClassEntity`, `RubyRailsRouteFacts`) are already
exported; only `removeDowngradedRailsControllerRoots` is not. That is why a
symbol-graph pass that counts unexported cross-family edges as the blockers
scored this pair as cheap — export status does not help when the dependency
runs both ways, because the child would import root for one symbol while root
imports the child for another. Distrust a cheap score on that basis for any
later family in this epic; it is exactly how the epic's original first pick,
`containerimage`, turned out to sit in three simultaneous cycles despite being
labelled clean and zero-inbound.

## Ownership boundary

This package owns the reachability walk, the code-root verdict builder, the
route-liveness downgrade layer, and the runner that drains pending snapshots
and replaces materialized rows. It does not read from storage or write to
Postgres directly — `CodeReachabilityInputLoader` and `CodeReachabilityRowWriter`
are the ports the reducer root wires to `internal/storage/postgres`. It does
not decide reducer scheduling or lease ownership; the runner's `Run` loop is a
`serviceSideRunner` the reducer root's `Service.startSideRunners` starts
alongside the other projection lanes.

## Exported surface

- Reachability projection: `BuildCodeReachabilityRows`,
  `BuildCodeReachabilityRowsWithStats`, `CodeReachabilityProjectionInput`,
  `CodeReachabilityRow`, `CodeReachabilityRoot`, `CodeReachabilityEdge`,
  `CodeReachabilityProjectionStats`, `CodeReachabilityStateReachable`,
  `CodeReachabilityStateAmbiguous`.
- Runner and ports: `CodeReachabilityProjectionRunner`,
  `CodeReachabilityProjectionRunnerConfig`, `CodeReachabilityProjectionResult`,
  `CodeReachabilityInputLoader`, `CodeReachabilityRowWriter`.
- Code-root verdicts: `BuildCodeRootVerdicts`, `CodeRootVerdictRow`,
  `CodeRootVerdictBasis`, `CodeRootVerdictStats`, `CodeRootKindRubyRailsControllerAction`,
  `CodeRootVerdictConfirmed`, `CodeRootVerdictDowngraded`, `RubyClassEntity`.
- Route liveness (#5494): `RubyRailsRouteFacts`, `ReasonRouteUnreachable`,
  `RouteEvidenceNoData`, `RouteEvidenceAmbiguous`, `RouteEvidenceRouted`,
  `RouteEvidenceUnrouted`.

See `doc.go` for the godoc-rendered contract, in particular how the two
downgrade paths (ancestry and route liveness) relate.

## Dependencies

`internal/codeprovenance` for provenance metadata on reachability rows,
`internal/cpubudget` for the runner's concurrency clamp, `internal/rubycontroller`
for the shared Rails-controller-ancestry decision and verdict constants, and
`pkg/log` for the runner's logger type. `internal/parser/ruby` and
`internal/parser/shared` are test-only, pulled in by
`code_root_verdicts_integration_test.go` to feed real parser output into
`BuildCodeRootVerdicts` rather than hand-built fixtures. No dependency on the
reducer root or any other family subpackage.

The reducer root (`internal/reducer/service.go`) imports this package to
declare the `CodeReachabilityProjectionRunner` field on `Service`, and
`internal/storage/postgres` (`code_reachability.go`,
`code_reachability_loader.go`) imports it to implement
`CodeReachabilityInputLoader` and `CodeReachabilityRowWriter` and to shape SQL
scan targets. `cmd/reducer` wires the concrete runner. Import is one-way: this
package never imports back.

## Telemetry

This package emits no metric or span of its own. The runner's execution is
covered by the reducer's standard `eshu_dp_reducer_executions_total` /
`eshu_dp_reducer_run_duration_seconds`, plus
`eshu_dp_postgres_query_duration_seconds` for the class-registry and route-fact
loads and the atomic row replacement. Per-cycle outcomes surface through the
runner's "code reachability projection completed" structured log
(`verdicts_written`, `verdicts_downgraded`, `verdicts_route_downgraded`,
`verdicts_inconclusive_missing_context`) and the per-partition "code root
controller verdicts downgraded" log. See
`docs/public/observability/telemetry-coverage.md` rows for "projection
(code-root controller verdicts)" and "projection (code-root route liveness)".

## Gotchas / invariants

- **Absence is not a claim.** An entity or root with no verdict/reachability
  row is *kept* by the dead-code query, not treated as dead. This is the
  lag-safety keystone: an incomplete or stale snapshot must never manufacture a
  false positive.
- **Ancestry and route-liveness downgrades are independent but co-committed.**
  `BuildCodeRootVerdicts` runs the #5376 ancestry walk and the #5494 route join
  and writes both outcomes into the same `CodeRootVerdictRow` slice, replaced
  in the same transaction as the reachability rows. They must never be split
  across separate writes — a downgraded controller root and the reachability
  rows built from the downgraded-filtered root set would otherwise be able to
  disagree.
- **Lexical scoping matters for base-class resolution (#5500).** A bare base
  reference resolves through the lexical-prefix chain of the referencing
  class's own namespace before falling back to a broad suffix search, so a
  same-last-segment class in an unrelated namespace can never be mistaken for
  the real base. `code_root_verdicts_lexical_scope_test.go` and
  `code_root_verdicts_integration_test.go` prove this against the real Ruby
  parser, not hand-built fixtures — do not replace those with synthetic
  `RubyClassEntity` values when extending this logic.
- **Partitions are the conflict domain.** The runner partitions pending inputs
  by (scope, generation, repository) and each partition is a PK-scoped
  DELETE+INSERT, so partitions are provably disjoint and safe to run
  concurrently up to `Config.Concurrency`.

## Related docs

- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
- `../evidence-5376-code-root-verdicts.md`, `../evidence-5494-route-liveness.md`,
  `../evidence-5500-lexical-scope-restriction.md` — the prove-the-theory-first
  records for the three features this package implements.
