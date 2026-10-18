# #7853: drain shared-projection intents with absent acceptance and gone generation

## Problem

A shared-projection intent whose `shared_projection_acceptance` row is absent
is skipped forever: never processed, never stale, never completed. Enough such
orphans fill the 10,000-row scan window and starve healthy work in the same
partition. The skip exists in three selections (shared worker, code_call,
repo_dependency), and `MarkIntentsCompleted` reports success on 0-row updates.
See eshu-hq/eshu#7853 for the failing shapes.

## Rule

Drain an absent-acceptance row only when its `scope_generations` row is absent
(cascade-deleted or never committed), under reason
`acceptance_absent_generation_gone`. Generation-present rows are kept pending
as torn writes and WARNed, never drained: a retry re-upserts the same
`intent_id` and the upsert never reopens `completed_at`, so draining one would
lose its edge the moment the acceptance row arrives. The probe keys on row
absence, never on "not the active generation" (that races activation) and
never on status. Lookup errors fail the selection; a reader without the
`AbsentGenerationReader` port keeps its pre-#7853 behavior. `MarkIntentsCompleted`
returns rows matched (not rows newly completed), and a short mark
(`affected < requested`) is a WARN, not an error.

## Safety case

- An acceptance row carries a foreign key to its scope generation, so an
  absent generation means no acceptance row can ever name the intent: it can
  never become authoritative, and completing it drops nothing projectable.
- Generation identity is stable: retention pruning already deletes generation
  rows assuming an id is never reused, and the #7766 arbiter ruling this drain
  implements rests on the same assumption.
- The shared worker drains through its normal batch mark (one statement);
  code_call and repo_dependency mark the generation-gone subset inline only on
  paths where no downstream mark survives (scan-cap error path, selected-unit
  race), and delete marked ids from the rescan set so cap-continue never
  re-marks.
- Drained ids ride the existing completed path, so crash replay is the same
  idempotent mark; torn rows stay pending and re-probe each cycle.

## Performance Evidence:

Conflict domain: `shared_projection_intents` rows of one selection window per
final round. One bounded primary-key `ANY()` probe over the window's distinct
generation ids per round that reaches an exit, plus one per cap-continue, each
making permanent progress; a window with no absent rows costs no query.
Worker/lease settings unchanged; no new index (generation_id is
`scope_generations`' primary key).

Prove-the-theory-first shape (unit): `SplitAbsentAcceptanceRows` issues
exactly 1 bounded lookup per call and 0 for empty input, asserted by
`TestSplitAbsentAcceptanceRows*` probe-count tests
(`go/internal/reducer/intents/shared/worker/absent_acceptance_test.go`).

Baseline (reported in eshu-hq/eshu#7853, measured on PostgreSQL 18.3 while
proving the #7766 design): 10,100 orphan intents plus 1 healthy intent in one
partition: one cycle 2.1 s, processed 0, healthy intent starved (10 ms with no
orphans). The RED half of the live proof below fails pre-fix by construction
(healthy never processes, first-cycle drain is 0).

After (measured this lane, disposable PostgreSQL 18.6 in docker
`postgres:18`, shared ARM host, `TestAbsentAcceptanceOrphansStarveHealthyIntentLive`):
10,100 generation-gone orphans plus 1 healthy plus 1 torn-write intent, one
partition, batch limit 100. PASS in 2.16 s wall for the whole proof (fixture
seed plus up to 5 `ProcessPartitionOnce` cycles plus end-state asserts).
Terminal row counts: 10,100 orphans marked completed, 1 torn row still pending
(never drained), 1 healthy intent processed with its edge written in the first
cycle after the drain. Per-cycle `elapsed=` is logged by the test for the
probe-plus-mark cost. The 2.1 s baseline cycle and the 2.16 s whole-proof wall
are not a before/after pair (different scopes and backends); no wall-time
speedup is claimed beyond unblocking (NOT_CHECKED on a quiet host): the win is
starvation removal, visible as `acceptance_absent_generation_gone` drain
counts once deployed.

## Observability Evidence:

- `eshu_dp_shared_projection_stale_intents_total{reason="acceptance_absent_generation_gone"}`
  from all three lanes through
  `AcceptanceTelemetry.RecordAbsentGenerationGoneIntents`, plus the
  `shared projection drained intents with absent acceptance and gone generation`
  INFO log with `stale_reason` and `stale_count`. Zero-count cycles record
  nothing.
- One WARN per cycle with surviving torn writes,
  `shared projection intents have no acceptance row but their scope generation
  still exists`, with `torn_write_count` (first row sampled).
- One WARN per short mark,
  `<runner> marked fewer intents completed than requested`, with
  `requested_intents` and `completed_intents`, for the shared, code_call, and
  repo_dependency runners.
