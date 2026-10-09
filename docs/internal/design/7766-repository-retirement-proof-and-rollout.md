# Repository Retirement: Proof And Rollout (#7766)

Issue: #7766
Companions: [Repository Retirement](7766-repository-retirement.md) holds the
design: the marker, fence sequence, graph retraction, read surfaces, operator
surface, and telemetry.
[Concurrency Contract](7766-repository-retirement-concurrency.md) holds the
phase 1 lock budget, the intent-delete barrier, the invariants, and the commit
gate. This file holds the proof the design owes before and during the build,
the prove-first results, the corrections, and the issues kept out of scope.

Binding inputs: the
[shared-contract arbiter ruling](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6073882598)
and the arbiter ruling on the #7766 prove-first results (to be posted on
#7766).

Source check: origin/main 195337b97, 2026-10-08; amendments re-checked against
16c8c2a36, 2026-10-09.

## Prove-First Table

Data shape: PostgreSQL 18, 12,000 scopes and 739,838 generations, per-scope
p50 27, p99 79, max 3,280, plus one 5,000 tail. This is the QA shape measured
in `storage/postgres/membership/README.md:160-161`; ADR 2248 does not carry
these numbers. Facts are seeded per the #2249 large fixture
(`TestGenerationRetentionStoreLargeFixtureIntegration`), scaled to 400 facts
per generation. Graph: Neo4j pinned image, fixture corpus plus the largest
fixture repository.

Status after the first run: P1 and P9 failed as originally designed, and P2
passed only in its folded form. The rows P1', P1'', P1''', P2', and P9a-P9d are
the bars the amended design owes. They must pass before code lands. Results are
in [Prove-First Results](#prove-first-results).

| # | Theory | Shim | Pass | Fail → |
| --- | --- | --- | --- | --- |
| P1 | The phase-1 critical section (LOCK TABLE→COMMIT) is short. **Failed as designed** | `EXPLAIN (ANALYZE, BUFFERS)` of steps 7a-7g (Fence Sequence, phase 1) on scopes with 79 / 3,280 / 5,000 generations; wall time from lock to commit, 20 runs | p99 ≤ 250 ms at 5,000. 7a updates every non-superseded generation of the scopes: ≤ 3 per scope when the projector is current, every generation of a stalled scope | Move 7e to phase 2. 7d cannot move (reducer claim, step 7b) |
| P1' | Tuned bindings hold the bar in commit mode | Tuned form (7c by non-superseded pairs, 7d and recheck by `scope_id = ANY`), commit mode, n=20, cold: Rw5000, Ww5000 at the precheck limits (5,000 generations, about 20,000 7d rows), and a 25-repo realistic group | p99 ≤ 250 ms for each | Lower the precheck limits |
| P1'' | The 1 s wait plus retry bounds the convoy | Retry loop against a 5 s `ROW EXCLUSIVE` holder; time an unrelated single-row `UPDATE` and phase 1 | Unrelated `UPDATE` p99 ≤ 1.3 s, and phase 1 succeeds within the 30 s budget | Shorten the wait or the budget |
| P1''' | The precheck counts are cheap | `EXPLAIN ANALYZE` of both precheck counts at 3x QA scale | ≤ 50 ms | Count from an index-only form |
| P2 | The commit gate is free. **Lookup passed; separate statement failed the 1% bar** | `EXPLAIN ANALYZE` of the open-row lookup (index scan on `_open_repo_idx`); `BenchmarkCommitScopeGeneration` before/after | ≤ 0.1 ms; ≤ 1% ns/op | Fold the gate into the scope upsert (done, see P2') |
| P2' | The folded gate is free on the real code | Benchmark the real PR 3 commit path with the gate folded in, 10,000 marker rows | Median delta ≤ 1% at facts=1 and facts=400, at least 6 runs each; the `ON CONFLICT` arm byte-identical to `ingestion_queries.go` (derived-prefix test) | Revisit the gate shape |
| P3 | Purge batches are bounded | EXPLAIN every purge step (Fence Sequence, step 2d) for a 100-generation batch of the 3,280 scope; run the full purge while projector load runs | Each batch ≤ 5 s and ≤ 100,000 rows; total ≤ 33 batches; active projection slowdown ≤ 10% (ADR 2248 bound) | Lower the batch limit |
| P4 | Repo-keyed deletes use indexes | EXPLAIN of `content_*`/`repository_refs` deletes by `repo_id` | Index scans on `content_files_repo_path_idx` / `content_entities_repo_idx` | Add index (new proof) |
| P5 | Graph retract is repo-bounded | `PROFILE` of each G5 statement with the sentinel generation and of G6 | db hits proportional to repo nodes, not store size (≤ 2× at 10× store); G6 = `NodeUniqueIndexSeek`, 1 row | Bounded drain per label |
| P6 | Index-status grouping is cheap | `PROFILE` of the grouped count against the current count at 12,000 Repository nodes | ≤ 50 ms and ≤ 2× baseline | Count indexed only |
| P7 | The candidates read is cheap | `EXPLAIN ANALYZE` of the Candidates query at 12k scopes × 2 selectors | ≤ 200 ms | Per-selector partition |
| P8 | Refinalize skip arm | `EXPLAIN ANALYZE` of `AffectedGenerationsTemplate` with the `EXISTS` arm, all-scopes | ≤ +5% | Hashed anti-join |
| P9 | A shared worker drops intents whose acceptance or generation is gone. **Failed as designed** | Scratch test: delete acceptance rows mid-batch | Intent filtered as stale, not retried forever | Intent-delete barrier (done, see P9a) |
| P9a | The 2b' predicate holds a pre-barrier writer | RED/GREEN on real Postgres, P9H shape: a 4 s write, barrier at t=300 ms; 20 runs | The predicate stays > 0 until release (about 4 s) and reads 0 within one poll after; a cycle claimed after the barrier does not block it; no false pass | Revisit the barrier |
| P9b | The `claimed_at` arm is free on the heartbeat path | `ClaimPartitionLease` benchmark before and after the arm | Median ns/op delta ≤ 1% | Move the epoch out of the claim statement |
| P9c | The 2b deletes use their indexes | `EXPLAIN` of both 2b deletes at 3x QA scale | Index scans on the named indexes; ≤ 1 s per 10,000-row chunk; 0 rows remain | Add index (new proof) |
| P9d | 2b' latency is known | p99 cycle time per shared domain on the QA shape | State it. If > 5 min, the readback already names the partition | Runbook step for the operator |
| P10 | Post-retirement residue | Census: `MATCH (n) WHERE n.repo_id=$id RETURN labels(n), count(*)` and `MATCH (r:Repository {id:$id})-[x]-() RETURN type(x), x.evidence_source, count(*)` | 0 owned nodes; only foreign-owned incoming edges | Extend plan table |

## Prove-First Results

P1, P2, and P9 ran on 2026-10-08. **P1 and P9 failed as originally designed,
and the design was amended per the arbiter ruling on the #7766 prove-first
results (to be posted on #7766).** P2 passed only after the gate was folded
into the scope upsert. P3-P8 and P10 run in the PR that builds each piece, and
the bars in the table above that the amendments added must pass before code.

Provenance: a scratch harness drove real Postgres through
`postgres.ApplyBootstrap`. The raw outputs and the harness are local scratch
from the proving session and are not committed. Output files are named below
relative to that session's `proof-7766/out/` directory, as provenance only.
A committed evidence note (see PR 1) is still to be written.

### Setup

- PostgreSQL 18.3 (aarch64-musl), image
  `postgres:18-alpine@sha256:4da1a4828be12604092fa55311276f08f9224a74a62dcb4708bd7439e2a03911`,
  `shared_buffers` 2GB, fsync on. Toolchain go1.26.9. Host: Apple M5, 10 cores,
  32 GB, under other load.
- Background data: 12,000 scopes and 729,462 generations with lognormal
  per-scope counts (p50 27, p90 141, p99 522, max 3,280). That tail is heavier
  than the QA shape's p99 of 79. Status mix: 12,000 active, 941 pending, 325
  failed, the rest superseded. The final database held 2.2M generations and
  8.0M work items, about 3x QA scale.
- Target shapes: **R** (realistic: one pending, one active, one failed
  generation, the rest superseded), **W** (stalled: every non-active
  generation pending or failed), **M** (a 25-repo request: one 5,000, one
  3,280, and 23 of 79 generations, realistic statuses). Each scope has four
  reducer rows per generation and one expired claimed reducer row.
- Cold means `shared_buffers` evicted with `pg_buffercache_evict_relation`
  (OS cache still warm). Warm means prewarmed by a read pass.
- The harness uses nearest-rank percentiles, so at n=10 and n=20 every "p99" is
  the maximum. Design-form figures are n=20 in commit mode (commit added 2.7 to
  19 ms), except the 25-repo group at n=10. Tuned figures are n=10 in rollback
  mode. The two are not the same
  total and must not be compared as a speedup. No tuned commit-mode figure
  exists yet (P1').

### P1: failed as designed

Design form, commit mode, lock request to commit, milliseconds
(`p1_summary.txt`):

| Shape | Cache | n | p50 | p99 (= max) | Notes |
| --- | --- | --- | --- | --- | --- |
| R79 | cold | 20 | 7.1 | 22.3 | |
| R3280 | cold | 20 | 69.6 | 96.2 | |
| R5000 | cold | 20 | 173.3 | **252.2** | 7c 111.6 ms mean; over the 250 ms bar |
| R5000 | warm | 20 | 102.3 | 114.9 | |
| M, 25 repos (10,097 generations) | cold | 10 | 171.8 | 208.2 | |
| W3280 | cold | 20 | 204.2 | 234.7 | 7a 3,280 rows, 7d 13,116 rows |
| W5000 | cold | 20 | 346.9 | **885.9** | 7a 5,000 rows (135.9 ms mean), 7d 19,996 rows (100.7 ms), commit 19.0 ms |
| W5000 | warm | 20 | 301.4 | **411.9** | 7a 5,000 rows, 7d 19,996 rows |

R5000 cold missed the bar by 2.2 ms, and the stalled shape missed it by a wide
margin. The pass text "7a touches ≤ 3 rows" was also false: it counted
statuses on a current projector, not an invariant, and the stalled shape
updated 5,000 rows.

Tuned form (7c bound to the non-superseded pairs, 7d and the recheck bound by
`scope_id = ANY`), n=10, rollback mode, lock request to commit, milliseconds
(`p1_time_*_tuned_*.csv`; p50 is nearest-rank):

| Shape | Cache | p50 | max | Design form, same mode (max) |
| --- | --- | --- | --- | --- |
| Rw5000 | cold | 27.9 | 88.5 | 194.2 |
| Rw5000 | warm | 12.0 | 19.3 | 88.7 |
| Rw3280 | cold | 15.5 | 28.7 | |
| Ww5000 | cold | 114.4 | 184.9 | |
| Ww5000 | warm | 87.3 | 140.1 | |

Tuned 7c costs 6.5 ms cold on Rw5000 and 32.3 ms on Ww5000. Caveat: the tuned
Ww5000 files record 107 to 171 rows for 7a and 7c per run, not 5,000, while 7d
records 19,996 rows. So the 184.9 ms figure does not prove the 5,000-row
7a/7c leg. The cause was not investigated. P1' re-measures at the precheck
limits in commit mode.

Fleet pause, claim-shaped statements running against unrelated scopes (3
claim-shaped `UPDATE`, 2 heartbeat-shaped `UPDATE`, 1 enqueue-shaped `INSERT`;
design-form statements; n=10 per family; `p1_block_summary.txt`):

| Shape | Section p50 | Section max | Blocked statement max | Baseline statement p50 |
| --- | --- | --- | --- | --- |
| R5000 | 47.9 | 99.4 | 100.5 | 1.1 |
| W5000 | 115.5 | 229.1 | 230.5 | 1.1 |
| R79 (9 of 10 runs) | 4.0 | 4.8 | 5.4 | 0.8 |

The tenth R79 run stalled for 451,635.6 ms behind a leftover real claim
statement of about 7.5 minutes, which is the convoy this design guards against.

Convoy: a `LOCK TABLE` waited 6,975 ms behind a `ROW EXCLUSIVE` holder
(`p1_convoy_B_lock.txt`), and an unrelated single-row `UPDATE` then waited
5,963 ms behind that waiter (`p1_convoy_C_unrelated_update.txt`).

Design consequences: tuned bindings for 7c and 7d, a 1 s lock wait with retry,
the precheck budget, and 7d staying in phase 1. See the
[Concurrency Contract](7766-repository-retirement-concurrency.md#phase-1-lock-budget).

### P2: separate lookup failed the 1% bar; the folded gate passed

- **Lookup cost** (`p2_explain.txt`, `p2_pgbench.txt`): at 0 marker rows the
  planner uses a sequential scan, 0.006 to 0.016 ms. At 10,000 rows it uses an
  index scan on `repository_retirements_open_repo_idx`, 0.013 to 0.046 ms in
  custom plans and 0.014 to 0.021 ms in generic plans. `pgbench` over a unix
  socket measured 0.004 to 0.009 ms average per lookup (protocol floor 0.004).
  The ≤ 0.1 ms bar passes.
- **End-to-end commit** (`p2_bench_summary.txt`, `BenchmarkCommitScopeGenerationLive`,
  median delta against the gate off):

| Marker rows | Facts | Runs | Off (ms/commit) | Separate lookup | Folded into upsert (sd) |
| --- | --- | --- | --- | --- | --- |
| 0 | 1 | 6 | 5.64 | +3.04% (+183 µs) | +0.46% (1.25) |
| 0 | 400 | 4 | 41.06 | +1.05% | +0.82% (0.92) |
| 10,000 | 1 | 6 | 7.81 | +2.25% (+195 µs) | -0.09% (1.10) |
| 10,000 | 400 | 4 | 38.20 | +0.18% | -0.39% (1.12) |

The separate lookup fails the ≤ 1% bar at facts=1. The folded form is inside
the noise everywhere. The harness measured its own copy of the upsert, not the
real PR 3 code, and facts=400 had four runs, so P2' re-measures with at least
six.

### P9: failed as designed

Real Postgres, shared worker (`p9_run1.txt`):

| Case | Result |
| --- | --- |
| Control: acceptance present, generation active | processed 1, edges written, intent completed |
| Control: acceptance points at a newer generation | filtered as stale, no write, completed |
| Acceptance deleted before selection | processed 0 in each of 5 cycles, intent never completed |
| Generation deleted before selection (cascade left 0 acceptance rows) | same |
| Acceptance, generation, or intent deleted mid-batch | worker still wrote edges |
| After phase 1 (generation superseded, acceptance and intent kept) | processed 1, edges written |

The pass text "intent filtered as stale, not retried forever" fails: an intent
without an acceptance row is neither filtered nor completed, and the worker
skips it on every cycle. The last row shows the shared worker does not read
generation status, so superseding the generation does not fence it.

Starvation (`p9_run3_starvation.txt`): 10,100 orphan intents (20 distinct
acceptance keys, no acceptance rows) plus one healthy intent. The cycle took
2.123 s (selection 2.112 s), processed 0, wrote 0, and left the healthy intent
uncompleted. An earlier run (`p9_run2.txt`) processed the healthy intent in 10
ms. The difference is unexplained; the RED test for the follow-up issue must
reproduce deterministically.

Horizon repro (`p9_run2.txt`, P9H): a 4.025 s write cycle, the design's horizon
taken at t=300 ms.

| t | `now() > horizon` | Lease expiry past horizon | Worker |
| --- | --- | --- | --- |
| 0.801 s | false | +0.50 s | inside `RetractEdges` |
| 1.501 s | true | +1.00 s | inside `RetractEdges` |
| 2.501 s | true | +2.00 s | inside `RetractEdges` |
| 3.501 s | true | +3.00 s | inside `RetractEdges` |

The horizon wait would have passed at 1.5 s with the writer holding a renewed
lease until 4.0 s. The design's 2a wait is unsound, so the
[intent-delete barrier](7766-repository-retirement-concurrency.md#shared-projection-barrier)
replaces it, and 2b runs first.

## Concurrency Proof

Real-Postgres tests run in the `reducer-contention` gate. Graph tests run
under `race-graph-writes`.

| Case | Test |
| --- | --- |
| A commit holds the shared lock and retirement waits, then supersedes its generation | `TestRetirementWaitsForInflightCommitThenFencesIt` |
| A commit after the marker is refused, with no facts or work written | `TestCommitRefusedWhileRepositoryRetiring` |
| Retire vs refinalize, both orders, no 40P01 | `TestRetirementAndRefinalizeDoNotDeadlock` |
| A live reducer lease aborts phase 1, rolled back, no marker | `TestRetirementAbortsOnLiveReducerLease` |
| A live projector lease is refused by Ack; phase 2 stays blocked until expiry | `TestRetirementBlocksOnProjectorLeaseUntilExpiry` |
| Runner vs generation retention on one scope | `TestRetirementRunnerAndRetentionSkipEachOther` |
| Two runners, one row | `TestRetirementRunnerLeaseIsExclusive` |
| A crash between batches or mid-graph resumes with exact Postgres counts | `TestRetirementResumesAfterCrashMidPurge`, `TestRetirementGraphCursorResume` |
| Re-issue, key reuse, in-progress key | `TestRetireIdempotencyMatrix` |
| Concurrent re-admitting commits stamp once | `TestReadmissionStampsOnce` |
| A pre-barrier shared lease holds 2b' until release; a cycle claimed after the barrier does not (P9a) | `TestRetirementBarrierWaitsForPreBarrierLease` |
| 2b removes every intent before any acceptance row goes, so no orphan intent exists | `TestRetirementDeletesIntentsBeforeAcceptance` |
| The step 6 retry loop under a `ROW EXCLUSIVE` holder (P1'') | `TestRetirementFenceRetriesUnderTableLockConvoy` |
| The folded gate's `ON CONFLICT` arm matches `ingestion_queries.go` | `TestFoldedScopeUpsertArmMatchesIngestionQuery` |
| Webhook, backfill, replay, and reindex during retiring | `TestWebhookTriggerFailsRepositoryRetiring`, `TestBackfillSkipsRetiringRepo`, `TestDeadLetterReplayCannotResurrectRetiringWork`, `TestReindexRefusesRetiringRepository` |

Retry and dead-letter matrix:
- transient errors retry with backoff, and the state reports `blocked` with a reason;
- non-transient errors go to `failed` after 5 attempts and are resumable by re-issue;
- the tombstone row is the durable failure record, so no work item is dead-lettered;
- collector refusals return nil, so no `commit_retryable` quarantine fires (`collector/service.go:330-347`).

## Test Plan

1. **Store:** RED `TestCommitRefusedWhileRepositoryRetiring` fails on main
   because the commit succeeds. Then the marker DDL schema test and the gate.
2. **Phase 1:** RED, the retire call is a 404 or not configured. Then
   fixture-level assertions for each 7a-7g effect.
3. **Graph:** RED live test `TestLiveRepositoryRetirementLeavesOnlyForeignEdges`.
   It seeds the 11 incoming families from `TestLiveRepositoryRetryKeepsReducerEdges`,
   retires, and asserts the P10 census. Then the sweep reap after TTL, with the
   clock injected.
4. **Purge:** RED zero remaining rows across `rows_deleted` tables for the
   scope, plus the counts-equal-deletes invariant.
5. **API/MCP truth:**
   - RED handler tests: `index_state=retired` on the list, verdict `retired`,
     and the index-status block;
   - an MCP `tools/call` over `POST /mcp/message` for `list_indexed_repositories`
     and `get_index_status`;
   - golden corpus (B-7) and snapshot (B-12) updated together.
6. **Live Compose E2E:** index the fixture filesystem copy and the remote copy
   (the #7766 duplicate). Then:
   - retire the stale copy by scope id;
   - watch the readback reach `complete`;
   - check that API, MCP, and Console report `retired` and the other copy stays `indexed`;
   - re-add the copy to selection and observe `readmitted_after_retirement`.

## PR Breakdown

| PR | Content | Gates (`specs/ci-gates.v1.yaml`) |
| --- | --- | --- |
| 1 | These three design files, with the Prove-first results recorded in the proof file; the evidence note (`docs/internal/evidence/7766-retirement-prove-first.md`) is still to be written from the scratch outputs | markdown-file-cap, docs-*, doc-citations, measurement-citations |
| 2 | Refactor: move the retention prune steps and SQL to leaf `storage/postgres/retention/prune` with no behavior change. `generation_retention.go` sits at the 500-line cap, and `storage/postgres` is pinned at 334 files, so the move is net zero. | go-file-cap, go-dir-gate, reducer-contention, package-docs |
| 3 | Migration (marker table, `claimed_at`) + leaf `storage/postgres/retirement` (marker store, folded commit gate); `claimPartitionLeaseSQL` epoch arm with the P9b benchmark; gates in `ingestion.go` (489 lines: share the finalized-skip rollback helper), backfill, recovery skip reasons, reindex refusal; refusal counter | migration-immutability, reducer-contention, telemetry-coverage, go-file-cap |
| 4 | Webhook handoff gate (`collector/repo/git`, edits only; grandfathered at 65) | go-dir-gate, telemetry-coverage |
| 5 | `repositoryretirement` service; phase 1 with the precheck and the bounded-wait fence helper in `rebuild/reset`; admin leaf routes (create, get, list, dry run); audit EventType; OpenAPI fragment | openapi-surface, route-coverage, reducer-contention, telemetry-coverage |
| 6 | Graph library: retract plan, demotion, coverage test, live Neo4j test | race-graph-writes, query-plan-regression |
| 7 | Reducer runner (phases 2-3, including 2b and 2b') in `reducer/retirement` + `cmd/reducer` wiring | reducer-contention, race-graph-writes, telemetry-coverage, env-registry-doc |
| 8 | Read surfaces + MCP descriptions + golden corpus | openapi-surface, mcp-schema-drift, golden-corpus-gate, read-api-latency-gate, query-plan-regression |
| 9 | CLI + public docs (operate runbook, http-api reference) | docs-cli-env-refs, docs-build-changed |
| 10 | Candidates read | openapi-surface, route-coverage, query-plan-regression |
| 11 | Console badge | frontend-console-checks, console-e2e, console-a11y |
| 12 | Live Compose E2E evidence | remote-validation-artifacts |

PR 7 must not merge before PR 6. PR 5 alone is safe: it leaves the scope in
`pending`, which is fenced and readable as `retiring`, with no purge yet.

## Corrections To The Arbiter Ruling

The review of this design against origin/main 195337b97 found seven premises in
the first arbiter ruling that were false or incomplete. The design follows the
adjustment, not the ruling's original wording. The second list, after them,
holds what the prove-first results review found false or imprecise.

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
   fence** (`recovery.go:119`, `projector_queue_sql.go:264-273`). The
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

Found false or imprecise in the results review (arbiter ruling on the #7766
prove-first results, to be posted on #7766):

1. **The P1 pass text "7a touches ≤ 3 rows"** counted statuses; it is not an
   invariant. A stalled scope updated 5,000 rows. The pass text now reads
   "every non-superseded generation of the scopes".
2. **Design 2a, "`now() > shared_lease_horizon` ... has finished or lost its
   lease"** is false: a heartbeat keeps renewing the lease past the horizon
   (P9H). The barrier replaces it.
3. **The P1 fail-arrow "Move 7d/7e to phase 2"** is wrong for 7d. The reducer
   claim's supersede CTE requires `scope.active_generation_id =
   active_generation.generation_id` (`storage/postgres/reducer_generation_filter_sql.go`),
   and 7b nulls that pointer. 7e can move; it costs 0.5 ms and stays.
4. **The NOT_CHECKED claim "every shared-domain writer holds a partition lease
   while writing"** is confirmed for the shared worker and code_calls and still
   open for repo_dependency.
5. **A working note put the convoy on the advisory wait.** It comes from step
   6's `LOCK TABLE` inheriting step 3's `SET LOCAL`. The advisory wait convoys
   nothing.
6. **A working note called repo_dependency "serialized with phase 1" by the
   advisory lock** at `storage/postgres/repo_dependency_acceptance_gate.go:72`.
   Only the `fn` inside that transaction is (`:72-86`); where its graph write
   runs is unverified.
7. **Line references drifted in the working notes.** The repo_dependency skip is
   at `repo_dependency_projection_runner.go:348-350` and its cap error at
   `:366-369`, not `:340-343`.
8. **The working notes' tuned "p99" figures** are n=10 rollback-mode maxima and
   are not comparable to the n=20 commit-mode design-form totals.
9. **The comment at `reducer/intents/shared/worker/process.go:145-149`**, "cannot
   starve at the scan cap", is false within one partition (P9 starvation run).

Two items the first ruling listed as not verified are now closed:
- The content-catalog fallback for GET `/api/v0/repositories` reads
  `ingestion_scopes WHERE scope_kind='repository'`
  (`query/content_reader_repository_catalog.go:16-32`).
- Migration 166 has `CHECK (selector_kind IN ('github_org','explicit'))`. This
  matters to #7765, not to this design.

## Out Of Scope: Pre-Existing Issues

Two defects the proof exposed predate #7766. #7766 references them and does not
depend on them. Both are to be filed; the coordinator adds the numbers after the
owner approves.

- **Refinalize `LOCK TABLE` convoy (to be filed).**
  `AcquireReducerClaimFence` runs `LOCK TABLE fact_work_items IN EXCLUSIVE
  MODE` (`rebuild/reset/refinalize.go:65-66`, `:381`, driven from
  `storage/postgres/recovery.go:369`) with no `lock_timeout` anywhere in `recovery.go`
  or `refinalize.go`. The fix is the bounded-wait and retry fence helper in
  `rebuild/reset`. #7766 PR 5 builds it, and the refinalize issue adopts it.
- **Orphan-intent starvation and silent 0-row completion (to be filed).** All
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

## NOT_CHECKED

- **repo_dependency runner.** Whether it claims a partition lease (its config
  has a 5 minute `LeaseTTL` and a process-unique owner, but the claim call was
  not read) and whether its graph write sits inside the gate transaction. I1
  depends on the answer.
- **Projector lease.** The default TTL, and whether a projector mid-write is
  guaranteed stopped once `claim_until` passes (2a).
- **Generation id reuse.** Whether a deleted `generation_id` can be re-created
  on re-ingest. It affects the drain rule in the orphan-intent issue.
- **Tuned numbers.** Commit-mode tuned figures and any 25-repo tuned figure
  (P1'). The tuned Ww5000 rows record 107 to 171 rows for 7a and 7c, not 5,000,
  and the cause was not investigated.
- **Gate benchmark.** P2 measured a harness copy of the scope upsert with four
  runs at facts=400, not the real PR 3 code (P2').
- **Starvation discrepancy.** Why one earlier starvation run processed the
  healthy intent in 10 ms and the later run processed none.
- **Pre-barrier edges.** Whether the retract plan covers shared-domain edges
  written by pre-barrier cycles (the P10 census covers it once built).
- **Provenance.** The raw outputs and the harness are uncommitted scratch, so a
  reviewer cannot rerun them from this branch.
- Whether workload, semantic, or other reducer-materialized nodes carry
  `repo_id` and survive G1-G5 (P10 census).
- Whether the ingester's in-process repository catalog cache ever evicts a
  deleted repo id.
- The `repo_slug` form stored for GitLab and Bitbucket scopes, against the
  trigger form `provider/<full_name>` (`webhook_trigger_selector.go:298-311`).
  The webhook gate depends on it.
- Whether NornicDB honours the demotion `SET` and the sweep reap the way Neo4j
  does. Neo4j is the supported backend, so this is not a blocker.
- GitHub fork filtering in `githubOrg` (a #7765 item).
- Every performance number outside P1, P2, and P9: none has been measured yet.
