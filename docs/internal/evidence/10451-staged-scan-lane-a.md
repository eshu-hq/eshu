# 10451 lane A: staged winners batch and census

Refs #10451.

Lane A builds the per-(scope, generation) batch candidate fetch and the
active-pair census for the staged stale-winners audit scan. Production calls
neither new function yet. Lane B wires them to the in-memory compactor.

## What changed

`supplyChainImpactWinnerBodySQL` now composes from shared builders beside it.
The rendered text is byte-identical to the previous literal. The staged batch
queries reuse those builders, so the batch cannot drift from the
single-statement scan. The new file adds `CensusPairs`, `StageBatch`,
`ScopeGeneration`, and `StagedRow` with no production caller.

## Measurement

No-Regression Evidence: the refactor changes no evaluated SQL. A golden diff
of `supplyChainImpactWinnerBodySQL` and the full
`supplyChainImpactWinnerSelectSQL` against `origin/main` is byte-identical,
and `TestWinnersAuditLegacyStatementsUnchanged` pins the legacy statement set
and order exactly. Focused suite
`go test ./internal/storage/postgres/ -run 'Winners|Staged|Census' -count=1`
passes (31 pass, 0 fail; live legs skip without a database), and
`go vet ./internal/storage/postgres/` is clean. The new batch adds no index:
it rides `fact_records_supply_chain_impact_active_scan_idx` on
`(scope_id, generation_id, fact_id)` with equality on the leading pair plus
the index predicate.

No-Observability-Change: no spans, metrics, logs, status, or pprof change.
The new functions emit no telemetry. They add no statement to any existing
path.
