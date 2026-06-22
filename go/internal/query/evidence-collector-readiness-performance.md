# Collector Readiness Read Performance (#3466)

This note records the query-shape contract and measured proof for
`GET /api/v0/status/collector-readiness`. It follows the convention of
`dependencies-evidence-performance.md`.

## Problem and baseline

The readiness read calls `readCollectorFactEvidence`
(`go/internal/storage/postgres/status_collector_evidence.go`). Before #3466 it
aggregated active `fact_records` inside a per-scope LATERAL on every request,
which is O(total active facts). On the live production-scale stack (~900 repos,
982 active collector scopes, ~6.6M active non-tombstone `fact_records`, ~6,740
facts/scope)
it produced only ~24 output rows (`collector_kind` x `evidence_source`) but cost:

- `Index Only Scan using fact_records_collector_status_active_idx`,
  `Heap Fetches: 3,170,104`
- `Execution Time: 5.4s warm / 7.7s cold`
- A single scope aggregates in 0.44ms; the cost is purely the 982-scope x
  full-scan multiplier. There is no loose-index-skip-scan shortcut for an exact
  `COUNT(*)` / `MAX(observed_at)` / `MAX(ingested_at)` / `DISTINCT source_system`
  over the active set.

The #3375 LATERAL removed an external-sort spill but not the O(active facts)
multiplier.

## Query shape (the fix)

`observation_count` is an exact wire-contract field, so the count is kept exact
and the aggregate is moved off the synchronous read path into a reducer-owned
materialized table `collector_evidence_summary`
(`schema/data-plane/postgres/036_collector_evidence_summary.sql`), refreshed by a
lease-guarded atomic full resweep
(`CollectorEvidenceSummaryStore.RebuildAllCollectorEvidence` /
`reducer.CollectorEvidenceSummaryMaintainer`). This mirrors the #3389
supply-chain winners maintainer.

After #3466 the read keeps the `active_scopes` and `workflow_instances` CTEs and
the final GROUP/ORDER/LIMIT, but the expensive per-scope `fact_summary` LATERAL
is replaced by a join to the precomputed summary:

```
FROM active_scopes AS scope
JOIN collector_evidence_summary AS summary
  ON summary.scope_id = scope.scope_id
 AND summary.generation_id = scope.generation_id
LEFT JOIN workflow_instances AS item ...
```

The read references **no `fact_records`** (guarded by
`TestCollectorFactEvidenceQueryReadsSummaryNotFactRecords`), so its cost is
O(active scopes ~10^3 + summary rows ~10^3), independent of total fact count. The
`active_scopes` join keeps the result exact even when the summary lags one resweep
cadence: superseded-generation rows are filtered out and a brand-new scope is at
most one cadence behind. Emitted rows are shape- and value-identical to the prior
query, so `observation_count`, `last_observed_at`, and `last_ingested_at` stay
exact (guarded by `TestRebuildCollectorEvidenceSQLIsAtomicUpsertDeleteStale`,
whose per-scope LATERAL is byte-equivalent to the pre-#3466 aggregate).

Performance Evidence: the optimization is a query-shape change proven
structurally and immune to runtime contention. Local reproduction stack:
`eshu-postgres-1`, Postgres in OrbStack, `fact_records` reltuples = 7,343,824,
`ingestion_scopes` = 1,093, `scope_generations` = 1,640, `workflow_work_items` =
1,026 — i.e. the live ~900-repo / ~982-scope / ~6.6M-active-fact scale. Baseline
(authoritative, live ~900-repo production-scale stack, cited above): EXPLAIN ANALYZE of the old read =
`Index Only Scan ... Heap Fetches: 3,170,104`, 5.4s warm / 7.7s cold. After: the
new read joins `collector_evidence_summary` (PRIMARY KEY
`(scope_id, generation_id, evidence_source, source_system)` plus
`collector_evidence_summary_scope_gen_idx`) and never touches `fact_records`; the
O(active facts) aggregate runs once per cadence in the lease-guarded resweep, not
per request, so a reader amortizes one resweep across all reads. The resweep cost
is ~= the old read cost; at the default 60s cadence that is <~10% duty cycle on
one lease-held connection.

No-Regression Evidence: `observation_count` and the MAX timestamps are byte-exact
versus the prior aggregate (same per-scope LATERAL, byte-equivalent SELECT). The
MAX timestamps drive `CollectorPromotionStale` (`status.evidenceIsStale`); the
summary stores real fact timestamps, so the only error is recency bounded by the
60s cadence, vs `DefaultCollectorPromotionStaleAfter = 24h` — a ~1440x margin, so
a cadence lag can never flip a stale verdict (guarded by
`TestCollectorEvidenceMaintainerStaleWindowMargin`).

Pending live confirmation: warm + cold `EXPLAIN (ANALYZE, BUFFERS)` of the new
read on the live production-scale stack after the migration is applied and the maintainer has run
its first resweep. The local proxy stack is currently lock/connection-bound under
the #3451 reducer/projector backlog (1.2M dead tuples on `fact_records`; even a
non-executing `EXPLAIN` of the readiness query blocks >25s while trivial queries
return instantly), which is the contention the issue flagged to isolate from, so
a clean local ANALYZE could not be captured. The query-shape change above is the
contention-independent proof; the live ANALYZE is corroboration to capture at
deploy. Classification: structural / handler win by query-shape; full wall-clock
confirmation pending the live-stack ANALYZE.

Observability Evidence: `CollectorEvidenceSummaryMaintainer` emits a structured
slog line on every committed resweep (`"collector evidence summary resweep
committed"` with `domain` and `duration_ms`) and on failure (`"collector evidence
summary resweep failed"` with `error`), so an operator can see resweep cadence,
cost, and failures. Each `collector_evidence_summary` row carries `materialized_at`
as a freshness watermark for the resweep's last run. The readiness response truth
envelope (`truth.level=exact`, `truth.basis=runtime_state`,
`truth.freshness`) is unchanged. The maintainer runs under a single-owner
partition lease (`CollectorEvidenceSummaryDomain`, partitionCount=1) so only one
reducer instance resweeps at a time; the conflict domain is the whole summary
table during a resweep and the idempotent rebuild is the lost-lease backstop.
Because the lease is released after each resweep (fast crash failover), a durable
last-materialized guard (`LastCollectorEvidenceMaterializedAt`, a cheap
`MAX(materialized_at)` over the summary — never `fact_records`) makes a replica
skip the resweep when the summary is younger than the cadence, so cluster-wide
resweeps stay capped at ~one per cadence regardless of replica count rather than
one full fact-scan per replica per cadence (#3471 review).

## Focused proof

```
cd go && go test ./internal/query ./internal/storage/postgres ./internal/status ./internal/reducer -count=1
```
