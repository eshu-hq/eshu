# Repository Retirement: Fenced, Idempotent, Operator-Driven (#7766)

Issue: #7766
Companions, one file per concern:
- [Concurrency Contract](7766-repository-retirement-concurrency.md): phase 1
  lock budget and deadline, the lock-order rule and the `Fail` fix, the
  shared-projection barrier, invariants I1 to I4, the commit gate, and
  re-admission.
- [Reducer Reopen And Replay Writers](7766-repository-retirement-reducer-writers.md):
  every path that creates or reopens a reducer row after phase 1, and the
  reducer quiesce (2q) that closes them.
- [Runner](7766-repository-retirement-runner.md): Deliverable 2, phases 2 and 3
  and the graph retraction plan.
- [Table Census](7766-repository-retirement-table-census.md): the disposition
  of every scope- and repo-keyed table.
- [Surfaces](7766-repository-retirement-surfaces.md): read surfaces, OpenAPI,
  the operator surface, candidates, and telemetry.
- [Proof And Rollout](7766-repository-retirement-proof-and-rollout.md): the
  prove-first table by deliverable, results, tests, and the PR breakdown.
- [Corrections And Open Items](7766-repository-retirement-corrections.md):
  corrections to the rulings, issues kept out of scope, and NOT_CHECKED.
- Evidence note:
  [7766-retirement-prove-first.md](../evidence/7766-retirement-prove-first.md)
  holds the raw P1, P2, and P9 results and the re-runnable harness.

Binding inputs: the
[arbiter ruling for #7765 and #7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6073882598)
(Option C: signal only, plus one operator-driven retire primitive), the
[arbiter ruling on the prove-first results](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6082885964),
the arbiter ruling, round 3 (to be posted on #7766), ADR 2248
([retention semantics](2248-retention-semantics-generations-facts-content.md)),
design [7324](7324-cross-scope-writer-rearm.md),
`docs/public/reference/hosted-retention-deletion-policy.md`, and
`docs/public/operate/graph-rebuild-from-facts.md`. Round 3 changes the earlier
rulings only where this design says so.

Status: proposed design, reworked on 2026-10-09 per round 3. P1 and P9 failed
as originally designed (the second ruling amended the design). Round 3 found
that reducer work for a retiring scope is re-created after phase 1, a
lock-order cycle between phase 1 and `Fail`, and a silent grant cascade, and it
split the work into two deliverables. No code lands until the bars for that
deliverable in the
[prove-first table](7766-repository-retirement-proof-and-rollout.md#prove-first-table)
pass.

Source check: origin/main 3b03f018e, 2026-10-09. Every `file:line` citation in
the eight design files was re-verified against that SHA.

## Purpose

Eshu has no operator path to remove an indexed repository. A deselected or
duplicate repository (the #7766 report: a stale filesystem copy plus a remote
copy with the same name) keeps its scope, generations, facts, content, and
graph nodes. It keeps answering as indexed. This design adds one service-level
retire primitive. The admin route and the CLI call it, and any future opt-in
policy would call it unchanged. It is fenced against every ingest path, purges
in bounded batches, retracts the graph through existing paths, and leaves a
durable tombstone that read surfaces report as `retired`.

## Two Deliverables

| | Deliverable 1: fence, tombstone, read surfaces | Deliverable 2: the runner |
| --- | --- | --- |
| Contents | Marker table and folded commit gate; ingest refusals (collector, webhook, backfill, recover, reindex); phase 1 with 7d as a mark; the `Fail` lock-order fix; admin create, get, list, and dry run; read surfaces; OpenAPI and MCP | 2a, 2q, 2b, 2b', 2c, 2d, 2e, 2z, and phase 3; the graph retraction library; the lease epoch (`claimed_at`); the table census test |
| PRs | 1 to 5 and 8, plus 4a (the `Fail` fix) | 6, 6a, and 7 |
| Bars before code | P1', P1'', P1''', P2' | P1q, P9a to P9d, P10' |
| End state | Scope `pending`, fenced, reported `retiring` | `complete`, tombstone, reported `retired`, re-admission works |

**Deliverable 1 is safe alone.** After phase 1 commits, every generation of the
scope is `superseded`, the active pointer is NULL, and every claimable
non-succeeded projector and reducer row is terminal. Collector commits,
webhooks, deferred backfill, `recover-generations`, and reindex are refused.
Read surfaces report `retiring`. Nothing is deleted, so nothing is lost, and the
#7766 outcome lands: the stale copy stops answering as indexed.

What Deliverable 1 does not do, stated plainly:
- **Reducer work can still be replayed.** Until the runner ships, maintenance
  reopens can replay reducer work on a retiring scope (see
  [Reducer Reopen And Replay Writers](7766-repository-retirement-reducer-writers.md)).
  That work is wasted, not wrong: reads already say `retiring`, and the graph
  for the repository has not been retracted yet. 2q stops it.
- **There is no way back.** The row stays `pending`. Re-issue is idempotent and
  the open-repo index refuses a second marker. Nothing in Deliverable 1 cancels
  a retirement; re-admission exists only after the runner completes it. An
  operator who retires the wrong repository waits for Deliverable 2. The dry
  run exists to prevent that, and Deliverable 1 does not remove the risk.
- **Nothing is purged.** Storage and graph nodes stay until the runner runs.

## Decision Summary

- The marker is a new table, `repository_retirements`, keyed by the canonical
  repo id (`ingestion_scopes.partition_key`). One open row covers the
  default-branch scope and every `@ref` scope of the repository. The table has
  no FK to `ingestion_scopes`, so the tombstone outlives the scope row.
- Phase 1 runs in the API, in one transaction, inside one 5 min 30 s deadline.
  It takes the existing per-repository advisory lock in exclusive mode, uses a
  scope-bound drain, recheck, and fence helper, marks every non-superseded
  generation `superseded`, marks the claimable reducer rows `superseded` (a
  mark, never a delete), and writes the marker. Superseding the generations
  fences the projector. No claim SQL changes.
- Phases 2 and 3 run in a reducer runner (Deliverable 2). Phase 2 order is 2a,
  2q, 2b, 2b', 2c, 2d, 2e, 2z: wait out projector leases, quiesce the reducer
  queue, delete shared intents and wait out the pre-barrier shared-projection
  leases, retract the graph, purge Postgres in ADR 2248 batches, clear
  repo-keyed leftovers, and census the residue. Phase 3 deletes the scope rows
  and marks the tombstone `complete` in the same transaction.
- The Repository node is never deleted by the primitive. It is demoted to a
  non-projector stub shape, and the existing `Repository` orphan sweep reaps it
  once its other owners retract (see
  [Graph Retraction](7766-repository-retirement-runner.md#graph-retraction)).
- Re-admission is a fresh first generation. It sets `readmitted_at` inside the
  ingest transaction that re-admits, and it emits
  `outcome=readmitted_after_retirement`. Tenant and role grants are dropped by
  design and not restored (see
  [Operator Surface](7766-repository-retirement-surfaces.md#operator-surface)).

## Durable Marker And Writers

`ingestion_scopes.status` cannot carry the marker. The ON CONFLICT arm of the
upsert rewrites it on every commit (`storage/postgres/ingestion_queries.go:79-85`, value bound at `:249`).

The Deliverable 1 migration takes the next free number: 169 at this check
(`migrations/168_webhook_refresh_triggers_claim_fencing.sql` is the latest;
167 is `167_unroutable_intents_generation_idx.sql`). Re-check at PR time,
because open branches can claim the same number. The table carries every column
the runner needs, because a merged migration cannot be edited. The
`shared_projection_partition_leases.claimed_at` column is not here: it belongs
to Deliverable 2 (PR 6a), since only the barrier reads it.

```sql
CREATE TABLE IF NOT EXISTS repository_retirements (
    retirement_id TEXT PRIMARY KEY,            -- 'rr_' || sha256(repo_id||':'||requested_at)[:16]
    repo_id TEXT NOT NULL,                     -- ingestion_scopes.partition_key
    scope_id TEXT NOT NULL,                    -- default-branch scope
    repo_slug_key TEXT NOT NULL DEFAULT '',    -- lower(payload->>'repo_slug'), webhook gate only
    state TEXT NOT NULL CHECK (state IN ('pending','running','blocked','repairing_graph','complete','failed')),
    phase TEXT NOT NULL CHECK (phase IN ('fenced','quiesce','barrier','graph_retract','purge','census','finalize','done')),
    graph_step_cursor INTEGER NOT NULL DEFAULT 0,
    blocked_reason TEXT NOT NULL DEFAULT '' CHECK (blocked_reason IN
      ('','projector_lease_live','reducer_lease_live','shared_lease_live',
       'scope_lock_busy','claim_fence_busy','graph_unavailable')),
    failure_class TEXT NOT NULL DEFAULT '',    -- failed rows: e.g. graph_residue
    failure_details JSONB NOT NULL DEFAULT '{}'::jsonb,  -- 2z residue census
    reason_code TEXT NOT NULL CHECK (reason_code IN ('operator_retired')),
    reason_hash TEXT NOT NULL, actor_class TEXT NOT NULL, actor_id_hash TEXT NOT NULL DEFAULT '',
    idempotency_key_hash TEXT NOT NULL, scope_id_hash TEXT NOT NULL,
    generation_ids_hash TEXT NOT NULL, generations_fenced INTEGER NOT NULL,
    rows_deleted JSONB NOT NULL DEFAULT '{}'::jsonb,  -- {table: count}, summed per batch
    graph_nodes_deleted BIGINT NOT NULL DEFAULT 0,
    graph_relationships_deleted BIGINT NOT NULL DEFAULT 0,
    intent_barrier_at TIMESTAMPTZ NULL,        -- set after 2b's intent delete commits
    lease_owner TEXT NULL, claim_until TIMESTAMPTZ NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0, next_attempt_at TIMESTAMPTZ NOT NULL,
    requested_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL,
    retired_at TIMESTAMPTZ NULL, readmitted_at TIMESTAMPTZ NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS repository_retirements_open_repo_idx
    ON repository_retirements (repo_id) WHERE readmitted_at IS NULL;
CREATE INDEX IF NOT EXISTS repository_retirements_runnable_idx
    ON repository_retirements (next_attempt_at)
    WHERE state IN ('pending','running','blocked','repairing_graph');
CREATE INDEX IF NOT EXISTS repository_retirements_scope_idx ON repository_retirements (scope_id);
```

An open row (`readmitted_at IS NULL`) in any state except `complete` refuses
ingest. A `complete` row is a tombstone. The next ingest re-admits the
repository and stamps `readmitted_at`. The `Ingest refusal` column below gives
the bounded refusal each writer returns.

| Writer | Check point | Ingest refusal |
| --- | --- | --- |
| Collector commit: `CommitScopeGeneration` / `CommitClaimedScopeGeneration` (`ingestion.go:118-143`), bootstrap-index (`cmd/bootstrap-index/bootstrap_collector_commit.go:79`), ingester's in-process path, collector dead-letter replay (`recovery.go:288-311` re-commits) | Inside `commitScopeGeneration`, after the shared advisory lock (`ingestion.go:219`), folded into `upsertIngestionScope` (`:231`) so the check and the write share one snapshot (see [Commit Gate](7766-repository-retirement-concurrency.md#commit-gate-folded-into-the-scope-upsert)) | `repository_retiring`. Roll back, drain the stream, return nil. This mirrors the finalized-skip branch (`ingestion.go:237-251`), so there is no dead letter. |
| Webhook handoff (`collector/repo/git/webhook_trigger_selector.go:142`) | After `repositoryIDsFromWebhookTriggers`, match on `repo_slug_key` for open, non-`complete` rows | `MarkTriggersFailed(..., "repository_retiring", ...)`, a new constant beside `:36-40`. A slug-form miss still hits the commit fence. |
| Projector claim (`projector_queue_claim_sql.go:158-179`) | No SQL change. Phase 1 supersedes the generations, so the #7130 branch supersedes claimable rows. Heartbeat refuses (`projector_queue_scan.go:405-420`), Ack refuses (`projector_queue_sql.go:108-116`, the refusal marker at `:275-305`), and replay is fenced (`recovery.go:119`, `projector_queue_sql.go:307-316`). | Existing `projector_superseded_by_newer_generation` / ack-superseded classes. |
| Projector zombie heal (`projector_queue_zombie_heal.go`, #7209) | No change. The heal re-opens the scope's active generation row only when `scope.active_generation_id IS NOT NULL` (`:97`), and 7b nulls it. A heal whose snapshot predates phase 1 can insert a pending projector row of a superseded generation; the #7130 claim branch supersedes it. | None needed. |
| Reducer claim | Phase 1 marks the claimable reducer rows `superseded` (7d) under the claim fence. Later inserts and reopens are closed by 2q (see [Reducer Reopen And Replay Writers](7766-repository-retirement-reducer-writers.md)). | No claimable row at commit |
| `recover-generations` / `refinalize` (`rebuild/reset/reset.go:63-100`) | New first CASE arm: `EXISTS` open row → skip. Named unknown scope with a `complete` row (`refinalize.go:262-267`) | `skip_reason=repository_retiring` / `repository_retired`, new `recovery.SkipReason*` |
| Repository reindex (`query/admin/reindex_repository.go:68`) | After `gitDefaultScopeMatch`, look up the open row | 400 problem `"<sel>": repository is retiring (state <s>)`. Phase 1 also deletes the scope's `repository_reindex_requests` row. |
| Deferred backfill (`ingestion_backfill.go:333`, under the same exclusive repo lock) | Filter repo ids that have an open row from `loadGenerations` | skipped; `writer=deferred_backfill` refusal count |

Backfill must be fenced explicitly. `latestGenerationCTE`
(`latest_generation_cte.go:41-52`) falls back to the newest generation of any
status, so clearing `active_generation_id` does not hide a retiring scope from
backfill or from freshness resolution. Two other writers are exempt: the
`ingestion_scopes` writers in `vulnerability_suppression_store.go:26` and
migration 115 write non-git scopes.

## Fence Sequence

### Phase 1: mark and fence, one transaction

The API runs this inside its response window. `WriteTimeout` is
`DefaultRefinalizeDrainTimeout + apiRecoveryResponseMargin`, 5 min plus 1 min
(`cmd/api/main.go:26`, `:130-131`; `refinalize.go:31`). Phase 1 therefore runs
under **one deadline of 5 min 30 s** from request start. Every wait below is
bounded by `min(its own bound, remaining)`. The
[lock budget](7766-repository-retirement-concurrency.md#phase-1-lock-budget)
justifies the waits, bindings, and request bound. The drain, recheck, and fence
helper is new and bound by `scope_id = ANY`
([why](7766-repository-retirement-corrections.md#round-3-corrections)):
`WaitForReducerDrain` and `AcquireReducerClaimFence` in `rebuild/reset` bind
`(scope_id, generation_id)` pairs (`refinalize.go:148-150`), which is the
refinalize shape and the wrong one here.

1. Read the generation set of every scope with `partition_key = ANY($repo_ids)`.
   The read takes no locks. This mirrors `ReadAffectedGenerations`
   (`refinalize.go:227-271`). Build `reset.Generations`.
   - **Precheck.** Count non-superseded generations and reducer rows matching
     step 7d's predicate. Over 5,000 generations or 20,000 rows in total: 409
     `request_too_large` with per-repo counts. These limits are candidates that
     P1' confirms or lowers.
2. The scope-bound drain wait runs while the transaction holds no locks. Bound:
   `min(5 min, remaining)`.
3. Take `lockstore.AcquireDeferredMaintenanceRepoExclusiveLocks(repo_ids)`
   (`lock/deferred_maintenance.go:66-79`, sorted). The same key is held in
   shared mode by every commit (`ingestion.go:219`), so this waits for in-flight
   commits of these repos and blocks new ones. `lock_timeout` is computed from
   the remaining budget minus a fence reserve (see the lock budget), not a
   constant.
4. Re-read the generation set under the lock. It is now authoritative,
   because no commit for these repos can land. If it exceeds the step 1 budget,
   abort with the same 409. If it grew within budget, **roll back and restart
   at step 1** inside the same deadline. Do not wait for a drain while holding
   the key: a draining reducer can itself need the shared key (the
   acceptance-unit gate at `repo_dependency_acceptance_gate.go:72` and the
   acceptance writer at `shared_intent_acceptance_writer.go:83-87`), so a
   drain under the exclusive key can stall until the deadline.
5. `SELECT ... FROM ingestion_scopes WHERE partition_key = ANY($1) ORDER BY scope_id FOR NO KEY UPDATE`.
   - The scope row is locked first, before any generation or work row (postgres `AGENTS.md`, Ack lock order).
   - NO KEY UPDATE, not UPDATE, keeps FK `KEY SHARE` inserts compatible. A
     concurrent refinalize that holds the table lock and inserts work for this
     scope (`refinalize.go:80-132`) proceeds instead of deadlocking.
   - **Lock-order rule: advisory key, then scope row, then the
     `fact_work_items` relation.** A writer that blocks on a retired scope row
     or on the key takes it in a statement that references no other relation
     (Ack `projector_queue.go:221`, Finalize `activation/sql.go:68`, admin
     reopen `store/reopen.go:257`, the commit `ingestion.go:219` and `:231`).
     `Fail` violated this at the relation level and is fixed in PR 4a, which
     lands before PR 5 (see
     [Lock-Order Rule](7766-repository-retirement-concurrency.md#lock-order-rule-and-the-fail-fix)).
6. `set_config('lock_timeout','1s',true)`, then the scope-bound fence:
   `LOCK TABLE fact_work_items IN EXCLUSIVE MODE`, then recheck live reducer
   leases by `scope_id = ANY`. On `55P03`: roll back, sleep a jittered 100 to
   500 ms, and re-run steps 1 to 8. After a 30 s total fence budget (also
   capped by the remaining deadline), return 409 `blocked` / `claim_fence_busy`.
7. Writes (7f, the shared-lease horizon, was removed; letters are kept so the
   proof outputs still map):
   - (a) `UPDATE scope_generations SET status='superseded', superseded_at=$now WHERE scope_id = ANY AND status IN ('pending','active','failed')`;
   - (b) `UPDATE ingestion_scopes SET active_generation_id = NULL`;
   - (c) projector rows in `pending`/`retrying`, or `claimed`/`running` with an expired lease → `superseded` with `failure_class='repository_retired'`, bound to the step-4 non-superseded `(scope_id, generation_id)` pairs;
   - (d) **mark, not delete.** The reducer rows of the scopes (`scope_id = ANY`) in `pending, retrying, failed, dead_letter`, plus expired `claimed`/`running`, become `superseded`. The statement is in the
     [reducer writers file](7766-repository-retirement-reducer-writers.md#the-7d-statement).
     Succeeded rows are not touched in phase 1. 7d stays in phase 1: after 7b nulls the active pointer, the reducer claim would otherwise pick these rows up. It never deletes a row, because deleting is what re-opens the enqueue path (`ON CONFLICT (work_item_id) DO NOTHING` is inert for every id that still exists);
   - (e) `DELETE FROM repository_reindex_requests WHERE scope_id = ANY`;
   - (g) `INSERT INTO repository_retirements ... ON CONFLICT DO NOTHING RETURNING *` on the open-repo index. `DO NOTHING` returns no row on a conflict, so when `RETURNING` is empty the transaction reads the existing open row by `repo_id`. It holds the key and the scope rows, so the read is stable. Either path yields the row, which makes a re-issue idempotent.
8. `AssertRetirementFenced` (scope-bound form, counting generations superseded
   plus reducer rows marked; the shape of `refinalize.go:401-418`), then commit.

Phase 1 closes the claim path for rows that exist at commit. It does not stop
later inserts or reopens, and it does not carry I2. The 2q recheck under
`EXCLUSIVE` does (see
[Invariants](7766-repository-retirement-concurrency.md#invariants)).

The `rebuild/reset` invariant "never widen the reducer delete past `succeeded`"
protects live leases during a rebuild. Step 7d keeps its spirit: it never marks
a live-lease row, and it marks rows of already-superseded generations that
phase 2d's cascade would delete anyway.

Up to 25 repositories go in one request, all or nothing. That is the request
cap; the precheck is the cost bound. Errors:
- `InflightReducersError`, `55P03` at step 3, and `40P01` → 409 `blocked`, rolled back, no marker;
- `55P03` at step 6 retries, then 409 `blocked` / `claim_fence_busy`; over budget → 409 `request_too_large`;
- deadline exhausted in a wait → the 409 reason of that wait (`inflight_reducers`, `lock_timeout`, or `claim_fence_busy`);
- the idempotency ledger stays `in_progress`, as in `generations.go:118-122`. Retrying with a new key is safe, because step 7g resumes the same row.

Phases 2 and 3 are in the [Runner](7766-repository-retirement-runner.md).

## Re-Admission And In-Flight Syncs

The commit gate is folded into the scope upsert: an open non-`complete` row
refuses the commit with `repository_retiring`, and a `complete` row re-admits as
a first generation (`outcome=readmitted_after_retirement`). The gate SQL and the
interleavings are in the
[Commit Gate](7766-repository-retirement-concurrency.md#commit-gate-folded-into-the-scope-upsert)
and
[Re-Admission](7766-repository-retirement-concurrency.md#re-admission-and-in-flight-syncs)
sections of the concurrency contract.

## Risks And Rejected Alternatives

**Risks:**
- **Fleet-wide claim pause.** The `EXCLUSIVE` table lock blocks unrelated
  claim, heartbeat, and enqueue statements for the section, as refinalize does.
  Measured with design-form statements and a claim-shaped proxy (P1 blocking
  runs, n=10 each): the realistic 5,000-generation scope had a section p50 of
  47.9 ms and a max of 99.4 ms; the stalled scope p50 115.5 ms and max 229.1 ms
  (blocked statements max 230.5 ms). 2q step B is a second fenced section and
  carries its own bar (P1q). P1'' adds a fleet-throughput bar. The precheck
  bounds the work.
- **Barrier waits.** A worker that renews forever holds 2b' in
  `blocked/shared_lease_live` with the lease named, never failing
  automatically. NULL `claimed_at` rows block until their first re-claim. Wait
  length: P9d.
- **Fresh-id window between 2q and 2d.** An INSERT with a fresh
  `work_item_id` can land after 2q's last pass. 2d's cascade bounds it and 2z
  catches it. If 2z ever finds residue after a clean 2c, the structural fix is a
  claim-side supersede arm, tracked as a separate issue (see
  [Reducer Reopen And Replay Writers](7766-repository-retirement-reducer-writers.md#residual-risk)).
- **Retry, snapshot, gate shape.** A step 6 retry restarts from step 1. The
  precheck is a snapshot, and step 4 is authoritative. The gate moves
  `upsertIngestionScope` from `Exec` to `Query`; `LIMIT 1` relies on the
  partial unique open-repo index.
- **Retired id lingers in the ingester catalog.** The catalog read covers every
  `repository` fact (`ingestion_queries.go:19-24`), and the in-process catalog
  cache may hold a retired id until restart. Other repos' evidence then names
  it and produces truthful stubs.
- **Name confusion with the 7324 counter.** The new
  `eshu_dp_repository_retirements_total` sits beside
  `eshu_dp_canonical_repository_retirements_total`, which counts the
  path-conflict delete. The metrics doc must keep them apart.
- **Retiring a still-selected repo bounces it back.** The dry run warns.
- **Grants do not survive.** Phase 3 drops tenant and role grants on purpose.
  The dry run and the tombstone count them.

**Rejected:**
- **`ingestion_scopes.status` as the marker.** It is overwritten on every upsert.
- **A predicate in `claimProjectorWorkQuery`.** It is a hot path, and superseding the generations already fences the projector.
- **A marker gate on reducer enqueue or reopen.** Both are hot paths (enqueue
  runs per projector Run in 500-row batches; the reopen listings run every
  drain), nothing has measured a gate there, and it still leaves the fresh-id
  path unless the INSERT itself is gated. 2q closes the paths instead.
- **Deleting reducer rows in phase 1.** A deleted id is a free id: `DO NOTHING`
  stops being inert, and an enqueue re-creates the row. Marking keeps the id.
- **The shared-lease horizon, or claiming every shared partition.** The first
  is unsound under heartbeats (P9H), and the second pauses all shared
  projection fleet-wide. A claim epoch replaces both.
- **Dropping 7c, or a separate gate statement.** The first moves an unmeasured
  lock sweep into an unrelated claim; the second fails the 1% bar.
- **DETACH DELETE, or a conditional delete, of the Repository node.** See Graph Retraction.
- **Postgres purge before graph.**
  - G5 needs `content_files` paths, and G3 needs scope ids.
  - A permanent graph failure after the purge would leave a graph that Postgres
    cannot account for (`graph-rebuild-from-facts.md:55`).
  - The policy's privacy-first order is a stated non-goal here.
- **Graph retraction in the API.** The API holds no write credential.
- **A new per-repository lock, or serialising workers.** The existing advisory key already linearises commits.
- **Keeping the scope row, re-pointing grants, or a tombstone-held grant snapshot.**
  Keeping the scope row changes re-admission from a first generation and keeps
  catalog reads answering. There is no target to re-point to. Restoring a
  snapshot would re-grant access to content the retirement erased, which is a
  policy decision this primitive must not make.
- **Deleting webhook trigger rows.** They are delivery history.
- **A single-transaction purge.** It is unbounded.

## Open Questions

- What does the freshness route report for a `retiring` scope? The design adds
  only the `retired` verdict. Deliverable 1 never produces it, so a retiring
  scope keeps the verdict that `latestGenerationCTE` yields over its superseded
  generations. PR 8 decides whether to add `retiring` to the enum.
- Should the other per-repository routes (context, story, stats) return the
  policy's tombstone envelope instead of 404 after completion? A proposed
  follow-up, not required by the ruling.

## Non-Goals

These follow the arbiter ruling: no auto-retire, no selection change, no policy
engine, no tenant offboarding, no privacy guarantee beyond the proof, no
retention sweep, and no observation-row sweep (#7774).
