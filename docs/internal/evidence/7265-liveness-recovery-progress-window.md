# #7265 Liveness Recovery Progress Window

Generation-liveness recovery reopened succeeded `projector/source_local` rows
for generations whose shared intents were still draining. This record holds the
cause, the arbiter rulings, the measurements that picked the SQL shape, and the
test and telemetry proof.

## Root Cause

Root-Cause Evidence: `recoverWedgedActiveGenerationsQuery` treated any aged
active generation with an actionable outstanding `shared_projection_intents`
row as wedged, provided its reducer work had drained and no source-local
projector row was in flight. Nothing in that gate asked whether the blocking
intent's domain queue was still moving. A generation whose intents sat behind a
busy shared queue, such as a large bootstrap `code_calls` backlog, therefore
had its succeeded source-local row reopened. That launched a duplicate
canonical replay while the downstream queue was already making progress.

The live RED run shows the defect on unmodified `origin/main` (182a31124).
`TestGenerationLivenessProgressWindow` failed 6 of 9 subtests, rc=1:

- A generation whose `code_calls` queue completed a minute ago, or 10m-1s ago,
  counted as `stuck`.
- The mixed-domain generation counted as `stuck`.
- The lookalike `repo_dependency` generation counted as `stuck`.
- The transition first sweep reported `Recovered = 1, want 0 (draining)`.
- The four-bucket gauge returned `map[aging:1 fresh:1 stuck:2]`.

The 3 subtests that passed are the genuinely wedged cases, which both old and
new code re-drive.

## Binding Rulings

1. **Wedged gate.** A generation is wedged only when it passes the existing
   actionable-outstanding-intent gate AND has no actionable outstanding intent
   whose `projection_domain` completed any intent, for any generation, after
   `now - ProgressWindow`.
2. **Follow-up refinement.** Progress is tracked per domain queue, so the
   progress probe does not filter completions on `source_run_id`. An exact
   `repo_dependency` completion proves that the `repo_dependency` queue is
   moving. The exact family (`repo_dependency` or `repo_dependency:*`) stays
   excluded only from actionability.
3. **No schema or parameter changes.** No new index and no Go-side `text[]`
   parameter. `AS MATERIALIZED` is allowed only if a plan shows the domain set
   evaluated more than once.
4. **Absolute bounds.** On the fixture below, recover and gauge count each need
   a median of at most 250 ms and no Seq Scan on `shared_projection_intents`.
   The same must hold with in-window completions doubled.

The first proof, run against the original 2x ratio criterion, failed for
candidate A (a direct per-intent probe, 219.6 s) and for the DISTINCT-domain
CTE (2.5x to 3.5x). The arbiter then accepted the absolute bounds with the
refinement in item 2. The earlier plans are kept with the artifacts listed
below.

## Fixture

- **Database:** PostgreSQL 18.6 (`postgres:18-alpine`), a disposable container
  with `shared_buffers=256MB` and `work_mem=16MB`. Default JIT settings apply
  (`jit_above_cost` 100K, optimize and inline at 500K).
- **DDL:** taken verbatim from migrations 001, 002, 005, 008, 043, 091, 108 and
  113. That includes `shared_projection_intents_pending_idx (projection_domain,
  completed_at, created_at)` and `shared_projection_intents_generation_pending_idx`.
- **Generations:** 1,200 active (1,000 aged 2 h, 200 fresh), 20,000 superseded,
  and 207,200 `fact_work_items`.
- **Pending intents:** 300,100, about 300 per aged generation:
  - gens 1..400 wait in `code_calls` and gens 701..850 in `inheritance_edges`.
    Both domains are progressing.
  - gens 401..700 wait in `sql_relationships`, which is quiet: its newest
    completion is 2 h old.
  - gens 851..980 hold only the exact `repo_dependency` family.
  - gens 981..1000 hold lookalike `code_import_repo_dependency:*` intents in
    the `repo_dependency` domain.
  - Every tenth aged generation also has one quiet `sql_relationships` intent.
- **Completed intents:** 1,000,000. Base seed: about 122K of them fall inside
  the 10-minute window, mostly exact-family `repo_dependency`. Doubled seed:
  250,694 in window.
- **Method:** `PREPARE` + `EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE` inside
  `BEGIN`/`ROLLBACK`. `fact_work_items` was vacuumed before each recovery run,
  and 7 interleaved rounds ran after each fresh reseed.

## Shape Selection (refined semantics, measured before any fixture was written)

Median ms over 5 interleaved runs:

| Shape | Base recover | Base count | Doubled recover | Doubled count |
| --- | ---: | ---: | ---: | ---: |
| B `IN (SELECT DISTINCT ...)` | 75.1 | 336.5 | 119.5 | 378.0 |
| C `= ANY(ARRAY(SELECT DISTINCT ...))` inline | 52.8 | 304.3 | 59.6 | 339.4 |
| D: C through an `OFFSET 0`-fenced derived table | 55.1 | 74.4 | 147.3 | 83.2 |
| E: C through an unfenced derived table | 51.7, 71.6 (2 runs) | 378 to 465 | 66.9 | n/a |

- **The domain set is cheap.** With `source_run_id` gone, the DISTINCT domain
  set is one InitPlan: an Index Only Scan with skip scan on
  `shared_projection_intents_pending_idx`, 0 heap fetches, 6 to 8 ms at 122K to
  250K window rows. That removed the 31 ms exact-family filter cost.
- **Why B and C fail on count.** In the count query the progress EXISTS sits
  inside a `CASE`, so it stays a correlated SubPlan. The planner charges the
  InitPlan cost (about 2,900) to each of the 1,200 rows, so the estimated total
  is about 7.1M. JIT inlining and optimization then take about 260 ms.
- **What the fence does.** `OFFSET 0` keeps the array as an InitPlan of the
  outer statement, which drops the estimate to about 142K (generation-only JIT,
  about 17 to 34 ms).
- **Why recover stays unfenced.** In the recovery query the same fence changed
  the join order: the newer-generation anti-join became a nested loop that
  removed 1.2M rows by join filter, reaching 147 ms on the doubled fixture.
  Unfenced, the derived table is pulled up into the C plan.

Shipped shape: both queries embed one const,
`generationIntentProgressingPredicate`, which reads
`liveness_progress.progressed_domains`. `recoverWedgedActiveGenerationsQuery`
defines `liveness_progress` unfenced and binds `$5`.
`countActiveGenerationsByAgeQuery` defines it with `OFFSET 0` and binds `$4`.
`TestGenerationLivenessProgressWindowParameters` pins both fence choices.

## Performance Evidence

Performance Evidence: final run of the exact production query text (extracted
from the Go consts), 7 interleaved runs per seed.

| Statement | Seed | Median ms | Min ms | Top-level shared hit (median) | Seq Scan on intents |
| --- | --- | ---: | ---: | ---: | --- |
| shipped recover | base | 26.0 | 25.7 | 69,248 | none |
| accepted recover | base | 53.6 | 53.2 | 158,892 | none |
| shipped count | base | 39.9 | 39.8 | 44,572 | none |
| accepted count | base | 74.9 | 74.6 | 141,019 | none |
| shipped recover | doubled | 25.5 | 24.6 | 69,248 | none |
| accepted recover | doubled | 57.3 | 56.6 | 158,968 | none |
| shipped count | doubled | 39.1 | 38.8 | 44,572 | none |
| accepted count | doubled | 79.5 | 79.1 | 141,095 | none |

- **Absolute bound:** both statements are at most 80 ms median against the
  250 ms limit.
- **Doubled-window sensitivity:** doubling the in-window completions changed
  recover by 1.07x (53.6 to 57.3 ms) and count by 1.06x (74.9 to 79.5 ms).
- **Where the time goes:** the added work is the per-generation read of
  outstanding intents through `shared_projection_intents_generation_pending_idx`
  (the ruling accepts this as the pathological bound) plus the one domain-set
  InitPlan (`loops=1`).
- **JIT:** count runs in the generation-only JIT class, the same as the shipped
  count (Total 16.8 ms).

Derived bucket expectations, from how the fixture was built:

- 1..400 (`code_calls`, progressing) are `draining` (400).
- 401..700 (quiet) are `stuck` (300).
- 701..850 (`inheritance_edges`, progressing) are `draining` (150).
- 851..980 are exact-family only, so they are not actionable and count as
  `aging` (117). The 13 mixed generations (860, 870, ... 980), whose only
  actionable intent is quiet, are `stuck` (13).
- 981..1000 are lookalike `repo_dependency` intents while exact completions
  flow, so they are `draining` (20).
- 1001..1200 are `fresh` (200).

Totals: `stuck` 313, `draining` 570, `aging` 117, `fresh` 200. The accepted
count returned exactly these totals on both seeds. The accepted recover with
batch 2000 re-drove 313 generations, which equals `stuck`.

Residual: the Index Only Scan depends on the visibility map. On a hot,
unvacuumed table the domain set pays heap fetches. The earlier bitmap-heap form
cost about 31 ms at 120K window rows, which leaves headroom under 250 ms.

## Concurrency And Budget

- **Transactions and locking.** The progress gate is a read-only predicate
  inside the existing wedged CTE, so transaction scope, lock order, and the
  `ON CONFLICT ... WHERE NOT (...)` write-time re-verification are unchanged.
  `TestRecoverWedgedActiveGenerationsQueryDoesNotClobberConcurrentlyRenewedLease`
  still passes live and under `-race`.
- **Recovery budget.** Skipped (draining) generations never reach the upsert,
  so `liveness_recovery_attempts` is untouched. Every draining subtest asserts
  the payload key is absent and the row is still `succeeded`.
- **Transition.** The transition subtest proves the sequence: skip, then
  exactly one re-drive once the domain has been quiet past the window, then a
  no-op while that re-drive is pending.
- **No serialization.** No worker count, batch size, or poll default changed.

## Observability Evidence

Observability Evidence:

- **Gauge.** `eshu_dp_active_generations{age_bucket}` gains the closed bucket
  `draining`, emitted by `countActiveGenerationsByAgeQuery` and pre-seeded to 0
  in `CountActiveGenerationsByAge`. It is the only signal for skipped
  generations; there is no per-generation skip log.
- **Re-drive log.** Each re-drive logs `generation liveness re-drove wedged
  generation` at Info from `GenerationLivenessRunner.recordResult`. The log
  carries `scope_id`, `generation_id`, `liveness_recovery_attempts` (from the
  upsert's `RETURNING`), `reason="no_intent_progress_within_window"`, and
  `progress_window`.
- **Configuration.** `ESHU_GENERATION_LIVENESS_PROGRESS_WINDOW` (default 10m) is
  clamped to at least `ESHU_GENERATION_LIVENESS_POLL_INTERVAL`, with one Warn
  at reducer startup.
- **Collection path.** The gauge callback serves a cached snapshot. The
  Postgres query runs on the background `snapshot.Refresher` every
  `ESHU_POSTGRES_GAUGE_REFRESH_INTERVAL` (default 5 m, timeout 30 s), not on
  the OTel periodic reader (SDK default 60 s). At 75 to 80 ms per refresh, the
  cost is acceptable at any interval of 15 s or more.

## Residual Risk

- A genuinely wedged generation whose blocking domain queue never goes quiet
  stays `draining`. It is visible in the gauge, and
  `POST /api/v0/admin/recover-generations` is the manual path.
- The bootstrap-only compose shape and the remote #7230-shape rerun are out of
  scope here.

## Artifacts

Plans, candidate SQL, seed scripts (`seed7265.sql`, `seed7265_doubled.sql`),
the harnesses (`explain.sh`, `bench_refined.sh`, `bench_final.sh`), and
`red-origin-main.log` are kept outside the tree in the session scratchpad
folder `7265-plans/`. The coordinator report lists the absolute path.
