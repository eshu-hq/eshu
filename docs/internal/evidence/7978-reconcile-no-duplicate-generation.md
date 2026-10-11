# Reconcile Sweep Stores No Duplicate Generation (#7978)

The reconcile sweep forced a full snapshot of every git scope once per
interval (24 hours by default). The fact builder blanked the freshness hint for
that snapshot, so the ingestion store's unchanged-generation check could not
match it. The store then wrote a new `scope_generations` row and every fact
row, and the reducer rebuilt documents and terms for the same content. On the
QA environment, 23,290 of the 23,547 same-commit generations carried an empty
hint.

## Contract

A snapshot is a duplicate when its scope, `source_commit_sha`, and freshness
hint equal those of the scope's newest `pending` or `active` generation. The
git hint is v3. It folds in `GenerationContentVersion` (the hand-bumped
`gitGenerationContentVersion` plus a digest of the fact-schema registry) and the
default branch. It is never blank. A duplicate stores no generation, fact,
document, or term row.

| Snapshot | Store action | `refresh_skip_reason` |
| --- | --- | --- |
| Duplicate, not a reconcile | drain the stream, store nothing | `unchanged` |
| Duplicate reconcile, newest match is `pending` | drain, store nothing | `unchanged` |
| Duplicate reconcile, match is `active` | re-open the active generation's projector work | `reconcile_reprojected` |
| Same, work row already pending or leased | stamp the request only | `reconcile_reprojection_pending` |
| Same, a newer generation activated before the scope lock | drop the snapshot | `reconcile_superseded` |
| Different hint, commit, or version; or `DefaultBranchChanged` | store a generation | none |

The re-open mirrors the #7209 zombie heal. It uses the same canonical
`work_item_id`, `ON CONFLICT DO UPDATE`, the in-flight guard, and a monotonic
`attempt_count`. It runs after the scope row lock, which is the first lock
every blocking path takes. It is fenced on the generation still being the
scope's active generation with the same hint and commit. It stamps
`reconcile_reason` and `reconcile_requested_at` in the generation payload.
`fullReconcileStateQuery` and the #7389 writer coverage count the stamp only
while the generation stays `active`.

The re-projection retracts drift that carries another generation id. The
projector service treats a claim with `attempt_count > 1` as a non-first
generation, and the claim also reports an activated generation as having a
prior generation. The re-projection does NOT retract drift stamped with the
active generation id itself, because canonical retraction filters on
`generation_id <> $current`. The old behaviour stored a new generation id, so
it did retract that drift.

## Performance Evidence

Performance Evidence: the source is branch `fix/7978-reconcile-no-duplicate-generation`
at `d8d22ce65a` on base `f41526bf8a`. The backend is a private local
PostgreSQL 18.6 cluster (Homebrew). `TestUnchangedReconcileStoresNothingLive`
measures the store commit time through the production `IngestionStore` and
`ProjectorQueue`. The "before" column is the same reconcile with the blank hint
that the collector sent before this change, which is the path main takes. Each
figure is a single-process wall time on a shared developer host, so treat it as
an order-of-magnitude result and not as a latency distribution.

| facts per generation | fixed reconcile | blank-hint reconcile (before) | rows written by the fixed reconcile |
| ---: | --- | --- | --- |
| 200 | 1.6-1.8 ms (2 runs) | 33-142 ms (2 runs) | 0 generation, 0 fact, 0 search, 0 content |
| 6,800 | 1.6-2.5 ms (3 runs) | 419-497 ms (3 runs) | 0 generation, 0 fact, 0 search, 0 content |

The before path also writes 1 generation row, every fact row, and 1 work row.
The reducer then rebuilds documents and terms; that cost was not measured
here. The collector still parses the repository for a reconcile, because the
content hint needs parse output. This change does not save the parse.

Plans: generic plans (`plan_cache_mode = force_generic_plan`) with `EXPLAIN
(ANALYZE, BUFFERS)` on 35,001 seeded generations across 800 scopes. One scope
holds 3,040 of them. The query text came from the Go constants.

| statement | plan | execution |
| --- | --- | ---: |
| candidate read | `scope_generations_scope_idx`, LIMIT 1 | 0.034-0.050 ms |
| full reconcile state | `scope_generations_scope_latest_lookup_idx` x2 | 0.032-0.037 ms |
| uncovered writers | existing plan; the CTE runs on the LIMIT 1 row | 0.122-0.436 ms |
| re-open (in `BEGIN`/`ROLLBACK`) | pkey, `scope_generations_active_scope_idx`, `ingestion_scopes_active_generation_idx` | 0.354-0.787 ms |

No plan used a sequential scan or a sort. The projector claim change adds one
expression to the SELECT list over the already-joined generation row. Its plan
was not re-measured.

## Concurrency Evidence

`TestReconcileReopenLosesLockRaceToNewerActivationLive` holds the scope row in
a transaction that supersedes the active generation and activates a newer one.
The reconcile reads the old generation as a match. It then waits on the scope
lock, which the test observes in `pg_stat_activity`. After the other
transaction commits, the reconcile drops the snapshot with
`reconcile_superseded` and writes nothing.

`TestReconcileReprojectionSupersededBeforeClaimIsNoopLive` re-opens the work
and activates a newer delta first. The claim sweep then retires the re-opened
row as `superseded`, and the stamp stops counting.

A repeated reconcile while the re-open is pending writes nothing and reports
`reconcile_reprojection_pending`.

## Graph Truth Evidence

The explicit-mode Neo4j proofs run on `neo4j:2026-community` (pinned digest)
with the production projector service and canonical writer:

- `TestDeltaRefusedAtAckLeavesOverlayLive`: the #7389 `graph_dirty` heal at the
  active commit re-projects the active generation, stores no generation, and
  retracts the refused writer's overlay.
- `TestUnchangedReconcileRetractsPlantedDriftLive`: an `interval_elapsed`
  reconcile on a one-generation scope stores no generation and retracts a
  planted `File` node. The scope then reads `fresh`.

## Observability Evidence

Observability Evidence: `telemetry.RecordSkippedRefresh(reason)` counts each
skip under one value of the closed `telemetry.RefreshSkipReasons` set.
`/metrics` exposes `eshu_runtime_refresh_skipped_by_reason_total{service_name,reason}`
next to the existing total.

The skip log line `ingestion refresh skipped` carries `refresh_skipped`,
`refresh_skip_reason`, the scope, the matched generation, and
`reconcile_reason`. The caller's span gets an `ingestion.refresh_skipped` event.

A stored reconcile generation records `reconcile_reason` in
`scope_generations.payload`, and a stored default-branch generation records
`default_branch_changed`.
