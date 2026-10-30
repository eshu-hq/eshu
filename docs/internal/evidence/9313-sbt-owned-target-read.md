# Owned-package target read with sbt rows: plan before and after (#9313)

## What changed

`FactStore.ListOwnedPackageDependencyTargets`
(`go/internal/storage/postgres/owned_package_targets.go`) selects two more
`entity_metadata` keys (`sbt_cross_version`, `sbt_binary_artifact`) and, when
alias expansion is on, also selects raw `sbt` rows for a `maven` read. The
`sbt` ecosystem is never folded in SQL, the `DISTINCT ON` identity is
unchanged, and no predicate, join, ordering, or limit moved.

## Method

Throwaway Postgres 18 (`postgres:18-alpine` in Docker on the dev host, default
`work_mem`), one active generation, 60,000 synthetic `content_entity`
dependency facts spread evenly over `maven`/`gradle`/`sbt`/`go`/`npm`, sbt
rows carrying `binary` cross plus artifact, then `ANALYZE fact_records`.
`EXPLAIN (ANALYZE, BUFFERS)` on `listOwnedPackageDependencyTargetsQuery(true,
true)` with the `maven` expanded filter, read limit 101, bucket 0, target
limit 100. Before is the base commit `a2d69c98c2`; after is this branch. One
run each: the claim below is plan shape, which is deterministic given the
stats; timings are single-run and indicative.

## Before and after

| | Before | After |
| --- | --- | --- |
| Filter values | `[gradle maven]` | `[gradle maven sbt]` |
| Fact index rows | 24,000 | 36,000 |
| Access path | Index Scan on `fact_records_active_package_dependency_entity_idx`, `package_manager = ANY(...)` plus generation | Same index, same join order, `ANY` gains `'sbt'` |
| Sequential scan on `fact_records` | None | None |
| Shared buffers | 120,407 hit | 180,579 hit (+50%, linear in selected rows) |
| Execution time | 167.583 ms | 262.382 ms |
| Sort | quicksort, 2,603 kB in memory | external merge, 3,272 kB spill (throwaway `work_mem`; data-size effect of 50% more rows, not a plan change) |

## Verdict

The measured plan is kept: the same index scan, the same scope/generation
probes, the same hash-slot join. Added cost follows the added selected rows
linearly, the same trade the #9285 Gradle expansion accepted (+20% targets
for ~35 ms on the QA replica). No new index: the `sbt` prefix is served by
the existing `package_manager` index, as the #8961 sbt branch measured.

## No-Regression Evidence

Hermetic query-shape pins (`TestOwnedPackageEcosystemAliasExpansionSharesOneTable`,
`TestListOwnedPackageDependencyTargetsQueryIsActiveAndBounded`,
`TestOwnedPackageSlotDifferentialDerivesFromTheShippedQuery`) and the live
reader proof (`TestListOwnedPackageDependencyTargetsAliasesLive`, now with
sbt rows) pass on the final tree. The `DISTINCT ON` identity and slot-hash
inputs are untouched, so rotation and dedup behavior are unchanged.

## No-Observability-Change

Same read path, span, and window-stat logging; the two new columns ride the
existing row scan with no new metric, log field, or knob.
