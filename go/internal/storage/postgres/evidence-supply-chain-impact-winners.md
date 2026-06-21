# Supply-chain impact canonical winners materialization (#3389)

Tracks the maintained cross-scope dedup materialization that removes the
read-time `ROW_NUMBER` dedup spill from `GET /api/v0/supply-chain/impact/findings`.
Design + measured PoC:
`docs/internal/supply-chain-impact-canonical-dedup-materialization-design.md`.

## Scope of this change (Phase 1a + 1b write-core)

- Phase 1a: `supply_chain_impact_canonical_winners` denormalized read-model table
  + bootstrap registration.
- Phase 1b (write core): `supplyChainImpactWinnerSelectSQL` (winner per
  canonical_key, same tiebreak as the read) + `rebuildSupplyChainImpactWinnersSQL`
  (atomic upsert-all + delete-stale) + `SupplyChainImpactWinnersStore.RebuildAllWinners`
  (backfill / full resweep).

No runtime consumer is wired yet: the shared-projection partition worker
(incremental per-canonical_key recompute + dirty-key/generation-flip wiring) and
the read switch are later phases. This change only adds a table and a
recompute/backfill writer that nothing on the request path calls yet.

## Measurement environment

Isolated throwaway Postgres 18 (`postgres:18-alpine`, `work_mem=16MB`), seeded
with 500,000 active `reducer_supply_chain_impact_finding` facts across 901 active
scopes (the same seed used for the #3389 read measurements). Schema applied from
the shipped `033_supply_chain_impact_canonical_winners.sql`; the shipped
`rebuildSupplyChainImpactWinnersSQL` (dumped from the Go const) was executed
directly.

No-Regression Evidence: the recompute is byte-identical to the legacy read-time
dedup. After `RebuildAllWinners` populated 500,000 winner rows, a winners-derived
read (`SELECT … FROM supply_chain_impact_canonical_winners w JOIN fact_records
refetch ON refetch.fact_id = w.winner_fact_id WHERE <filter on w> ORDER BY
finding_id`) was compared via `COPY (...) TO STDOUT WITH (FORMAT csv)` against the
legacy `listSupplyChainImpactFindingsQuery` output for the same filters. `diff`
is empty for: no filter (500,000 rows), `impact_status=affected_exact` (125,000),
`severity_bucket=high` (100,000), and `ecosystem=npm` (166,666). The winner
selection reuses the read's exact canonical_key, public finding_id fallback,
`has_payload_finding_id` tiebreak, severity-bucket thresholds, and
suppression-state default; the parity is pinned in Go by
`TestSupplyChainImpactWinnerSelectMirrorsReadDedup` and proven against the legacy
read by the CSV diff above.

Performance Evidence: the maintained shape moves the dedup off the read path. The
backfill `RebuildAllWinners` materialized 500,000 winners in ~4.6 s (one-time /
resweep). The downstream read against the winners table (validated in the design
PoC, same seed) is `O(page)`: broad-filter first page **0.18–0.65 ms**, no-filter
first page **1.0 ms**, vs the legacy read-time-dedup **1873 ms** (~2000×), with no
external-merge spill. The read switch lands in a later phase; this note records
that the recompute produces the rows that read will serve.

Observability Evidence: No-Observability-Change for this phase. The table and the
recompute/backfill writer add no metric, span, log, status row, graph write, or
queue consumer, and nothing on the request path calls the writer yet. The
partition worker added in the next phase will introduce backlog/recompute/lease
telemetry per concurrency-deadlock-rigor; that telemetry ships with the code that
runs on the runtime path.
