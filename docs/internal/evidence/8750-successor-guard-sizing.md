# #8750: size the destructive-successor guard like for like

## Problem

The #7926 destructive-successor backstop refuses a successor generation that
would empty or sharply shrink a scope. It compared the successor's streamed
fact count with every `fact_records` row of the prior generation, and it
sized a git delta successor by the delta's own stream. Both sizes were wrong.

1. **Reducer rows inflate the prior.** Reducer domains write
   `reducer_`-prefixed facts into the source generation after it activates.
   No collector stream carries them, so a prior on a reducer-heavy repository
   looked about twice its real size. A forced full that re-parsed the same
   repository held well under half of the raw total and was refused.
2. **A delta carries only what changed.** A git delta streams its changed
   files plus tombstones for deleted paths. The projector overlays it on its
   base full generation, so the projected result keeps every other file. Sized
   by its own stream, a 41-file delta looked like a 96 percent shrink.

A refusal rolls the commit back, keeps the prior generation active, and the
next sync recollects and is refused again. Nothing converges.

Observed on one hosted deployment before the fix (no names, public-safe):

- 19 of 20 git commits refused in 3 hours.
- 35 of 124 pushed repositories behind, median lag 5.7 hours, maximum 74.7
  hours.
- A forced full of 11,360 facts refused against 28,921 prior rows, of which
  15,231 were collector rows (11,360 is 0.75 of the collector rows).
- A 41-file delta of 6,544 facts refused against 175,858 prior rows, of which
  81,049 were `reducer_` rows.

## Fix

Commits `e2443e8336` and `7d14116d97`.

- `successorGuardCountQuery` reads three counts in one index-only pass over
  `fact_records_scope_generation_idx`: collector rows
  (`ExcludeReducerDerivedKinds`), all rows, and file rows. `CountFacts` is
  unchanged for its other callers.
- `backfill.DecideSuccessor` compares a full successor with the prior's
  collector rows only. The refusal predicate and the half threshold are
  unchanged.
- A git delta successor is sized by its projected file-level union against its
  anchor: the newest projected full generation at `DeltaBaselineCommitSHA`.
  The anchor comes from `resolveDeltaBaseAtCommit`, the resolver the
  projector's overlay already uses. Path matching uses
  `facts.DeltaScope.FilePath`, the overlay's own qualifier.
- Zero-query lower bound: `anchorFiles - |touched ∪ live| + live`. At or above
  half the anchor's files, the delta stores. Under half, one probe of the
  anchor's file paths (`successorGuardAnchorFilePathsQuery`) gives the exact
  union, and the delta is refused only when `union * 2 < anchorFiles`.
- A delta with no anchor stores. The projector refuses it as base-missing and
  arms the reindex watermark, which a store-side refusal would not.
- A generation whose stream disagrees with `IsDelta` is sized as a full
  (the stricter rule) and logs `successor_delta_scope_mismatch`.
- Renames net to zero (old path deleted, new path changed). A deleted path
  that was never indexed removes no anchor file.

## Proof

`go/internal/storage/postgres/ingestion_successor_guard_sizing_live_test.go`
runs eight live PostgreSQL tests through the production `IngestionStore`
commit path and the real projector claim. All eight pass at `7d14116d97`.

| Test | Pins |
| --- | --- |
| `TestGuardStoresForcedFullOverReducerHeavyPriorLive` | A forced full holding 0.74 of the prior's collector rows stores although it is under half of the raw total. The production refusal. |
| `TestGuardRefusesSharplyShrunkenFullOverReducerHeavyPriorLive` | A full under half of the collector rows is still refused. |
| `TestGuardStoresSmallDeltaOverLargePriorLive` | A 41-file delta over a large anchor stores. The production refusal. |
| `TestGuardRefusesMassDeleteDeltaLive` | A delta deleting 150 of 200 indexed files is refused on the exact union. |
| `TestGuardStoresRenameDeltaLive` | Moving 120 of 200 files fails the lower bound, and the exact probe stores it. |
| `TestGuardStoresDeltaDeletingNeverIndexedPathsLive` | 150 deleted paths that were never indexed remove no anchor file. |
| `TestGuardStoresDeltaWithMissingBaseLive` | A delta with no anchor stores and is left to the projector. |
| `TestGuardSizesDeltaWithoutRepositoryFactAsFullLive` | A generation claiming `IsDelta` with no `delta_generation` fact is sized as a full and refused. |

The tests were written before each fix and run red first, on a disposable
local PostgreSQL 18 schema (`ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN`):

- On `0c966372f4` (before `e2443e8336`), the forced-full test failed with
  `destructive_successor skips = 1, want 0: 40 of 54 collector rows is not a
  sharp shrink`; the sharp-shrink test passed.
- On `e2443e8336` (before `7d14116d97`), three delta tests failed with
  `destructive_successor skips = 1, want 0`: the 41-file delta, the
  never-indexed deletes, and the missing anchor. The mass-delete, rename, and
  missing-repository-fact tests already held and stay as guard rails.

## Performance Evidence:

The guard adds bounded reads on the ingestion commit path, so the claim is
bounded cost, not a speedup.

- Count query: one index-only pass per generation (two for a delta whose
  anchor is not the prior), replacing the previous single-count pass
  (`countFactsQuery`) with the same access path and three aggregates.
- Anchor probe: runs only when the zero-query lower bound fails, a delta that
  may remove most of its anchor's files. It reads the anchor's file rows once.
  Ordinary deltas and fulls never run it.

Measured on a hosted QA read replica (PostgreSQL in recovery, read-only
session), warm cache, median of five `EXPLAIN (ANALYZE, BUFFERS)` runs after a
warm-up, the old and new count interleaved:

| Generation | Old `countFactsQuery` | New `successorGuardCountQuery` |
| --- | --- | --- |
| 1,036,727 rows, 12,402 files | 118.2 ms, Index Only Scan, 20,489 buffers | 63.3 ms, Parallel Index Only Scan (2 workers), 20,614 buffers |
| 175,858 rows, 4,446 files | 56.4 ms, Index Only Scan, 39,316 buffers | 61.2 ms, Index Only Scan, 39,316 buffers |

Both use `fact_records_scope_generation_idx`. Heap Fetches are unchanged by the
query (33,348 and 175,863: the smaller generation's pages are not all-visible).
The large-generation gain depends on free parallel workers; the small one costs
4.8 ms (8.6%) for the extra aggregates.

The anchor probe on the 12,402-file anchor: Index Scan on the same index,
67,085 shared hits, 486.6 ms median execution (485.1 to 487.0). Nearly all of
it is detoasting `payload` to read `relative_path`: the same rows without
`payload` take about 6 ms; the payloads total about 207 MB. Cold cache was not
measured. This cost is paid only by a delta that may remove more than half of
its anchor's files; the zero-query bound keeps it off every other commit.

Concurrency: the guard takes no new lock. The prior and anchor counts run on
the base connection before the commit transaction opens. The anchor probe
runs on the base connection too, after the fact stream lands and before any
projector work is enqueued. It takes none of the commit transaction's locks and
touches no claim, lease, or queue path, but the commit transaction stays open
while it runs, so a probed commit holds its shared deferred-maintenance barrier
and its inserted rows that much longer (up to the measured 0.49 s). The verdict is a pure function
of the values read (`DecideSuccessor`, `DecideDeltaFileUnion`).

## Observability Evidence:

- Metric: `eshu_dp_ingestion_successor_guard_decisions_total{kind,rule,outcome}`,
  one increment per guarded commit. `kind` is `full` or `delta`; `rule` is the
  closed `telemetry.SuccessorGuardRule*` set (`trusted_deletion`, `no_prior`,
  `orphan_fallback`, `empty`, `collector_ratio`, `anchor_missing`,
  `file_union_bound`, `file_union_exact`, `other`); `outcome` is `stored` or
  `refused`. No scope, repository, or generation labels.
- Log stage `successor_guard` on every guarded commit: `successor_kind`,
  `rule`, `refused`, `prior_collector_fact_count`, `prior_reducer_fact_count`,
  `prior_sizing`, `successor_fact_count`. For deltas also `anchor_file_count`,
  `touched_path_count`, `successor_live_file_count`, `union_file_lower_bound`,
  and, when the exact probe ran, `union_file_count` and
  `probe_duration_seconds`.
- Log stage `refuse_destructive_successor` now carries `rule`,
  `prior_reducer_fact_count`, and `prior_sizing`. Log stage
  `successor_delta_scope_mismatch` marks a stream that disagrees on being a
  delta.
- The existing `eshu_runtime_refresh_skipped_by_reason_total{reason="destructive_successor_refused"}`
  still counts each refusal.

At 3 AM an operator who sees repositories falling behind reads the counter by
`rule` and `outcome`: a refusal rate on `collector_ratio` or `file_union_exact`
is real shrinkage or a collector fault, and the `successor_guard` line for the
scope names the counts that decided it. Coverage rows are in
`docs/public/observability/telemetry-coverage.md` and
`docs/public/reference/telemetry/metrics-ingestion-collectors.md`.

Related: `docs/internal/evidence/8362-uncountable-prior-guard.md` (the orphan
prior fallback this change keeps), `docs/public/operate/graph-delta-union.md`
(the anchor rule the guard shares with the union read).
