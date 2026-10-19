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
the arbiter ruling, round 3 ([posted on #7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6084044833)),
and the arbiter ruling, round 4 ([posted on #7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6085855231)).
Source check: origin/main f4f14a4096, 2026-10-19, for
[Re-Anchor At f4f14a4096](#re-anchor-at-f4f14a4096) and the sections after it.
The correction lists before it are records, each checked at the SHA it names
(195337b97, 3b03f018e, or c88c3806a), and their citations are not rewritten.

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
9. **The comment at `reducer/intents/shared/worker/selection.go:179-183`**, "cannot
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
  cycle ([lock-order section](7766-repository-retirement-phase1-locking.md#lock-order-rule-and-the-fail-fix)).
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
- An earlier draft of this design had admin reopen closed by phase 1. The posted
  round 3 ruling already lists it among the succeeded-only writers that 2q
  closes, so the ruling needed no correction. With the pointer NULL the reopen
  resolves the newest generation, not an error (`admin/store/reopen.go:28-39`),
  so phase 1 does not close it.

**Review items (P3):**
- The stalled R79 run in the fleet-pause table was run 1, not run 10.
- Commit added 1.8 to 19.0 ms to the design-form section (the design said 2.7
  to 19).
- The tuned Ww5000 7c touched 106 to 170 rows and 7a touched 107 to 171. The
  design's "32 ms on the stalled one" for tuned 7c measured only those rows, not
  5,000 (concurrency file, 7c bullet).
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

## Round 4 Corrections

The round 4 ruling and the round 2 review of this design found these. Each is
fixed in the file named.

**Ruling section 7:**
- N1: "one atomic group that retracts the published generation's nodes" is
  precise for entity nodes and removed files only. Kept files are `MERGE`d, not
  deleted. The #7130 comment spans `projector_queue_claim_sql.go:114-121`, not
  `:114-116`. "Nothing is lost" is true as a graph claim and false as a Postgres
  claim, so the sentence is replaced, not deleted (design file, Two
  Deliverables). Waiting for the Run does not remove the post-Ack,
  pre-reducer frozen state.
- N2: the Deliverable 1 churn does not come from 7a's `failed` to `superseded`.
  It comes from the replay floor's fallback to the latest generation, and it
  equals today's active-scope replay (writers file, Interim Behavior). Freshness
  is a wrong-truth surface, not only an unlabelled one.
- N3: accurate. The "insert arm" is the `enqueueReducerBatch` call at
  `reducer_queue_replay.go:278-281`.
- N5: the scratch harness is 1,177 lines, not about 1,000, and both
  `run_p2_bench.sh` and the P1 driver embedded a local DSN with a password. The
  committed wrappers read `PROOF_PG_DSN`. The main design and the evidence note
  contradicted each other on the harness; both now say the outputs, SQL, and
  wrappers are committed and the drivers are described.
- The writer table's "Heartbeat refuses" sentence described a backstop, not the
  fence. `LeaseDuration` is no longer NOT_CHECKED.

**Review items (P3), round 2 review:**
- Admin reopen attribution (above, round 3 list).
- The acceptance-unit gate takes the repo key in exclusive mode
  (`repo_dependency_acceptance_gate.go:72`) and is not a draining reducer. The
  acceptance writer takes it in shared mode (`shared_intent_acceptance_writer.go:83-87`),
  which is what a drain under the exclusive key could stall on. The design's step
  4 now says so. The option of making the step 6 `InflightReducersError` restart
  like step 4 is adopted: step 6 restarts on a live lease and returns 409 only
  on deadline exhaustion.
- Evidence note figures: the P1 driver is 389 lines in `main.go` plus 202 in
  `block.go`. The seed scripts inserted 2,126,917 generations and 9,175,661
  work items (about 2.13M and 9.18M, as insert counts), not 2.2M and 8.0M. Seed
  04 was not reproduced in the note; it is committed now. The `w` and `Bk`
  families come from seed 05. The tuned Ww5000 runs touched 107 to 171 rows for
  7a and 106 to 170 for 7c. The custom-plan minimum for the marker lookup at 100
  and 10,000 rows is 0.012 ms, not 0.011. The "150 s projector claim" has no
  output file and is marked unrecorded.
- "The cause was not investigated" is reworded to say no run examined why the
  row counts are low (evidence note, proof file).
- The F11 fleet-throughput bar is added to P1q.
- The ACK trigger text is read (NOT_CHECKED, closed list above).
- The writers file's interim paragraph no longer says a projector must outlive
  its lease: a live lease was enough before round 4, and phase 1 now waits it
  out.
- The shared-intent admin reopen (`admin/store/reopen.go:124-134`) is in the
  writer table as harmless, because 2b deletes those intents.

**Citations corrected at `c88c3806a`** (#7724 restructured the shared worker
after `3b03f018e`):
- `worker/process.go:301` (claim) is `:137`; the release (`:339`) is the deferred
  call at `:175`; the edge write is `:238`; `MarkIntentsCompleted` (`:421`) is
  called at `:310` from the helper at `:292-316`.
- `worker/heartbeat.go:76-80` is `:32-38` (interval), `:60` (ticker), and `:78-80`
  (the claim call).
- `worker/selection.go:98-101` (orphan skip) is `:139-147`.
- `process.go:174-177` (empty batch at the cap) is `selection.go:233-238` and
  `:322-339`; the comment at `process.go:145-149` ("cannot starve at the scan
  cap") is `selection.go:179-183`.
- `worker/runner.go:30` (`DefaultLeaseTTL`, 60 s) is unchanged.

## Re-Anchor At f4f14a4096

Main moved 2,738 commits between c88c3806a and f4f14a4096. The design was
re-anchored twice: at ef5703ec26 (2026-10-15) and at f4f14a4096 (2026-10-19).
Every `file:line` citation in the design files was mapped to the new SHA. Where
the cited lines moved unchanged, only the number changed. The rest were re-read.
None of the changes below breaks lock order, the fences, the #7130 claim branch,
I1 to I4, or how `upsertIngestionScope` binds status. The correction lists above
are not rewritten.

**Changes that alter the design text:**
- **#7852 shipped** the bounded fence the design proposed, so phase 1 reuses it
  and decides four semantics differently
  ([Reuse Of The #7852 Fence](7766-repository-retirement-phase1-locking.md#reuse-of-the-7852-fence)).
- **#7853 shipped.** The selectors drain generation-gone orphans, and
  `MarkIntentsCompleted` returns a count. The "skipped forever" premises in Why
  2b Goes First are rewritten; 2b first still holds, and #7853 is a backstop.
- **#8205** made the reducer claim sweep lock rows in C order. 2q Step A now
  takes the same shape and retries `40P01`; it was an unordered multi-row
  UPDATE that could deadlock against the replays.
- **Retention's cascade grew** (#7420 fact-key sidecars, #8261 C-order work
  lock, #7427 search-index pre-delete; migrations 188 to 191 dropped FKs). The
  runner's 2d now reuses retention's step list instead of a copy.
- **The demotion reasons** cite #8359's proven edgeless-reap predicate and say
  why it still cannot delete a Repository.

**Moved or renamed code:**
- Deferred backfill is package `storage/postgres/backfill` (afeeba19c), and the
  targeted pass moved into it (`backfill/targeted/write.go`, #7648).
  `backfill.LatestGenerationCTE` keeps the any-status fallback. The gate goes
  behind the package's `Store` seam, because it must not import its parent.
- Ack is `projector_queue_ack.go`. Fail is unchanged
  (`projector_queue_sql.go:195-267`), so PR 4a is still owed; its comment is
  `projector_queue_ack.go:65-73`.
- The canonical node Cypher is `storage/cypher/canonical/node/templates.go`, and
  the orphan sweep is `storage/cypher/sweep/orphan/` (#6694).
- The package-manifest backfill is `storage/postgres/consumption/`.

**Writers the design did not list,** each added to the writer or lock-order
table: the #7978 duplicate-generation reproject, refinalize's #7797 delta
reindex request, the #8169 service-catalog re-trigger, the `failed` and
`dead_letter` replays, and the opt-in poison-liveness auto-retry.

**Point-in-time numbers at f4f14a4096:** the marker migration is 207 (206 is
the latest, and no open PR adds one). `scripts/lib/dirgate-grandfather.tsv` is
gone; `go/cmd/dirgate` ratchets growth through a `Dirgate-Growth:` trailer.
`ingestion.go` is 499 lines, `generation_retention.go` 474, and
`paths/status/admin.go` 485.

**Table census:** every table the c88c3806a census left NOT_CHECKED is
classified, and so are the tables migrations 170 and 178 to 206 added (see the
[census](7766-repository-retirement-table-census.md)).

## Draft Review

**N7 (P2, Deliverable 2): Parameter nodes.** Parameter nodes carry no
`repo_id`, and G5 retracted them through a path list paged from `content_files`.
After 2d and 2e that list is gone, and G6 has cleared `Repository.path`, so a 2c
re-run could not retract a leftover Parameter, and the 2z census could not see
one. Fixed with the root-anchored G5p and the `graph_repo_root` column (runner,
[Parameter Nodes (G5p)](7766-repository-retirement-runner.md#parameter-nodes-g5p)).
The root anchor was chosen over a snapshot of the paths because it also covers
Parameters that full refreshes already leaked, and the snapshot does not.

**Re-review at f4f14a4096 (round 1 of this revision).** The earlier six P3s
were not recorded, so a fresh deep review re-derived the findings. Each is fixed
in the page named:
- F1 (P1): G5p could delete a live repository's Parameters when a stale scope
  shares its root after a re-key. A shared root now skips G5p, and G5f retracts
  the retired id's Files when its Repository node is gone (runner).
- F2 (P1): other repositories' per-commit backfill and resolver could keep
  writing intents keyed to the retiring repository after 2b. Both are fenced
  from phase 1, 2b deletes the sourced relationship rows first, and 2z counts by
  `repository_id` and resumes Postgres residue at 2b (main design, concurrency,
  runner, census).
- F3 to F8 (P2): the evidence note's name repeated its directory; five
  citations did not support their claims; the #7852 reuse section contradicted
  itself; steps 5, 7, and 8 ran under step 3's long `lock_timeout`; a re-admitted
  repository would read `retired`; I0 overclaimed which projector rows are
  terminal.
- Thirteen P3s: wording and evidence details, fixed inline (for example the Ack
  comment range, the Neo4j transaction shape, the 2d' "last batch" definition,
  and the gate's returned `retirement_id`).

## Out Of Scope: Pre-Existing Issues

The proof exposed two defects that predated #7766: #7852 and #7853. Both are
fixed on main now, and #7766 does not depend on either.

- **Refinalize `LOCK TABLE` convoy (#7852), fixed by PR #8207.**
  `AcquireReducerClaimFence` sets `lock_timeout` to 1 s before `LOCK TABLE
  fact_work_items IN EXCLUSIVE MODE` and restores the prior value after the grant
  (`rebuild/reset/claim_fence.go:16`, `:150-152`, `:206-222`).
  `RefinalizeClaimFenceWithRetry` (`rebuild/reset/retry.go:24-89`) retries the whole
  transaction on that `55P03` after a 100 to 500 ms jitter, under a 30 s budget
  (`claim_fence.go:20`), then returns 409 `blocked/claim_fence_busy`
  (`recovery/replay.go:16-18`). It runs from `storage/postgres/recovery.go:318-331`.
  This is the shape the design proposed. The pair-bound form shipped first, so
  #7766 PR 5 reuses it (concurrency,
  [Reuse Of The #7852 Fence](7766-repository-retirement-phase1-locking.md#reuse-of-the-7852-fence)).
- **Orphan-intent starvation and silent 0-row completion (#7853), fixed by
  d3cd68415, 00d1568e8, and 9b4274f64.** The fix follows the rule this file set:
  an absent-acceptance intent is marked completed, reason
  `acceptance_absent_generation_gone`, only when its `scope_generations` row is
  absent (`reducer/intents/shared/worker/absent_acceptance.go:91-138`, probe at
  `storage/postgres/shared_intents_first_projection.go:215-260`). A torn write
  whose generation row exists is kept and WARNed, and `MarkIntentsCompleted`
  returns the rows matched (`shared_intents.go:265-287`). Two differences: a
  short mark is a WARN, not a metric, and the fix treats generation-id stability
  as settled (`shared_intents_first_projection.go:211-214`), which this file
  still lists as NOT_CHECKED.
- **Leaks the census found.** A full refresh retracts Parameters only for the
  current files, so a removed file's Parameters stay as nodes with no edges
  (runner, G5p). `relationship_evidence_facts`, `relationship_candidates`,
  `resolved_relationships`, and `relationship_generations` have no deleter, so
  their rows accumulate across generations (census, Relationship Tables).
  #7766 removes both for a retired repository only. Neither is filed yet.
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
- Provenance of the P1, P2, and P9 numbers: the evidence note indexes the
  results and the method, and its directory holds the raw per-run outputs, the
  fixture SQL, and the shell wrappers. The Go drivers are described, not
  committed (round 4, N5).
- Projector `LeaseDuration` defaults: 1 min for the projector service
  (`cmd/projector/runtime_wiring.go:72`), 5 min for the ingester's in-process
  projector (`cmd/ingester/wiring.go:304`), 1 min for bootstrap-index
  (`cmd/bootstrap-index/wiring.go:130`).
- ACK dirty-reopen trigger text (migration 093): it fires only on a `BEFORE
  UPDATE OF status` of a reducer row where `OLD.cross_scope_replay_required` and
  `OLD.status IN ('claimed','running')` and `NEW.status IN ('succeeded',
  'retrying','dead_letter','failed')` (`093_cross_scope_completion_queue.sql:150-159`).
  `NEW.status = 'succeeded'` returns the row to `pending` once; any other target
  clears the flag (`:108-135`). It never fires for a move to `superseded`, so 2q's
  mark is not undone by it. The sibling `fact_work_items_cross_scope_completion`
  trigger (`:212-224`) is an `AFTER UPDATE` that writes
  `cross_scope_completion_events` only.

**Open**
- **Projector lease.** Whether a projector mid-write is guaranteed stopped once
  `claim_until` passes. The lease contract says so for every lease in the system
  today, and a Run that ignores it is outside the contract.
- **Tuned numbers.** Commit-mode tuned figures and any 25-repo tuned figure
  (P1'). Why 7a and 7c touched only 106 to 171 rows on the tuned Ww5000 runs: no
  run examined it.
- **Gate benchmark.** P2 measured a harness copy of the scope upsert with four
  runs at facts=400, not the real PR 3 code (P2').
- **Generation id reuse.** Whether a deleted `generation_id` can be re-created
  on re-ingest. The #7853 drain relies on it: a drained intent re-emitted with
  the same `intent_id` stays completed. Git snapshot ids hash `observed_at` at
  nanosecond resolution (`collector/repo/git/source_processing.go:151-158`);
  other collectors are unread.
- **Pre-barrier edges.** Whether the retract plan covers shared-domain edges
  written by pre-barrier cycles (the P10 census covers it once built).
- **Reducer replay.** Whether `ReplayWorkloadMaterialization` callers can name a
  retired scope's generation between 2q and 2d (reasoned structurally only).
- **Runtime cost of the 2z census.** P10' is the bar.
- **Identity target writers.** Who writes `identity_role_scope_targets` and
  `identity_role_repository_targets`.
- **Census classification.** The three items left in the
  [census](7766-repository-retirement-table-census.md#not_checked-blocks-pr-7),
  the `repo` key of `function_*`, the winners rebuild cadence, and the
  changed-since reaper cadence.
- **Parameter leak.** That a full refresh leaves the Parameters of a removed
  file with no edges is from source, not run live.
- **Workload and semantic nodes.** Whether workload, semantic, or other
  reducer-materialized nodes carry `repo_id` and survive G1 to G5 (P10).
- **Ingester catalog.** Whether the in-process repository catalog cache ever
  evicts a deleted repo id.
- **Slug form.** The `repo_slug` form stored for GitLab and Bitbucket scopes,
  against the trigger form `provider/<full_name>`
  (`webhook_trigger_selector.go:474-487`). The webhook gate depends on it.
- **NornicDB.** Whether NornicDB honours the demotion `SET` and the sweep reap
  the way Neo4j does. Neo4j is the supported backend, so this is not a blocker.
- **#7765.** GitHub fork filtering in `githubOrg`.
- **Round 3's own proof.** The ruling ran no command against Postgres; every
  claim in the lock-order and reducer-writer analysis is from source until the
  RED tests run.
- **Performance numbers.** Every performance number outside P1, P2, and P9:
  none has been measured yet.
- **P9 after #7724 and #7853.** P9 ran at `3b03f018e`. Since then #7724
  restructured the shared worker, and #7853 drains orphans whose generation row
  is gone, so P9's starvation result now holds only for torn writes. P9 was not
  re-run. The #7853 live test (`shared_intents_absent_acceptance_drain_live_test.go`)
  supersedes the earlier starvation discrepancy.
- **Round 4's own proof.** The ruling ran no command against Postgres or Neo4j;
  the projector-wait and settle analysis is from source until the RED tests run.
- **Partial projector Run.** Whether `writeContentProjection` is one
  transaction, and whether a 500-row reducer `Enqueue` batch is. Both bound how
  partial a cancelled Run can be. The design does not depend on either.
- **Driver cancel semantics.** Whether a Neo4j `ExecuteGroup` already sent can
  commit after context cancellation.
- **Failed generations.** Whether a `failed` generation ever holds succeeded
  reducer rows (the marginal 7a churn case).
- **NornicDB projector write.** The `PhaseGroupExecutor` path is non-atomic, so a
  cancelled Run can leave a partial canonical write there. Neo4j is the supported
  backend.
- **Evidence file types.** No gate in `specs/ci-gates.v1.yaml` or
  `static-contract-gates.yml` rejects CSV under `docs/internal/evidence/` (a read,
  not a gate run).
