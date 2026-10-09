# Repository Retirement: Proof And Rollout (#7766)

Issue: #7766
Companions: [Repository Retirement](7766-repository-retirement.md) (the design
and the two deliverables),
[Concurrency Contract](7766-repository-retirement-concurrency.md),
[Reducer Reopen And Replay Writers](7766-repository-retirement-reducer-writers.md),
[Runner](7766-repository-retirement-runner.md),
[Table Census](7766-repository-retirement-table-census.md),
[Surfaces](7766-repository-retirement-surfaces.md), and
[Corrections And Open Items](7766-repository-retirement-corrections.md). The
raw results and the re-runnable harness are in the
[evidence note](../evidence/7766-retirement-prove-first.md).

This file holds the proof the design owes before and during the build, split by
deliverable, the results so far, the tests, and the PR breakdown.

Binding inputs: the arbiter rulings on
[#7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6073882598) and
[the prove-first results](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6082885964),
and the arbiter ruling, round 3 (to be posted on #7766).
Source check: origin/main 3b03f018e, 2026-10-09.

## Prove-First Table

Data shape: PostgreSQL 18, 12,000 scopes and 739,838 generations, per-scope
p50 27, p99 79, max 3,280, plus one 5,000 tail. This is the QA shape measured
in `storage/postgres/membership/README.md:160-161`; ADR 2248 does not carry
these numbers. Facts are seeded per the #2249 large fixture
(`TestGenerationRetentionStoreLargeFixtureLive`, formerly
`...Integration`; #7756 moved it to the migrated schema), scaled to 400 facts
per generation. Graph: Neo4j pinned image, fixture corpus plus the largest
fixture repository.

Shape labels used below: **R** is the realistic shape (one pending, one active,
one failed generation, the rest superseded). **W** is the stalled shape (every
non-active generation pending or failed). **M** is a 25-repo request (one 5,000,
one 3,280, and 23 of 79 generations). The suffix is the family of target
scopes: `c` cold commit-mode runs, `h` warm (hot) commit-mode runs, and `w` a
separate scope set for rollback-mode runs, measured cold (buffers evicted) or
warm (prewarmed). So `Rw5000` is the R shape with 5,000 generations in the
rollback-mode set, and `Ww5000` is the W shape in the same set.

Status after the first run: P1 and P9 failed as originally designed, and P2
passed only in its folded form. The rows below are the bars each deliverable
owes. Results are in [Prove-First Results](#prove-first-results).

### Deliverable 1: before code

| # | Theory | Shim | Pass | Fail → |
| --- | --- | --- | --- | --- |
| P1' | Tuned bindings hold the bar in commit mode, with 7d as a mark | Tuned form (7c by non-superseded pairs; 7d as `UPDATE ... SET status='superseded'`, not DELETE; recheck by `scope_id = ANY`), commit mode, n=20, cold: Rw5000 and Ww5000 at the precheck limits (5,000 generations, about 20,000 7d rows), and a 25-repo realistic group | p99 ≤ 250 ms for each. At n=20 the harness's nearest-rank p99 is the maximum. The 5,000 and 20,000 limits are P1' outcomes. | Lower the precheck limits |
| P1'' | The 1 s wait plus retry bounds the convoy, and the fleet keeps moving | Retry loop against a 5 s `ROW EXCLUSIVE` holder; time an unrelated single-row `UPDATE` and phase 1; run claim-, heartbeat-, and enqueue-shaped statements on unrelated scopes across the P1' sections | Unrelated claim throughput ≥ 90% of the no-retirement baseline over the whole run; the per-statement max stays as the second bar (unrelated `UPDATE` p99 ≤ 1.3 s); phase 1 succeeds within the 30 s budget | Shorten the wait or the budget |
| P1''' | The precheck counts are cheap | `EXPLAIN ANALYZE` of both precheck counts at 3x QA scale | ≤ 50 ms | Count from an index-only form |
| P2' | The folded gate is free on the real code | Benchmark the real PR 3 commit path with the gate folded in, 10,000 marker rows. No commit-path benchmark exists on main (the P2 harness used a scratch one), so PR 3 adds it. | Median delta ≤ 1% at facts=1 and facts=400, at least 6 runs each; the `ON CONFLICT` arm byte-identical to `ingestion_queries.go` (derived-prefix test) | Revisit the gate shape |

Checked in the PR that builds the piece: P6 (index-status grouping) and P8
(refinalize skip arm) in PR 8 and PR 3.

| # | Theory | Shim | Pass | Fail → |
| --- | --- | --- | --- | --- |
| P6 | Index-status grouping is cheap | `PROFILE` of the grouped count against the current count at 12,000 Repository nodes | ≤ 50 ms and ≤ 2x baseline | Count indexed only |
| P7 | The candidates read is cheap (PR 10) | `EXPLAIN ANALYZE` of the Candidates query at 12k scopes x 2 selectors | ≤ 200 ms | Per-selector partition |
| P8 | Refinalize skip arm | `EXPLAIN ANALYZE` of `AffectedGenerationsTemplate` with the `EXISTS` arm, all-scopes | ≤ +5% | Hashed anti-join |

### Deliverable 2: before code

| # | Theory | Shim | Pass | Fail → |
| --- | --- | --- | --- | --- |
| P1q | 2q step B is a short fenced section | On Ww5000 after step A has run, time step B (lock, mark residual, recheck, commit); time step A per 10,000-row chunk with `EXPLAIN (ANALYZE, BUFFERS)` | Step B p99 ≤ 250 ms (the fenced pass touches only the residual); step A ≤ 1 s per chunk, index scan on `fact_work_items_scope_generation_idx` (its leading column is `scope_id`, `migrations/005_fact_work_items.sql:30-31`) | Smaller step A chunks; lower the residual |
| P9a | The 2b' predicate holds a pre-barrier writer | RED/GREEN on real Postgres, P9H shape: a 4 s write, barrier at t=300 ms; 20 runs | The predicate stays > 0 until release (about 4 s) and reads 0 within one poll after; a cycle claimed after the barrier does not block it; no false pass | Revisit the barrier |
| P9b | The `claimed_at` arm is free on the heartbeat path | `ClaimPartitionLease` benchmark before and after the arm | Median ns/op delta ≤ 1% | Move the epoch out of the claim statement |
| P9c | The 2b deletes use their indexes | `EXPLAIN` of both 2b deletes at 3x QA scale | Index scans on the named indexes; ≤ 1 s per 10,000-row chunk; 0 rows remain | Add index (new proof) |
| P9d | 2b' latency is known | p99 cycle time per shared domain on the QA shape | State it. If > 5 min, the readback already names the partition | Runbook step for the operator |
| P10' | The runtime 2z graph census is index-anchored | `PROFILE` of the 2z census statements | db hits ≤ 2x at 10x store | Restrict the census to the label and edge families in the runner's plan table |

Checked in the PR that builds the piece:

| # | Theory | Shim | Pass | Fail → |
| --- | --- | --- | --- | --- |
| P3 | Purge batches are bounded | EXPLAIN every purge step (runner, step 2d) for a 100-generation batch of the 3,280 scope; run the full purge while projector load runs | Each batch ≤ 5 s and ≤ 100,000 rows; total ≤ 33 batches; active projection slowdown ≤ 10% (ADR 2248 bound) | Lower the batch limit |
| P4 | Repo-keyed deletes use indexes | EXPLAIN of the 2e deletes by `repo_id` and `scope_id` for the census `2e` rows | Index scans on `content_files_repo_path_idx` and `content_entities_repo_idx`; a named index per other table | Add index (new proof) |
| P5 | Graph retract is repo-bounded | `PROFILE` of each G5 statement with the sentinel generation and of G6 | db hits proportional to repo nodes, not store size (≤ 2x at 10x store); G6 = `NodeUniqueIndexSeek`, 1 row | Bounded drain per label |
| P10 | Post-retirement residue | Census: `MATCH (n) WHERE n.repo_id=$id RETURN labels(n), count(*)` and `MATCH (r:Repository {id:$id})-[x]-() RETURN type(x), x.evidence_source, count(*)` | 0 owned nodes; only foreign-owned incoming edges | Extend plan table |

### Original rows

These ran on 2026-10-08. They failed or passed in the form shown, and the
amended rows above replace them.

| # | Theory | Outcome |
| --- | --- | --- |
| P1 | The phase 1 critical section (LOCK TABLE to COMMIT) is short, p99 ≤ 250 ms at 5,000 generations | **Failed as designed.** Design-form Rc5000 cold p99 252.2 ms, Wc5000 cold 885.9 ms. 7a updates every non-superseded generation of the scopes: at most 3 per scope when the projector is current, every generation of a stalled scope. Fail → 7d cannot move to phase 2 (reducer claim, step 7b). |
| P2 | The commit gate is free: lookup ≤ 0.1 ms, ≤ 1% ns/op | **Lookup passed; the separate statement failed the 1% bar.** Folded into the scope upsert it passed (see P2'). |
| P9 | A shared worker drops intents whose acceptance or generation is gone | **Failed as designed.** Intent-delete barrier (see P9a). |

## Prove-First Results

P1, P2, and P9 ran on 2026-10-08. **P1 and P9 failed as originally designed,
and the design was amended per the arbiter ruling on the #7766 prove-first
results.** P2 passed only after the gate was folded into the scope upsert. The
tables, the environment, the method, and the harness are in the
[evidence note](../evidence/7766-retirement-prove-first.md). What they show:

- **P1, design form, commit mode.** Cold p99 (the maximum at n=20): R79 22.3 ms,
  R3280 96.2 ms, R5000 252.2 ms, W3280 234.7 ms, W5000 885.9 ms. The 25-repo
  group (n=10) peaked at 208.2 ms. Commit added 1.8 to 19.0 ms to the section.
  R5000 cold missed the bar by 2.2 ms, and the stalled shape missed it by a wide
  margin. "7a touches at most 3 rows" was never an invariant: the stalled shape
  updated 5,000.
- **P1, tuned form, n=10, rollback mode.** Rw5000 cold max 88.5 ms, Ww5000 cold
  max 184.9 ms. The tuned Ww5000 files record 107 to 171 rows for 7a and 106 to
  170 for 7c, not 5,000, while 7d records 19,996. So 184.9 ms does not prove the
  5,000-row 7a/7c leg; the cause was not investigated. P1' re-measures at the
  precheck limits in commit mode with 7d as a mark. These figures are
  not comparable to the design-form totals.
- **Fleet pause.** Claim-shaped proxy statements on unrelated scopes (the real
  projector claim did not finish in 150 s on the fixture, so the harness used
  statements of the same shape). Section p50 and max: R5000 47.9 and 99.4 ms,
  W5000 115.5 and 229.1 ms. The first R79 run stalled for 451,635.6 ms behind a
  leftover real claim statement of about 7.5 minutes, which is the convoy the
  design guards against.
- **Convoy.** A `LOCK TABLE` waited 6,975 ms behind a `ROW EXCLUSIVE` holder, and
  an unrelated single-row `UPDATE` then waited 5,963 ms behind that waiter.
- **P2.** The separate lookup measured +3.04% median at 0 marker rows and
  +2.25% at 10,000 (facts=1), failing the 1% bar. The folded gate measured +0.46%
  and -0.09%. The harness measured its own copy of the upsert, and facts=400 had
  four runs, so P2' re-measures on the real code with at least six.
- **P9.** An intent whose acceptance row or generation was deleted before
  selection stayed unprocessed and uncompleted for five cycles. The shared
  worker does not read generation status, so superseding the generation does not
  fence it: after phase 1 a cycle processed 1 and wrote edges. 10,100 orphan
  intents plus one healthy intent made one cycle take 2.123 s and process 0. An
  earlier identical run processed the healthy intent in 10 ms, which is
  unexplained. P9H: the design's lease horizon (taken at t=300 ms) read as
  passed (`now() > horizon`) at t=1.5 s while the writer held a renewed lease
  until t=4.0 s, so the horizon is unsound and the barrier replaced it.

## Concurrency Proof

Real-Postgres tests run in the `reducer-contention` gate. Graph tests run under
`race-graph-writes`.

### Deliverable 1

| Case | Test |
| --- | --- |
| A commit holds the shared lock and retirement waits, then supersedes its generation | `TestRetirementWaitsForInflightCommitThenFencesIt` |
| A commit after the marker is refused, with no facts or work written | `TestCommitRefusedWhileRepositoryRetiring` |
| Retire vs refinalize, both orders, no 40P01 | `TestRetirementAndRefinalizeDoNotDeadlock` |
| `Fail` takes the scope row before any work-table lock | `TestProjectorFailScopeLockPrecedesWorkTableLock` |
| Retire vs `Fail`, both orders, no 40P01 | `TestRetirementAndProjectorFailDoNotDeadlock` |
| A live reducer lease aborts phase 1, rolled back, no marker | `TestRetirementAbortsOnLiveReducerLease` |
| 7d marks and never deletes; the ids persist and an enqueue of a marked id is a no-op | `TestRetirementPhase1MarksReducerRowsNeverDeletes` |
| Every phase 1 wait fits the single 5 min 30 s deadline, and each wait is `min(bound, remaining)` | `TestRetirementPhase1DeadlineBoundsEveryWait` |
| A reducer that needs the repo key finishes while a step 4 re-drain runs | `TestRetirementStep4RedrainDoesNotHoldRepoKey` |
| The step 6 retry loop under a `ROW EXCLUSIVE` holder (P1'') | `TestRetirementFenceRetriesUnderTableLockConvoy` |
| The 7d statement folds the prior failure | `TestSupersedeStatementsFoldPriorFailure` (exists; must keep passing) |
| Re-issue, key reuse, in-progress key; `DO NOTHING` returns no row and the existing row is read | `TestRetireIdempotencyMatrix` |
| The folded gate's `ON CONFLICT` arm matches `ingestion_queries.go` | `TestFoldedScopeUpsertArmMatchesIngestionQuery` |
| Concurrent re-admitting commits stamp once | `TestReadmissionStampsOnce` |
| Webhook, backfill, replay, and reindex during retiring | `TestWebhookTriggerFailsRepositoryRetiring`, `TestBackfillSkipsRetiringRepo`, `TestDeadLetterReplayCannotResurrectRetiringWork`, `TestReindexRefusesRetiringRepository` |
| Dry run counts the four grant tables and sets `will_drop_grants` | `TestRetirementDryRunReportsGrantCounts` |

### Deliverable 2

| Case | Test |
| --- | --- |
| The hazard on main: pointer NULL, a pending reducer row is claimed | `TestRetiredScopeReducerRowIsClaimableWithoutPointer` |
| After 2q a projector `Enqueue` leaves nothing claimable | `TestRetirementQuiesceStopsProjectorEnqueue` |
| After 2q the maintenance and admin reopens reopen 0 | `TestRetirementQuiesceStopsMaintenanceReopen` |
| A live-leased reducer row survives the fenced pass and the row is `blocked/reducer_lease_live` | `TestRetirementQuiesceWaitsForLiveReducerLease` |
| A live projector lease is refused by Ack; 2a stays blocked until expiry | `TestRetirementBlocksOnProjectorLeaseUntilExpiry` |
| Runner vs generation retention on one scope | `TestRetirementRunnerAndRetentionSkipEachOther` |
| Two runners, one row | `TestRetirementRunnerLeaseIsExclusive` |
| A crash between batches or mid-graph resumes with exact Postgres counts | `TestRetirementResumesAfterCrashMidPurge`, `TestRetirementGraphCursorResume` |
| A pre-barrier shared lease holds 2b' until release; a cycle claimed after the barrier does not (P9a) | `TestRetirementBarrierWaitsForPreBarrierLease` |
| 2b removes every intent before any acceptance row goes, so no orphan intent exists | `TestRetirementDeletesIntentsBeforeAcceptance` |
| The 2b' lease-domain list equals the writers of `shared_projection_partition_leases` found in source | `TestRetirementLeaseDomainsMatchLeaseWriters` |
| Every scope- and repo-keyed table has a disposition (seeded violation fails, clean tree passes) | `TestRetirementTableCensusClassifiesEveryScopeKeyedTable` |
| 2z fails closed: residue after a re-run of 2c gives `failed/graph_residue` and the census in `failure_details` | `TestRetirementResidueCensusFailsClosed` |
| Phase 3 pre-counts the grant tables, drops them, and re-admission does not restore them | `TestRetirementGrantsCountedAndDropped` |

Retry and dead-letter matrix:
- transient errors retry with backoff, and the state reports `blocked` with a reason;
- non-transient errors go to `failed` after 5 attempts and are resumable by re-issue;
- the tombstone row is the durable failure record, so no work item is dead-lettered;
- collector refusals return nil, so no `commit_retryable` quarantine fires (`collector/service.go:330-347`).

## Test Plan

**Deliverable 1**
1. **Store:** RED `TestCommitRefusedWhileRepositoryRetiring` fails on main
   because the commit succeeds. Then the marker DDL schema test and the gate.
2. **`Fail`:** RED `TestRetirementAndProjectorFailDoNotDeadlock` shows the
   deadlock on main. Then the fix.
3. **Phase 1:** RED, the retire call is a 404 or not configured. Then
   fixture-level assertions for each 7a to 7g effect.
4. **API/MCP truth:**
   - RED handler tests: `index_state=retiring` on the list and the index-status
     block;
   - an MCP `tools/call` over `POST /mcp/message` for `list_indexed_repositories`
     and `get_index_status`;
   - golden corpus (B-7) and snapshot (B-12) updated together.
5. **Live Compose E2E (D1 variant):** index the fixture filesystem copy and the
   remote copy (the #7766 duplicate). Retire the stale copy by scope id and
   check that API, MCP, and Console report `retiring` and the other copy stays
   `indexed`.

**Deliverable 2**
1. **Quiesce:** the RED tests in the
   [writers file](7766-repository-retirement-reducer-writers.md#red-tests).
2. **Graph:** RED live test `TestLiveRepositoryRetirementLeavesOnlyForeignEdges`.
   It seeds the 11 incoming families from `TestLiveRepositoryRetryKeepsReducerEdges`,
   retires, and asserts the P10 census. Then the sweep reap after TTL, with the
   clock injected.
3. **Purge:** RED zero remaining rows across `rows_deleted` tables for the
   scope, plus the counts-equal-deletes invariant.
4. **Live Compose E2E (D2 variant):** extend the D1 run. Watch the readback reach
   `complete`, check that API, MCP, and Console report `retired`, re-add the copy
   to selection, and observe `readmitted_after_retirement`.

## PR Breakdown

Gates are from `specs/ci-gates.v1.yaml`.

### Deliverable 1: fence, tombstone, read surfaces

| PR | Content | Gates |
| --- | --- | --- |
| 1 | These design files and the evidence note (`docs/internal/evidence/7766-retirement-prove-first.md`) | markdown-file-cap, docs-*, doc-citations, measurement-citations |
| 2 | Refactor: move the retention prune steps and SQL to leaf `storage/postgres/retention/prune` with no behavior change. `generation_retention.go` sits at the 500-line cap, and `storage/postgres` is pinned at 335 files (`scripts/lib/dirgate-grandfather.tsv`), so the move is net zero. Deliverable 2 consumes it; Deliverable 1 does not depend on it. | go-file-cap, go-dir-gate, reducer-contention, package-docs |
| 3 | Marker migration (`repository_retirements`, number 169 at this check) + leaf `storage/postgres/retirement` (marker store, folded commit gate, commit-path benchmark for P2'); gates in `ingestion.go` (490 lines: share the finalized-skip rollback helper), backfill, recovery skip reasons, reindex refusal; refusal counter. No `claimed_at` change. | migration-immutability, reducer-contention, telemetry-coverage, go-file-cap |
| 4 | Webhook handoff gate (`collector/repo/git`, edits only; grandfathered at 65) | go-dir-gate, telemetry-coverage |
| 4a | `Fail` lock-order fix: take the scope row first under a `lock_timeout`, defer-retry on `55P03`, correct the comment at `projector_queue.go:214-220`. Projector code, not retirement code. Must land before PR 5. | reducer-contention, go-file-cap |
| 5 | `repositoryretirement` service; phase 1 with the precheck, the deadline, 7d as a mark, and the new scope-bound drain/recheck/fence helper in `rebuild/reset`; admin leaf routes (create, get, list, dry run with grant counts); audit EventType; OpenAPI fragment | openapi-surface, route-coverage, reducer-contention, telemetry-coverage |
| 8 | Read surfaces + MCP descriptions + golden corpus | openapi-surface, mcp-schema-drift, golden-corpus-gate, read-api-latency-gate, query-plan-regression |

PR 5 alone is safe: it leaves the scope in `pending`, which is fenced and
readable as `retiring`, with no purge yet. PR 4a must merge before PR 5. PR 8
can follow PR 3.

### Deliverable 2: the runner

| PR | Content | Gates |
| --- | --- | --- |
| 6 | Graph library: retract plan, demotion, coverage test, live Neo4j test | race-graph-writes, query-plan-regression |
| 6a | `claimed_at` migration and the `claimPartitionLeaseSQL` epoch arm with the P9b benchmark; the lease-domain test | migration-immutability, reducer-contention |
| 7 | Reducer runner (2a, 2q, 2b, 2b', 2c, 2d, 2e, 2z, phase 3) in `reducer/retirement` + `cmd/reducer` wiring; the table census test; grants pre-count; 2q and 2z telemetry | reducer-contention, race-graph-writes, telemetry-coverage, env-registry-doc |

PR 7 must not merge before PR 6 and PR 6a.

### Follow-ups, not part of either deliverable's gate

| PR | Content | Gates |
| --- | --- | --- |
| 9 | CLI + public docs (operate runbook, http-api reference, the grants wording) | docs-cli-env-refs, docs-build-changed |
| 10 | Candidates read | openapi-surface, route-coverage, query-plan-regression |
| 11 | Console badge | frontend-console-checks, console-e2e, console-a11y |
| 12 | Live Compose E2E evidence (D1 variant after PR 8; D2 variant after PR 7) | remote-validation-artifacts |
