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

Observability Evidence: No-Observability-Change for the storage-layer table +
recompute writer themselves (they add no metric/span/log/status/graph/queue and
nothing on the request path calls the writer). Runtime visibility for the
maintainer that drives it is covered below.

## Phase 1b maintainer (this change) — reducer side-runner

`SupplyChainImpactWinnersMaintainer`
(`go/internal/reducer/supply_chain_impact_winners_maintainer.go`) runs as a
reducer service side-runner: one resweep at startup (backfill/reconcile) and then
on a fixed cadence (default 30s), each resweep calling `RebuildAllWinners` (the
atomic upsert-all + delete-stale statement).

Design choice — correctness over incrementalism: a full atomic resweep is used
instead of per-canonical_key dirty tracking. The atomic reconcile recomputes the
winner set from the current active facts, so it cannot miss a change class
(generation-activation flips, tombstones, new sources are all captured). This
removes the "missed dirty signal / stale winner served" correctness risk the
incremental design carried (ADR §10). Incremental per-key recompute remains a
future performance optimization.

Concurrency (conflict domain): the conflict domain is the whole winners table
during a resweep. A single-owner partition lease (`SupplyChainImpactWinnersDomain`,
partitionID 0, partitionCount 1, reusing the shared-intent
`ClaimPartitionLease`/`ReleasePartitionLease`) keeps exactly one reducer instance
resweeping at a time, so concurrent resweeps never contend on the table.
Transaction scope = one `RebuildAllWinners` statement (atomic per resweep). Retry
scope = the maintainer loop (next cadence, exponential backoff capped at 5m on
error). The lease is released after every cycle (even on error), bounding takeover
to the lease TTL; the idempotent rebuild is the backstop if the lease is lost
mid-run — the next owner reconciles to the same state.

No-Regression Evidence: maintainer logic is covered by
`supply_chain_impact_winners_maintainer_test.go` across the replay/retry matrix:
lease-acquired resweep, lease-not-acquired skip (no rebuild), rebuild-error with
the lease still released (no held lease after error), idempotent repeated cycles
(claim/release balanced), missing-dependency validation, and context-cancel loop
exit. `go test ./internal/reducer ./cmd/reducer -count=1` passes; the winners the
resweep writes are byte-identical to the legacy read (proven above).

Observability Evidence: the maintainer emits structured logs on resweep commit
(debug) and failure (error, with the wrapped cause carrying the
lease-claim/rebuild/release failure class), so an operator can see whether
resweeps are progressing or failing. Resweep recency is independently queryable as
`MAX(materialized_at)` on `supply_chain_impact_canonical_winners`, which the read
surfaces as `truth.freshness` in Phase 2. No graph write or queue consumer is
added. Richer per-resweep duration/row-count metrics are a follow-up; the logs +
materialized_at timestamp give progress/failure visibility now.
