# Repository Retirement: Proof And Rollout (#7766)

Issue: #7766
Companion: [Repository Retirement](7766-repository-retirement.md) holds the
design: the marker, fence sequence, graph retraction, read surfaces, operator
surface, and telemetry. This file holds the proof the design owes before and
during the build.

Source check: origin/main 195337b97, 2026-10-08.

## Prove-First Table

Data shape: PostgreSQL 18, 12,000 scopes and 739,838 generations, per-scope
p50 27, p99 79, max 3,280, plus one 5,000 tail. This is the QA shape measured
in `storage/postgres/membership/README.md:160-161`; ADR 2248 does not carry
these numbers. Facts are seeded per the #2249 large fixture
(`TestGenerationRetentionStoreLargeFixtureIntegration`), scaled to 400 facts
per generation. Graph: Neo4j pinned image, fixture corpus plus the largest
fixture repository.

| # | Theory | Shim | Pass | Fail → |
| --- | --- | --- | --- | --- |
| P1 | The phase-1 critical section (LOCK TABLE→COMMIT) is short | `EXPLAIN (ANALYZE, BUFFERS)` of steps 7a-7g (Fence Sequence, phase 1) on scopes with 79 / 3,280 / 5,000 generations; wall time from lock to commit, 20 runs | p99 ≤ 250 ms at 5,000; 7a touches ≤ 3 rows | Move 7d/7e to phase 2 |
| P2 | The commit gate is free | `EXPLAIN ANALYZE` of the open-row lookup (index scan on `_open_repo_idx`); `BenchmarkCommitScopeGeneration` before/after | ≤ 0.1 ms; ≤ 1% ns/op | Cache-free redesign |
| P3 | Purge batches are bounded | EXPLAIN every purge step (Fence Sequence, step 2d) for a 100-generation batch of the 3,280 scope; run the full purge while projector load runs | Each batch ≤ 5 s and ≤ 100,000 rows; total ≤ 33 batches; active projection slowdown ≤ 10% (ADR 2248 bound) | Lower the batch limit |
| P4 | Repo-keyed deletes use indexes | EXPLAIN of `content_*`/`repository_refs` deletes by `repo_id` | Index scans on `content_files_repo_path_idx` / `content_entities_repo_idx` | Add index (new proof) |
| P5 | Graph retract is repo-bounded | `PROFILE` of each G5 statement with the sentinel generation and of G6 | db hits proportional to repo nodes, not store size (≤ 2× at 10× store); G6 = `NodeUniqueIndexSeek`, 1 row | Bounded drain per label |
| P6 | Index-status grouping is cheap | `PROFILE` of the grouped count against the current count at 12,000 Repository nodes | ≤ 50 ms and ≤ 2× baseline | Count indexed only |
| P7 | The candidates read is cheap | `EXPLAIN ANALYZE` of the Candidates query at 12k scopes × 2 selectors | ≤ 200 ms | Per-selector partition |
| P8 | Refinalize skip arm | `EXPLAIN ANALYZE` of `AffectedGenerationsTemplate` with the `EXISTS` arm, all-scopes | ≤ +5% | Hashed anti-join |
| P9 | A shared worker drops intents whose acceptance or generation is gone | Scratch test: delete acceptance rows mid-batch | Intent filtered as stale, not retried forever | Explicit marker check in worker |
| P10 | Post-retirement residue | Census: `MATCH (n) WHERE n.repo_id=$id RETURN labels(n), count(*)` and `MATCH (r:Repository {id:$id})-[x]-() RETURN type(x), x.evidence_source, count(*)` | 0 owned nodes; only foreign-owned incoming edges | Extend plan table |

## Prove-First Results

P1, P2, P9: pending; results are added before this PR is published. P3-P8, P10: run in the PR that builds each piece.

Every threshold in the table above is a target. None has been measured yet, so
no performance number in the design is evidence.

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
| 1 | These two design files plus the Prove-first results evidence note (`docs/internal/evidence/7766-retirement-prove-first.md`) | markdown-file-cap, docs-*, doc-citations, measurement-citations |
| 2 | Refactor: move the retention prune steps and SQL to leaf `storage/postgres/retention/prune` with no behavior change. `generation_retention.go` sits at the 500-line cap, and `storage/postgres` is pinned at 334 files, so the move is net zero. | go-file-cap, go-dir-gate, reducer-contention, package-docs |
| 3 | Migration + leaf `storage/postgres/retirement` (marker store, commit gate); gates in `ingestion.go` (489 lines: share the finalized-skip rollback helper), backfill, recovery skip reasons, reindex refusal; refusal counter | migration-immutability, reducer-contention, telemetry-coverage, go-file-cap |
| 4 | Webhook handoff gate (`collector/repo/git`, edits only; grandfathered at 65) | go-dir-gate, telemetry-coverage |
| 5 | `repositoryretirement` service; phase 1; admin leaf routes (create, get, list, dry run); audit EventType; OpenAPI fragment | openapi-surface, route-coverage, reducer-contention, telemetry-coverage |
| 6 | Graph library: retract plan, demotion, coverage test, live Neo4j test | race-graph-writes, query-plan-regression |
| 7 | Reducer runner (phases 2-3) in `reducer/retirement` + `cmd/reducer` wiring | reducer-contention, race-graph-writes, telemetry-coverage, env-registry-doc |
| 8 | Read surfaces + MCP descriptions + golden corpus | openapi-surface, mcp-schema-drift, golden-corpus-gate, read-api-latency-gate, query-plan-regression |
| 9 | CLI + public docs (operate runbook, http-api reference) | docs-cli-env-refs, docs-build-changed |
| 10 | Candidates read | openapi-surface, route-coverage, query-plan-regression |
| 11 | Console badge | frontend-console-checks, console-e2e, console-a11y |
| 12 | Live Compose E2E evidence | remote-validation-artifacts |

PR 7 must not merge before PR 6. PR 5 alone is safe: it leaves the scope in
`pending`, which is fenced and readable as `retiring`, with no purge yet.

## Corrections To The Arbiter Ruling

The review of this design against origin/main 195337b97 found seven premises in
the arbiter ruling that were false or incomplete. The design follows the
adjustment, not the ruling's original wording.

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

Two items the ruling listed as not verified are now closed:
- The content-catalog fallback for GET `/api/v0/repositories` reads
  `ingestion_scopes WHERE scope_kind='repository'`
  (`query/content_reader_repository_catalog.go:16-32`).
- Migration 166 has `CHECK (selector_kind IN ('github_org','explicit'))`. This
  matters to #7765, not to this design.

## NOT_CHECKED

- **Shared-worker behaviour.** Not confirmed: what a shared worker does when
  the acceptance rows behind an intent are deleted (P9). Also not confirmed:
  that every shared-domain writer holds a partition lease while writing, which
  the lease-horizon wait relies on.
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
- Every performance number in the design: none has been measured yet.
