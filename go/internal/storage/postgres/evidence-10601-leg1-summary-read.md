# Evidence: #10601 (reopened) leg1 summary read — issue eshu-hq/eshu#10601

## Symptom

`GET /admin/status` (`?format=text` and `?format=json`) returned 500 on QA
under NVD-backlog load. The runtime snapshot reader bounds each snapshot to
5s (`statusSnapshotLimit` in `go/internal/runtime/postgres/status_reader.go`).
After the first #10601 fix (the inline lateral instance lookup,
`evidence-10601-collector-evidence-lateral.md`), the collector-evidence leg
still cost ~4.1s cold / ~1.1s warm at 6139 active scopes — 79% of the 5.2s
cold snapshot sum — so any load jitter pushes the whole snapshot over budget.

## Root cause (measured)

`collectorFactEvidenceQuery`
(`go/internal/storage/postgres/status_collector_evidence.go`) evaluated the
collector-instance identity per request as `LEFT JOIN LATERAL ... LIMIT 1`
over `workflow_work_items` in DISTINCT ON key and order, one indexed probe
per active scope (6139 probes), before the summary fanout over the 10.5k-row
`collector_evidence_summary`. The probes are individually cheap, but 6139 of
them plus the summary scan inside one 5s snapshot transaction leave no
headroom. The warm plan is probe-dominated; no index can fix a per-row probe
multiplied by the active-scope count.

## Fix

The instance leg moved into the reducer resweep, following the #3466
precedent: `rebuildCollectorEvidenceSummarySQL`
(`go/internal/storage/postgres/collector_evidence_summary.go`) now computes
the per-scope DISTINCT ON identity (newest non-blank
`collector_instance_id` by `updated_at DESC, work_item_id ASC`) in a second
`LEFT JOIN LATERAL` and stores it on the new
`summary.collector_instance_id` column (migration 296, `''` sentinel for
scopes with no non-blank work item). The instance is a non-key attribute of
the existing grain, so an instance change on an unchanged scope upserts in
place and the delete-stale key is unchanged.

The snapshot read is now a pure summary read: `active_scopes` (the lag guard:
superseded generations filtered, brand-new scopes at most one cadence
behind) joined to `collector_evidence_summary`, grouped by
`(collector_kind, collector_instance_id, evidence_source)`. It references no
`workflow_work_items` and no `fact_records`.

Backfill: the column defaults to `''`, so the read stays shape-identical
from the migration forward; the maintainer's startup `RunOnce` plus the 60s
cadence reconciles every row within one cadence (the resweep upserts the
column on conflict, so a partial first pass self-heals).

## Performance Evidence

`EXPLAIN (ANALYZE, TIMING OFF, COSTS OFF)` on a seeded fixture (2005 active
scopes, 10,006 facts, 2,006 work items, 4,004 summary rows; local
PostgreSQL 18.6, warm shared buffers), old vs new read over identical seed
data, timestamps normalized:

| Run | Before (per-scope lateral probe) | After (summary read) |
|---|---|---|
| 1 | 8.264 ms | 7.188 ms |
| 2 | 8.251 ms | 7.096 ms |
| 3 | 8.231 ms | 7.197 ms |
| 4 | 8.173 ms | 7.152 ms |

- Before plan: `Nested Loop Left Join` with 2004 indexed work-item probe
  loops before the summary fanout.
- After plan: zero work-item probes; the join is scopes-to-summary only.
- The resweep absorbs one lateral leg per scope in the background: 22.6 ms
  before vs 38.1 ms after on the same fixture — still three orders of
  magnitude under the 60s cadence, and off the request path entirely.

Classification: wall-clock win that removes the per-scope probe multiplier
from the snapshot budget. Fixture scale is small by design (it must run in
the focused gate); both plans fit comfortably here. The QA-scale win follows
from probe elimination (6139 probes at 1.1s warm removed from the 5s
budget), not from extrapolating these milliseconds: re-measure leg1 on QA
before closing the issue.

Row exactness: the timestamp-normalized before/after outputs diff empty (the
same 200 rows in the same order under the LIMIT 200 cut), and
`TestCollectorFactEvidenceSummaryReadMatchesLateralLive` diffs the shipped
resweep-then-read against the derived lateral oracle over blank-latest
fallback, `updated_at` tiebreak, empty-instance, superseded-generation,
tombstone, and blank-source-system branches (empty diff).

## Observability Evidence

No new metric. Every status snapshot read now stamps its duration on the
`postgres.status_snapshot` span as `status.snapshot.read.<label>` seconds
(`StatusStore.recordRead`), so the next slow-leg recurrence attributes in
traces without waiting for the metric scrape; the per-read
`eshu_dp_status_snapshot_read_duration_seconds` histogram is unchanged.
`TestRecordReadSetsPerLegSpanDuration` pins the attribute.

## Rejected alternatives (arbiter)

- Bound raise: the 5s bound stays; it is the snapshot truth contract.
- Stale caching: no new cache; the summary is the sanctioned resweep
  materialization with watermark and lag guard, not a TTL cache.
- Snapshot split: one snapshot, one transaction; no cross-transaction read.
