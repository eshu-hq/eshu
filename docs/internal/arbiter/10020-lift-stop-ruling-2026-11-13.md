# #10020 lift first-cycle STOP — arbiter ruling (2026-11-13)

RULING: STOP CONFIRMED and REVERT — set the soft window back to 2160h now;
the 223s hold is systematic under-lock work by design, not cold-cache noise;
no lift progression, KEV re-measure, or #9805/#9975/#10020 closure until the
fix re-earns Gate 3 on flip-scale batches.

Arbiter: `arbiter-eshu` (`muse-wonder-full-1.0`). Skills: eshu-postgres-rigor,
concurrency-deadlock-rigor, eshu-performance-rigor, eshu-diagnostic-rigor.
Verified from source at `main` 8f6a605dd7 (generation_retention.go,
generation_retention_relock.go, generation_retention_policy.go,
generation_retention_events.go, generation_retention_chunk_query.go,
retention/statements/targeted_candidate_lock.go, generation_retention_sql.go,
generation_retention_row_counts_sql.go, reducer retention runner/sweeps,
generation_retention_wiring.go, facts/keys/delete.go, infra/inventory/
retention.go, search/index/retention drain/census) and `gh issue view`
9805/9975/10020 (all OPEN). QA figures are coordinator raw observations:
first cycle 13:08Z, 8 generations pruned, 167,634 rows, skipped 5
(row_limit 4, row_limit_chunked 1), duration 265.6s, ScopeLockHold one
observation 223.07s; phases prune_chunked_facts 168.3,
census_search_index_orphans 79.7, census_search_index_superseded 46.9,
count_rows 43.7, delete_infra_orphans 35.6, sweep_search_index_superseded
23.3, delete_scope_generations 11.2, select_candidates 0.6,
key_index_check 0.005, commit 0.009. Lift PR eshu-deploy#674 (7b9c15a3),
engine 168h soft / 2160h hard; eligible census at flip 15,054
generations / 925 scopes (Gate time: 5,581 / 229).

## Decisions

1. STOP-CONFIRMED, scope = whole lift. 223.07s is 44.6x the Gate 3
   ScopeLockHold bar of 5s (docs/internal/evidence/
   10525-retention-planning-count.md Gate 3 bars; arbiter/
   10525-sequencing-ruling-2026-11-10.md trigger item 2). No second
   sample; a "check if cold" cycle at 168h costs another breach.
2. REVERT the soft window to 2160h now
   (ESHU_GENERATION_RETENTION_MAX_SUPERSEDED_AGE=2160h,
   go/cmd/reducer/config.go:44; unset hard resolves to 2160h,
   go/cmd/reducer/generation_retention_wiring.go:37-40). HOLD rejected:
   the runner loops immediately while passes prune
   (https://github.com/eshu-hq/eshu/blob/8f6a605dd75cb625db3f2096027598deee2700dc/go/internal/reducer/maintenance/retention/runner.go#L190-L192), so
   ~1,882 back-to-back passes (~8 pruned/pass, ~5.8 days at 265.6s)
   repeat the breach; post-cycle 200/queue checks are not during-hold
   lock-wait evidence; pausing keeps history (accuracy-safe, costs
   storage). Enabled=false is unavailable in production profiles
   (generation_retention_wiring.go:75-90); the window is the lever.
   Flip at a pass boundary if observable. Post-revert watch: >2160h
   generations still prune via the hard arm; holds must respect 5s.
3. Systematic, not cold. Under FOR UPDATE OF generation, scope SKIP
   LOCKED (retention/statements/targeted_candidate_lock.go:147) from
   narrowHoldStart to commit (generation_retention.go:142-153,
   172-174) run: chunk page drain
   (generation_retention_relock.go:266, 432-496), locked recount
   (generation_retention_relock.go:164, 285), and all pruneCandidates
   steps (generation_retention.go:405-444). Outside the lock: search
   pre-delete before Begin (generation_retention.go:93); planning after
   savepoint rollback (generation_retention.go:244-253,
   generation_retention_chunk_query.go:134-157); orphan/superseded
   census+sweeps after the prune, own transactions, once per poll
   interval (runner.go:233-240, orphan_sweep.go:54-62,
   superseded_sweep.go:55-57) — census time never holds scope locks.
   Chunk 168.3 + infra orphans 35.6 + scope generations 11.2 = 215.1s
   wholly under lock; residual 7.97s covers re-lock, locked recount,
   remaining prune steps, commit. Infra+generation deletes alone are
   9.4x bar. Cold buffers cannot explain 44.6x.
4. Brief corrections. Planning PASS from select_candidates is the wrong
   phase: planning counts accrue to count_rows
   (generation_retention_chunk_query.go:145-147), which aggregates
   unlocked planning with the locked recount
   (generation_retention.go:342-347). Planning >= 43.7 - 7.97 = 35.7s
   vs <=6.1s bar (5x under 30.5s): FAIL proven by subtraction. Locked
   recount vs 2.0s bar is indeterminate (bounded <=7.97s); the total
   count_rows phase is not an x2-countable signal for that bar.
5. Fix path: attribution shim first (per-statement split of
   prune_chunked_facts, infra-orphan delete, locked recount, cascade
   delete; EXPLAIN (ANALYZE, BUFFERS) cold/warm on flip-scale fitted
   batches). Then move bulk deletes out of the lock window following
   the #7427 committed-batch pre-delete precedent (advisory lock,
   per-batch eligibility re-check, drained-set admission:
   search/index/retention/drain.go:25-50,
   generation_retention_policy.go:371-434), chunk drain first — its
   pages already commit per cycle with the generation row untouched
   (generation_retention_relock.go:421-431). Under-lock remainder:
   re-lock + locked recount + events + final generation delete. If
   attribution proves one statement's plan dominates (e.g. the
   repo-scoped orphan delete, infra/inventory/retention.go:38-45),
   fix that statement; Gate 3 bars decide.
6. #9805/#9975/#10020 wait (all OPEN). KEV scope undrained; no KEV
   re-measure as acceptance evidence and no closure until lift-GO.

## Required proof for lift-GO

1. Shim above with thresholds stated upfront, flip-scale batch shape
   included.
2. Telemetry split: locked recount and planning count separately
   observable; aggregated count_rows cannot evidence Gate 3.
3. Live-Postgres concurrency proof: concurrent fact insert during
   drain/prune (#8206 style, docs/internal/evidence/
   8206-retention-insert-wait-hook.md), eligibility flips between
   drain and admission, crash-resume idempotency with no orphan
   keys/content, EvalPlanQual drop.
4. >=3 consecutive live cycles on the rebuilt binary at backlog shape
   (one chunked, one whole-generation-only): ScopeLockHold <=5s,
   locked recount <=2.0s warm, planning <=6.1s; during-hold
   pg_stat_activity/pg_blocking_pids or lock-wait telemetry; reducer
   pending/retrying before/during/after attributed.
5. Drain projection (rows/pass, pass wall, estimated backlog-clear
   time) and storage growth during the pause.
6. Steady-state (second+) hourly census durations for orphan and
   superseded sweeps.

## Will not do

Raise the 5s bar; cold-cache waiver; second 168h experiment cycle;
serialization (ingestion pause, worker cuts, batch=1); unmeasured
BatchRowLimit/chunk-page shrink as sole fix; closures or KEV
re-measure on partial drain.

## NOT_CHECKED

Lift PR #674 diff and QA env (deploy repo); superseded-sweep flag
inferred ON from phases; verbatim STOP x2 text (bars corroborated,
x2 from coordinator); strict single-cycle phase deltas; sub-statement
split of prune_chunked_facts; exact locked recount; during-hold QA
lock waits; retrying +108 cause; passes after 13:08Z; >2160h
generations post-revert; current KEV generation count.
