# Collector Readiness Evidence Summary Materialization (#3466)

## Problem

`GET /api/v0/status/collector-readiness` takes 5.4–9.2s at ~900-repo / 982
active-collector-scope scale. The cost is `collectorFactEvidenceQuery` in
`go/internal/storage/postgres/status_collector_evidence.go`: a per-scope LATERAL
aggregation over `fact_records`. The #3375 LATERAL fixed an external-sort spill,
but the work is still O(total active facts): the live stack reads ~6.6M active
non-tombstone `fact_records` (982 scopes x ~6,740 facts) to produce ~24 output
rows (`collector_kind` x `evidence_source`).

Live EXPLAIN ANALYZE: `Index Only Scan using
fact_records_collector_status_active_idx`, `Heap Fetches: 3,170,104`,
`Execution Time: 5.4s warm / 7.7s cold`. A single scope aggregates in 0.44ms;
the cost is purely the 982-scope full-scan multiplier. There is no
loose-index-skip-scan shortcut for an exact `COUNT(*)`, `MAX(observed_at)`,
`MAX(ingested_at)`, or `DISTINCT source_system` over the active set.

## Decision

Keep `observation_count` **exact** (it is a published wire-contract field on the
collector-readiness and status surfaces) and move the aggregate **off the
synchronous read path** into a reducer-owned materialized summary, refreshed by a
**lease-guarded periodic atomic resweep**. This mirrors the #3389 supply-chain
impact winners maintainer
(`go/internal/reducer/supply_chain_impact_winners_maintainer.go`,
`docs/internal/design/supply-chain-impact-canonical-dedup-materialization-design.md`).

### Options considered

1. **Drop exact count / cheap bounded signal** — cheapest, but degrades a
   published wire field to a bound/estimate to save query work. Rejected:
   accuracy-first; silently changing external contract semantics is a
   symptom-patch, not a root fix.
2. **Same-transaction incremental counters in Go** — theoretically cleanest, but
   the write surface defeats it:
   - tombstoning is not a dedicated UPDATE; `is_tombstone=TRUE` is set via
     `ON CONFLICT DO UPDATE` re-upsert (`facts.go`, reducer
     `canonicalReducerFactInsertQuery`), so blind +/-1 cannot distinguish a
     live->tombstone transition from a no-op re-upsert;
   - 25+ heterogeneous writers + a 500-row batch ingester path + an `UNNEST`
     batch path would each need identical correct delta logic;
   - FK `ON DELETE CASCADE` from `scope_generations` -> `fact_records`
     (`003_fact_records.sql`, `generation_retention.go`) deletes facts without
     touching any Go code — a Go counter silently misses every retention prune;
   - a per-scope counter row is a hot row; concurrent ingester + reducer writes
     to one scope contend on it (serialization hazard CLAUDE.md forbids), and
     the live stack is already write-bound (#3451). Rejected.
3. **DB trigger maintenance** — atomic, catches cascades, zero lag, but adds
   per-row cost on the hottest table plus counter-row contention, worsening
   #3451 write pressure. Rejected for the hot-path cost.
4. **pg_class/pg_stat approximate counts** — no `collector_kind x
   evidence_source` granularity; cannot produce the 24 output rows. Rejected.
5. **Chosen: reducer-owned atomic resweep materialization** — exact counts,
   off the read path, no per-row write cost, no hot counter row, self-healing
   for cascades/tombstones/generation flips because each resweep recomputes from
   the current active set. Matches an established in-repo precedent (#3389).

### Why atomic full resweep over per-scope dirty tracking

Per the #3389 design: the atomic reconcile **cannot miss a change class** —
generation-activation flips, tombstones, hard deletes, FK cascades, and new
source systems are all captured by recomputing from the current active set. This
removes the "missed dirty signal" correctness risk that per-scope incremental
tracking carries. Incremental per-scope recompute remains a future performance
optimization, not a correctness requirement.

## Schema

`schema/data-plane/postgres/036_collector_evidence_summary.sql`:

```sql
CREATE TABLE IF NOT EXISTS collector_evidence_summary (
    scope_id          TEXT NOT NULL,
    generation_id     TEXT NOT NULL,
    collector_kind    TEXT NOT NULL,
    evidence_source   TEXT NOT NULL,            -- 'reducer_facts' | 'source_facts'
    source_system     TEXT NOT NULL DEFAULT '', -- '' == no/blank source system (was NULLIF(BTRIM,''))
    observation_count BIGINT NOT NULL,
    last_observed_at  TIMESTAMPTZ NOT NULL,
    last_ingested_at  TIMESTAMPTZ NOT NULL,
    materialized_at   TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, evidence_source, source_system)
);
CREATE INDEX IF NOT EXISTS collector_evidence_summary_scope_gen_idx
    ON collector_evidence_summary (scope_id, generation_id);
```

`source_system` uses `''` as the sentinel for the original
`NULLIF(BTRIM(source_system),'')` NULL group, so it can sit in the primary key.
The read reconstructs the "no source system" semantics with
`WHERE source_system <> ''` in the `ARRAY_AGG ... FILTER`, identical to the
prior `FILTER (WHERE source_system IS NOT NULL)`.

### Backfill (matches the #3389 precedent — no duplicated aggregate SQL)

The migration creates the table only; it does **not** embed a backfill `INSERT
... SELECT`. Duplicating the per-scope aggregate in raw migration SQL would be a
second, un-unit-testable copy that can silently drift from the Go resweep. The
single authoritative aggregate is the Go resweep statement, and the maintainer's
**startup `RunOnce` is the full backfill** — an atomic full resweep from the
complete active set is a complete backfill by construction (exactly how
`RebuildAllWinners` backfills #3389). Each summary row carries `materialized_at`
as a freshness watermark.

Cold-start window: on a freshly deployed empty stack the API may read an
un-materialized summary for the seconds between API start and the reducer's first
resweep. Following the endorsed #3389 precedent, the read does **not** fall back
to a live scan (no silent fallback); the window is bounded by one cadence and is
seconds vs the 24h stale window, so no staleness verdict is affected. "Verify" =
a reconciliation check that resweep output equals the direct live aggregate,
captured in the evidence phase on the live stack.

## Resweep statement (RebuildAllCollectorEvidence)

One atomic statement: upsert-all active-scope aggregates + delete-stale, mirroring
`RebuildAllWinners`. The per-scope LATERAL aggregate is byte-identical to the
current `fact_summary` CTE (so output is preserved exactly), but it writes to the
summary table instead of returning to the API. Delete-stale removes rows whose
`(scope_id, generation_id, evidence_source, source_system)` is no longer in the
recomputed active set (covers superseded generations, tombstones, cascades).

## Read swap

`collectorFactEvidenceQuery` keeps the `active_scopes` and `workflow_instances`
CTEs and the final GROUP/ORDER/LIMIT unchanged; the expensive `fact_summary`
LATERAL CTE is replaced by a `SELECT` from `collector_evidence_summary` JOINed to
`active_scopes` on `(scope_id, generation_id)`. The active-scope join keeps the
read **exact even if the summary lags**: superseded rows are filtered out; a
brand-new scope not yet swept is at most one cadence behind. The emitted
`CollectorFactEvidence` rows are identical in shape and value to today.

The new read references **no `fact_records`** — the bounded-query guarantee.

## Staleness-verdict safety (timestamp lag)

`MAX(observed_at)/MAX(ingested_at)` are not display-only: `derivePromotionState`
-> `evidenceIsStale` uses them to derive `CollectorPromotionStale`
(`collector_promotion_proof.go`). The summary stores the **real fact
timestamps**, not the materialization time, so the only error is recency: a fact
ingested within the last cadence may not yet be reflected, making
`MAX(observed_at)` at most one cadence stale.

`DefaultCollectorPromotionStaleAfter = 24h` (`collector_promotion_proof_json.go`).
With a default resweep cadence of 60s the margin is 24h / 60s = **1440x**, so a
cadence lag can never flip a stale/fresh verdict. Asserted by a test that the
cadence is `<<` the stale window. (30s cadence -> 2880x; either is safe.)

## Maintainer

`go/internal/reducer/collector_evidence_summary_maintainer.go`, modeled on
`SupplyChainImpactWinnersMaintainer`:

- single-owner partition lease (`partitionCount = 1`) so exactly one reducer
  instance resweeps at a time — no concurrent contention on the table;
- runs once immediately at startup (backfill/reconcile) then on cadence;
- idempotent atomic rebuild is the backstop if the lease is lost mid-run;
- never exits on transient error; exponential backoff capped; converges.

Default cadence 60s (tunable). Because the read is decoupled, cadence can be
relaxed well within the 24h window to bound background DB duty cycle against the
#3451 write backlog.

Multi-replica guard (#3471 review): the lease is released after each resweep so a
crashed holder fails over immediately, but that means every replica could reclaim
the lease and run the full O(active facts) resweep on its own cadence. A durable
last-materialized guard (`MAX(materialized_at)` over the summary, read under the
lease) makes a replica skip the resweep when the summary is younger than the
cadence, so cluster-wide resweeps stay capped at ~one per cadence regardless of
replica count. The guard is decoupled from lease-hold duration, so it keeps fast
failover while preventing redundant fact scans. Resweep cost ~= the current read cost (~5–9s); at 60s
cadence that is <~10% duty cycle on one lease-held connection, amortized across
all readers (net win whenever readiness is queried more than ~once/cadence).

## Telemetry

- `eshu_collector_evidence_resweep_duration_seconds` histogram;
- `eshu_collector_evidence_resweep_errors_total` counter (labeled `failure_class`);
- span `reducer.collector_evidence_resweep` with `rows_written`, `rows_deleted`;
- maintainer logs resweep commit/failure;
- registration asserted by `TestInstrumentsRegistered`.

## Files

- `schema/data-plane/postgres/036_collector_evidence_summary.sql` (new)
- `go/internal/storage/postgres/collector_evidence_summary.go` (new: resweep store)
- `go/internal/storage/postgres/status_collector_evidence.go` (read swap)
- `go/internal/reducer/collector_evidence_summary_maintainer.go` (new)
- reducer service/run wiring (`go/internal/reducer/service.go`, `go/cmd/reducer/run.go`)
- `go/internal/telemetry/instruments.go` (instruments)
- tests beside each
- `go/internal/query/collector-readiness-evidence-performance.md` (perf note)
- package `README.md` / `doc.go` / `AGENTS.md` for touched dirs

Wire contract (`collector_readiness` / status JSON / OpenAPI) is **unchanged**:
same fields, exact `observation_count`. No `http-api.md` / `openapi*.go` change.

## Verification

- `cd go && go test ./internal/query ./internal/storage/postgres ./internal/status ./internal/reducer ./internal/telemetry -count=1`
- `cd go && go vet ./...` and `golangci-lint run ./...`
- before/after EXPLAIN ANALYZE on the live stack (warm + cold), read query in
  isolation to separate query-shape gain from #3451 contention
- resweep duration measurement on the live stack
- IP-free (eshu repo is PUBLIC)
```
