# #10020 lock-scope fix — implementation evidence (drive/10020-retention-lock-scope)

Worktree: `.muse/worktrees/retention-lock-scope-fix`, base `8f6a605dd7`.
Implements the STOP ruling's fix path
(docs/internal/arbiter/10020-lift-stop-ruling-2026-11-13.md):
(1) repo-scoped infra orphan delete plan fix,
(2) drain-then-admit pre-delete for the chunk page drain,
(3) locked-recount vs planning-count telemetry split.

## Pre-edit gate (answered before any edit)

1. **Entry point**: `GenerationRetentionStore.PruneSupersededGenerations`
   (`go/internal/storage/postgres/generation_retention.go:77`), via
   `postgresGenerationRetentionPruner.PruneSupersededGenerations`
   (`go/cmd/reducer/generation_retention_wiring.go:205`) ←
   `Runner.RunOnce` (`go/internal/reducer/maintenance/retention/runner.go:223`).
   All four read before editing.
2. **Phase ordering**: search-index pre-delete (committed, pre-Begin) →
   chunk predrain (new, committed, pre-Begin) → Begin → work_mem →
   key-index guard → savepoint select → rollback → prescreen/progressive
   planning (unlocked) → admit (per-generation advisory try-lock) →
   narrowHoldStart → targeted re-lock → locked recount → prune steps →
   chunk/search finish → commit (ScopeLockHold measured). Post-commit:
   orphan/superseded sweeps (own txns, poll cadence), then recordResult.
3. **Data dependencies**: scope_generations/ingestion_scopes rows
   (eligibility + lock set, written by ingestion/reducer activation);
   fact_records (counts, pages; cascade-deleted at generation delete);
   content_entities/files (doomed prunes); infra_resource_entities
   (repo-locked mirror); fact_work_items (live-work exclusion + lock);
   search index rows (predrained pre-Begin).
4. **Re-trigger**: the runner loop re-polls while passes prune; skips and
   undrained/busy generations are re-selected next pass; crash resume works
   through still-superseded rows plus committed idempotent batches (page
   deletes by PK, keyed doomed prunes, generation row untouched until the
   final whole prune). Consumers see retention events rows, RowsPruned and
   Skipped tallies, phase durations, ScopeLockHold, and sweep cadences.

## Sequence and lock ordering

```
Runner.RunOnce
 ├─ search predrain (pre-Begin, committed batches, advisory 7427 + lock_timeout)
 ├─ chunk predrain (NEW, pre-Begin, committed batches, same lock class)
 ├─ Begin → guard → savepoint select → planning (unlocked, count_rows)
 ├─ admit (advisory try-lock search-drained ids; undrained → skip)
 ├─ gate: still-over-limit chunkTarget dropped + row_limit_chunked (never locked)
 ├─ narrowHoldStart → targeted relock (FOR UPDATE ... SKIP LOCKED)
 ├─ locked recount (locked_recount) → prune steps → finish → commit
 └─ post-commit sweeps, recordResult (metrics + log)
```

Lock order, every acquisition try-only (never waits on another retention
lock): predrain batch takes advisory 7427 try-lock → row FOR NO KEY UPDATE
SKIP LOCKED → repo advisory locks in repo_id order; the prune takes admit
advisory try-locks → targeted FOR UPDATE SKIP LOCKED in
(scope_id, generation_id) order → repo locks in repo_id order → work-item
locks in C order (#8261); derives take repo locks in the same order with
lock-delete-insert. No wait edges, no cycles. Prune-vs-predrain on one
generation is decided by the advisory lock; the loser skips.

No `FOR UPDATE` is relocated: the retired under-lock chunk drain held no
lock of its own (it ran under the scope locks); the scope `FOR UPDATE`
stays in the targeted query. The new predrain site is `FOR NO KEY UPDATE`
with the full eligibility re-check, so a concurrent activation or work
claim that commits between the batch snapshot and its row lock fails the
re-check (EvalPlanQual) instead of being deleted under. Concurrent fact
inserts are never blocked by it: their foreign-key check takes
`FOR KEY SHARE`, compatible with `FOR NO KEY UPDATE` (proven live, see
lane 2 tests).

## Lane 1 — orphan delete plan (index/rewrite, exact semantics)

Symptom: repo-scoped orphan delete
(`infra/inventory/retention.go:38-45`) seq-scanned all 183,775 mirror rows
with one `content_entities_pkey` probe per row (plan cost 284,710;
35.6 s phase on the first cycle).

Root cause (proven with the cheapest shim, read-only `EXPLAIN` on the QA
read replica, same batch + stats as the attribution shim): the
`IN (SELECT DISTINCT ...)` subquery over fact_records is overestimated
(5,728 rows for 1 repo), so the planner joins the mirror table by filter
after a sequential scan instead of driving the existing
`(repo_id, relative_path)` index. The join shape cannot use the index no
matter how small the batch slice is.

Fix: two-step in the same transaction — `SELECT DISTINCT repo_id ...`
then `DELETE ... WHERE repo_id = ANY($1::text[]) AND NOT EXISTS ...`.
Same repository set, same orphan predicate; the array binding drives one
index range per batch repository. The empty-set short-circuit preserves
the old `IN (empty)` semantics (delete nothing, no scan).

Plan evidence (same replica, same batch):
before `Delete ... cost=277,744 ... Seq Scan on infra_resource_entities
(177,363 rows)`; after `Index Scan using
infra_resource_entities_repo_path_idx ... Index Cond: repo_id = ANY (...)`,
cost 238.92. Fixture proof (20 repos × 10k mirror rows, batch touches 1):
old cost 101,340 with mirror seq scan, new cost 70,211 with bitmap heap
scan on the 9,785-row slice (RED then GREEN in
`TestRetentionLiveOrphanDeletePlanUsesRepoIndex`).

Rejected alternatives: (a) narrowing to `entity_id = ANY(deleted content
ids)`: deletes only rows this batch's content prune removed, so orphans
left by writer/reap/purge paths (none clean the mirror table — verified
by grep: no `infra_resource_entities` reference in content_writer,
reap, or purge SQL) would leak; the repo-scoped sweep is their only
convergence. Exact semantics win over a narrower scan. (b) A covering
mirror index: the probe side (`content_entities_pkey` per row) is
untouched by it and there is no measured split showing the mirror side,
not the probe side, dominates; unmeasured index rejected per index
doctrine. Residual: when the batch repos own most of the table, the ANY
form visits the same rows in index order; QA re-earn decides.

TDD: plan RED on the old SQL (seq scan), GREEN on the new; differential
`TestRetentionLiveOrphanDeleteMatchesLegacySemantics` (frozen old SQL vs
new path, same shapes: same deleted counts and surviving rows per repo
role, untouched repo's orphans survive); existing exactness and fence
tests unchanged and green.

No `FOR UPDATE` relocated or added here (plain `SELECT` + `DELETE` inside
the prune tx, which already holds the scope locks); nothing to prove for
EvalPlanQual on this lane.

## Lane 2 — drain-then-admit chunk pre-delete (#7427 precedent)

Design: over-limit generations drain fact+content pages in committed
batches before `Begin`, oldest-first within a row cap (default 500,000)
and deadline (default 15 min; runner sets a quarter of the poll interval,
same divisor as search). Each batch: `SET LOCAL lock_timeout 500ms` →
advisory try-lock (class 7427, shared with the search pre-delete and the
prune's admission) → weakened targeted lock (`FOR NO KEY UPDATE ...
SKIP LOCKED`, identical predicates) with per-batch eligibility re-check
→ keyset page → repo locks → sidecar/file/entity prunes → mirror orphans
(page repos, `ANY` array) → commit. Outcomes: drained (short page),
budget, advisory-busy, row-busy, ineligible. Idempotent resume: the
generation row is never touched; committed batches stay deleted; a crash
resumes on the still-superseded row. Liveness: the first batch always
runs.

Drained-set admission: the pass admits only search-drained generations
(unchanged); a still-over-limit chunk target after the predrain means the
predrain did not finish it, so the gate excludes it with
`row_limit_chunked` instead of draining a page under the scope lock.
Fully drained targets fit and prune whole in the same pass. Under-lock
remainder: re-lock + locked recount + events + final delete only —
`relockChunkedBatch`/`pruneChunkedCandidate` and the `prune_chunked_facts`
phase are retired (a page under the lock was the 168.3 s ScopeLockHold
contributor). The dropped target is never relocked, so no recount and no
refit touch it: exclusion is structural.

Events/rows: predrained rows merge into the pruned generations' retention
events (`eventCounts`, mirroring search) and `RowsPruned` at finish;
generations left behind merge progress with the deferral skip. The recount
always emits zero rows per locked generation (LEFT JOIN from the candidate
CTE — verified in `generation_retention_row_counts_sql.go`), so
`eventCounts` never misses a pruned generation.

No serialization: workers, batch/page sizes, and the page bound are
unchanged (`chunkRows = min(BatchRowLimit, 100k)` as before); the bound
moved in time (pre-lock batches), not in size.

Concurrency proof (live, scratch PG 18.6):
`TestGenerationRetentionChunkPredrainConcurrentInsertLive` — 5 inserts
commit while a batch holds the generation's row lock (hook waits for all
5 before the batch proceeds; a FOR UPDATE regression would deadlock the
hook against the inserts and trip the 30 s bound), exact final counts,
whole prune same pass.
`TestGenerationRetentionChunkPredrainEligibilityFlipStopsLive` — a work
item claimed mid-drain stops the next batch (1 batch, 100 rows, skip,
generation retained, 200 facts left); after the claim clears, the next
pass resumes and prunes whole.
`TestGenerationRetentionChunkedDrainLive` — budget-capped cycle drains
one page with `ScopeLockHold == 0` (nothing locked: predrain committed
pre-Begin), next cycle drains the rest and prunes whole (final hold
16 ms on the fixture for relock+recount+prune), orphan-free end state,
1 retention event.
`TestGenerationRetentionChunkLockMissDropsSilentlyLive` — a held row is
skipped at every level, nothing pruned, facts intact.
Fake unit proofs: drain-to-fit→whole-prune, budget stop/resume shape,
advisory-busy deferral, mid-drain ineligible flip, event/finish
attribution.

Residuals: an activation racing a batch waits on the batch's brief row
lock (tens of ms); a flip between predrain and relock deletes already-
drained oldest-first pages of a generation that later turns protected
(same accepted class as the targeted lock's documented residual — the
generation row itself is never pruned while protected).

## Lane 3 — locked-recount vs planning-count split

`GenerationRetentionPhaseLockedRecount = "locked_recount"`: the two
relock recounts report under it; prescreen/progressive/recheck planning
counts stay under `count_rows`. Failing-first fake test
(`TestGenerationRetentionLockedRecountHasItsOwnPhase`) RED→GREEN.
Docs: `metrics-reducer-storage.md` phase list (including the
`prune_chunked_facts` retirement note), telemetry-coverage row, runner
log + `eshu_dp_generation_retention_chunk_predelete_batches` histogram
(emission proven by
`TestGenerationRetentionRunnerRecordsChunkPredeleteBatches`; verifier
`scripts/verify-telemetry-coverage.sh` passes).

## Performance Evidence

- Conflict domain: retention prune transaction vs concurrent fact inserts
  into the pruned scopes; orphan-delete plan on `infra_resource_entities`.
- Before/after row counts: orphan delete plan visits 177,363 mirror rows
  by seq scan (cost 277,744) → index ranges over the batch slice (cost
  238.92) on the QA replica; chunk drain moves 168.3 s of under-lock page
  work to committed pre-Begin batches (cycle-1 `ScopeLockHold == 0`
  proven live; final whole-prune hold 16 ms on the fixture).
- Metrics/spans/logs: `locked_recount` + `predrain_chunk` phases,
  `ScopeLockHold`, `ChunkPredeleteBatches` histogram + cycle log field,
  per-generation predrain left-for-later log, row_limit_chunked skips.
- No live-QA timing claim is made here: Gate 3 re-earn on flip-scale
  batches (≥3 live cycles, during-hold pg_stat_activity, KEV re-measure,
  steady-state sweeps) remains open and is coordinator-owned.

## Observability Evidence

- New phases `locked_recount`, `predrain_chunk`; retired
  `prune_chunked_facts` (documented in both telemetry refs).
- New instrument `eshu_dp_generation_retention_chunk_predelete_batches`
  + `chunk_predelete_batches` cycle log field; predrain left-for-later
  per-generation log mirrors the search pre-delete's.
- Skip reasons reused closed set (`row_limit_chunked`,
  `search_index_undrained`); no new unbounded labels.

## NOT_CHECKED / remaining proof (coordinator-owned)

QA-scale Gate 3 re-earn (>=3 live cycles at backlog shape, ScopeLockHold
<=5 s, recount <=2.0 s, planning <=6.1 s, during-hold lock-wait
telemetry, drain projection, storage growth, steady-state sweeps); KEV
re-measure and #9805/#9975/#10020 closure; `make pre-push` / `make
pre-pr` / review-attest / push / PR (not run in this lane per the
handoff).
