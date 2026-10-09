# Repository Retirement: Corrections And Open Items (#7766)

Issue: #7766
Companions: [Repository Retirement](7766-repository-retirement.md) (the design
and the two deliverables) and
[Proof And Rollout](7766-repository-retirement-proof-and-rollout.md).

This file lists what the rulings and reviews found false or imprecise, the
issues kept out of scope, and what is NOT_CHECKED. The design follows the
adjustment, not the original wording.

Binding inputs: the arbiter rulings on
[#7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6073882598) and
[the prove-first results](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6082885964),
and the arbiter ruling, round 3 (to be posted on #7766).
Source check: origin/main 3b03f018e, 2026-10-09.

## Corrections To The First Arbiter Ruling

The review of this design against origin/main 195337b97 found seven premises in
the first arbiter ruling that were false or incomplete.

1. **"Leave the disconnected stub to the orphan sweep" does not work as
   stated.** A retired Repository node is `evidence_source='projector/canonical'`
   (`canonical_node_cypher.go:124-129`), and the sweep's Repository guard
   excludes exactly that value (`orphan_sweep.go:410-412`), so the sweep never
   reaps it. Adjustment: demote the node by setting properties inside the
   projector-owned set. That keeps contract A and the property-writer guard
   intact, and the sweep then reaps it (Graph Retraction, G6).
2. **The reused fence covers reducer leases only.** `countInflightReducers`
   filters `w.stage = 'reducer'` (`refinalize.go:142-151`). It does not fence
   projector leases or collector commits. Adjustment: take the exclusive
   advisory lock that commits already take in shared mode
   (`lock/deferred_maintenance.go:32`, `ingestion.go:219`), supersede the
   generations, and add a phase-2 wait for projector leases.
3. **`supersededProjectorGenerationFence` is the replay fence, not the claim
   fence** (`recovery.go:119`, `projector_queue_sql.go:307-316`). The
   claim-side equivalent is the #7130 branch
   (`projector_queue_claim_sql.go:158-179`). Adjustment: fence the projector by
   superseding the generations, with no claim SQL change.
4. **Where the per-scope generation counts come from.** "Per-scope p99 79 and
   max 3,280 generations" is not from ADR 2248. The source is
   `go/internal/storage/postgres/membership/README.md:160-161`.
5. **Two citations had drifted.** Selector resolution is at
   `query/admin/reindex_repository.go:55-131`; `reindex.go:121` is
   `validateReindexRepositories`. `upsertIngestionScope` binds the status at
   `ingestion_queries.go:249`; `:239` is the query argument.
6. **The fence list was incomplete.**
   - Clearing the active pointer does not hide a scope. `latestGenerationCTE`
     falls back to the newest generation of any status
     (`latest_generation_cte.go:41-52`), so deferred backfill
     (`ingestion_backfill.go:333`) and freshness resolution still see a
     retiring scope. Backfill is an extra writer that must check the marker.
   - Phase 1 must lock scope rows `FOR NO KEY UPDATE` before the table lock, or
     it deadlocks against refinalize's FK inserts.
7. **The policy order is not followed.** The retention policy deletes content
   before it repairs the graph. This design runs the graph step first. The
   deviation is explicit (Risks And Rejected Alternatives in the design) and
   sits inside the ruling's non-goals.

## Corrections From The Results Review

Found false or imprecise in the review of the prove-first results:

1. **The P1 pass text "7a touches ≤ 3 rows"** counted statuses; it is not an
   invariant. A stalled scope updated 5,000 rows. The pass text now reads
   "every non-superseded generation of the scopes".
2. **Design 2a, "`now() > shared_lease_horizon` ... has finished or lost its
   lease"** is false: a heartbeat keeps renewing the lease past the horizon
   (P9H). The barrier replaces it.
3. **The P1 fail-arrow "Move 7d/7e to phase 2"** is wrong for 7d. The reducer
   claim's supersede CTE requires `scope.active_generation_id =
   active_generation.generation_id` (`reducer_generation_filter_sql.go:180`),
   and 7b nulls that pointer. 7e can move; it costs 0.5 ms and stays.
4. **The NOT_CHECKED claim "every shared-domain writer holds a partition lease
   while writing"** was confirmed for the shared worker and code_calls. Round 3
   closes it for repo_dependency by source read (invariant I1).
5. **A working note put the convoy on the advisory wait.** It comes from step
   6's `LOCK TABLE` inheriting step 3's `SET LOCAL`. The advisory wait convoys
   nothing.
6. **A working note called repo_dependency "serialized with phase 1" by the
   advisory lock** at `storage/postgres/repo_dependency_acceptance_gate.go:72`.
   Only the `fn` inside that transaction is (`:72-86`). The graph write runs
   inside that `fn`, under the partition lease (invariant I1).
7. **Line references drifted in the working notes.** The repo_dependency skip is
   at `repo_dependency_projection_runner.go:348-350` and its cap error at
   `:366-370`, not `:340-343`.
8. **The working notes' tuned "p99" figures** are n=10 rollback-mode maxima and
   are not comparable to the n=20 commit-mode design-form totals.
9. **The comment at `reducer/intents/shared/worker/process.go:145-149`**, "cannot
   starve at the scan cap", is false within one partition (P9 starvation run).

Two items the first ruling listed as not verified are closed:
- The content-catalog fallback for GET `/api/v0/repositories` reads
  `ingestion_scopes WHERE scope_kind='repository'`
  (`query/content_reader_repository_catalog.go:16-32`).
- Migration 166 has `CHECK (selector_kind IN ('github_org','explicit'))`. This
  matters to #7765, not to this design.

## Round 3 Corrections

The round-3 ruling and the deep review that preceded it found these. Each is
fixed in the design file named.

**Ruling section 5:**
- origin/main is `3b03f018e`, not "around 7be568e44" (an ancestor, not the tip).
- F1: `reducer_queue.go:24-35` is `:25-35`, and `ReplayDomain` has no production
  caller. Otherwise accurate.
- F2: "if `Fail` starts waiting first" understates it. `Fail` always loses this
  cycle ([lock-order section](7766-repository-retirement-concurrency.md#lock-order-rule-and-the-fail-fix)).
- The design's "No row is left to claim" (writer table) and the old I2 were
  false. They are replaced by the 7d mark and the restated I2.
- The concurrency file's "`ingestion.go:219` precedes every write" is false as
  written: `HeartbeatClaim` at `:213` writes `workflow_claims` and
  `workflow_work_items` first, and `:222` writes after the key. Neither touches
  `fact_work_items` or `ingestion_scopes`, so the cycle is unaffected.
- `projector_queue.go:214-220` says `Fail` takes the scope row first: true at
  row level, false at relation level. PR 4a fixes the comment.

**Citations and names that drifted since the design was written:**
- `projector_queue_sql.go` helpers moved by about 43 lines. The Heartbeat class
  constant is at `:240` (was `:230`), the Ack-refusal marker is at `:275-305`
  (the old `:230-262` range), and `supersededProjectorGenerationFence` is at
  `:307-316` (was `:264-273`). Ack's activation refusal is at `:108-116`.
- `TestGenerationRetentionStoreLargeFixtureIntegration` is now
  `TestGenerationRetentionStoreLargeFixtureLive` (#7756).
- `BenchmarkCommitScopeGeneration` does not exist on main. The P2 harness used a
  scratch `BenchmarkCommitScopeGenerationLive`. PR 3 adds the commit-path
  benchmark.
- Migrations 167 (`167_unroutable_intents_generation_idx.sql`) and 168
  (`168_webhook_refresh_triggers_claim_fencing.sql`) now exist. The marker
  migration takes 169, re-checked at PR time.
- `repo_dependency_projection_runner.go` cap error is `:366-370`, not `:366-369`.
- `paths/status/admin.go` is 461 lines, not 460. `ingestion.go` is 490 lines.
  `storage/postgres` is pinned at 335 files, not 334.
- New code since the design: `projector_queue_zombie_heal.go` (#7209), inert
  after 7b because it needs a non-NULL pointer; and the #7734 admin reopen
  rollover fence (`admin/store/reopen.go:199-213`).
- Round 3 named `fact_work_item_audit` as a `SET NULL` table. That is the
  migration file name (`006_fact_work_item_audit.sql`); the table is
  `fact_backfill_requests`.
- Round 3's draft listed admin reopen as closed by phase 1. It is closed by 2q:
  with the pointer NULL it resolves the newest generation, not an error
  (`admin/store/reopen.go:28-39`).

**Review items (P3):**
- The stalled R79 run in the fleet-pause table was run 1, not run 10.
- Commit added 1.8 to 19.0 ms to the design-form section (the design said 2.7
  to 19).
- The tuned Ww5000 7c touched 106 to 170 rows and 7a touched 107 to 171.
- `ingestion.go:213` (`HeartbeatClaim`) precedes `:219`.
- I3 ignored generation retention's cascade, which also deletes acceptance
  rows. That is harmless: retention deletes the generation's intents first.
- The step 4 re-run drain holds the advisory key. The design now rolls back and
  restarts at step 1 instead, because a draining reducer can need the shared
  key.
- `INSERT ... ON CONFLICT DO NOTHING` returns no row. Step 7g reads the
  existing open row when `RETURNING` is empty.
- The labels `Rw` and `Ww` were never defined. They are defined in the
  [prove-first table](7766-repository-retirement-proof-and-rollout.md#prove-first-table).

## Out Of Scope: Pre-Existing Issues

Two defects the proof exposed predate #7766. #7766 references them and does not
depend on them: #7852 and #7853.

- **Refinalize `LOCK TABLE` convoy (#7852).**
  `AcquireReducerClaimFence` runs `LOCK TABLE fact_work_items IN EXCLUSIVE
  MODE` (`rebuild/reset/refinalize.go:65-66`, `:381`, driven from
  `storage/postgres/recovery.go:369`) with no `lock_timeout` anywhere in
  `recovery.go` or `refinalize.go`. The fix is a bounded-wait and retry fence
  helper in `rebuild/reset`. #7766 PR 5 builds the scope-bound form, and the
  refinalize issue adopts the same shape for its pair-bound form.
- **Orphan-intent starvation and silent 0-row completion (#7853).** All
  three shared selectors skip an intent whose acceptance row is absent, forever,
  and `MarkIntentsCompleted` ignores rows affected (P9). Rule for the fix: mark
  an intent completed (reason `acceptance_absent_generation_gone`) only when its
  `scope_generations` row is absent. Count and WARN, without draining,
  absent-acceptance intents whose generation row exists: a retry re-upserts the
  same `intent_id` and the upsert never reopens `completed_at`
  (`storage/postgres/shared_intents_upsert.go:53-66`), so a premature drain
  loses the edge for good. `MarkIntentsCompleted` must return the affected
  count, and the worker must count `affected < requested`. Prove-first for that
  issue: RED/GREEN with 10,100 orphans and one healthy intent (the healthy one
  processed in cycle 1 after the drain, one extra query per cycle, a torn-write
  intent not drained), reproduced deterministically.
- **Claim-side supersede arm for retired generations.** If 2z ever finds
  residue after a clean 2c, the structural fix changes general reducer queue
  semantics. It gets its own issue and prove-first (see the
  [writers file](7766-repository-retirement-reducer-writers.md#residual-risk)).

## NOT_CHECKED

**Closed by this rework**
- repo_dependency I1: confirmed by source read (not live-proven); see
  invariant I1.
- The column order of `fact_work_items_scope_generation_idx`: `scope_id` leads
  (`migrations/005_fact_work_items.sql:30-31`).
- Provenance of the P1, P2, and P9 numbers: the evidence note now carries the
  results, the method, and the harness SQL. The Go harness is partly omitted;
  the note lists what.

**Open**
- **Projector lease.** The default `LeaseDuration` (the 2a bound in seconds),
  and whether a projector mid-write is guaranteed stopped once `claim_until`
  passes.
- **Tuned numbers.** Commit-mode tuned figures and any 25-repo tuned figure
  (P1'). The cause of the 106 to 171 row counts on the tuned Ww5000 runs.
- **Gate benchmark.** P2 measured a harness copy of the scope upsert with four
  runs at facts=400, not the real PR 3 code (P2').
- **Starvation discrepancy.** Why one earlier starvation run processed the
  healthy intent in 10 ms and the later run processed none.
- **Generation id reuse.** Whether a deleted `generation_id` can be re-created
  on re-ingest. It affects the drain rule in the orphan-intent issue.
- **Pre-barrier edges.** Whether the retract plan covers shared-domain edges
  written by pre-barrier cycles (the P10 census covers it once built).
- **Reducer replay.** Whether `ReplayWorkloadMaterialization` callers can name a
  retired scope's generation between 2q and 2d (reasoned structurally only).
- **ACK dirty-reopen trigger.** Its text was not read; the design relies on the
  comments at `reducer_queue_claim_query.go:19-27` and
  `reducer_queue_replay.go:69-72`.
- **Runtime cost of the 2z census.** P10' is the bar.
- **Identity target writers.** Who writes `identity_role_scope_targets` and
  `identity_role_repository_targets`.
- **Census classification.** The tables listed as NOT_CHECKED in the
  [census](7766-repository-retirement-table-census.md), the `repo` key of
  `function_*`, the winners rebuild cadence, and the changed-since reaper
  cadence.
- **Workload and semantic nodes.** Whether workload, semantic, or other
  reducer-materialized nodes carry `repo_id` and survive G1 to G5 (P10).
- **Ingester catalog.** Whether the in-process repository catalog cache ever
  evicts a deleted repo id.
- **Slug form.** The `repo_slug` form stored for GitLab and Bitbucket scopes,
  against the trigger form `provider/<full_name>`
  (`webhook_trigger_selector.go:298-311`). The webhook gate depends on it.
- **NornicDB.** Whether NornicDB honours the demotion `SET` and the sweep reap
  the way Neo4j does. Neo4j is the supported backend, so this is not a blocker.
- **#7765.** GitHub fork filtering in `githubOrg`.
- **Round 3's own proof.** The ruling ran no command against Postgres; every
  claim in the lock-order and reducer-writer analysis is from source until the
  RED tests run.
- **Performance numbers.** Every performance number outside P1, P2, and P9:
  none has been measured yet.
