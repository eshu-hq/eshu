# #11563 QA contention cut: run manifest + abort bars (pre-registered)

Date: 2026-11-26.
Refs #11563 (part of #11520), #11544, #11430.

## Summary

Pre-registered plan for the H2 step-3 contention cut: a >=10min
vuln-filtered pass sample of hot/cold holder reads against the QA read
replica, scored on 7 bars (usable window, zero timeouts, no slow run,
cold/hot p95 bands, pool wait, warmup gate). The vehicle is a local H2
build in read-only transactions; no deploy, no mutations, no knob moves.

## 1. Objective

Step-3 contention cut from the [H2 isolation diagram](11520-h2-isolation-diagram-2026-11-26.md):
>=10 min vuln-filtered pass sample against the QA read replica with the H2
lane build: hot-statement p95, cold-pack p95 vs today, pooled-conn wait, zero
timeouts. No worker, batch, or guard knob moves.

## 2. Merged-truth verification (done before the run)

- `git merge-base --is-ancestor f67e24903fc7501860388de7a59826e186e3e313 origin/main`
  → exit 0 (H2 merge #11544 is in main).
- H2 code: `go/internal/reducer/supplychain/core/fanout_split.go`
  (`h2HotLane`, hot-first two lanes, one shared semaphore of
  `fanOutHolderReadParallelism`), statement text byte-identical
  (`FindSupplyChainImpactHoldersQuery`), 10 s guard intact
  (`supplyChainImpactFanOutReadTimeout`, pinned by
  `supply_chain_impact_fanout_h2_claim_guard_contract_test.go`).
- Step-3 bar source: diagram section (c) item 3.

## 3. Vehicle (read-only; no deploy, no mutation)

Local H2 build from a clean `origin/main` worktree drives ONLY the
production holder-read path
(`SupplyChainImpactFanOutStore.FindHolderScopes` → `readScopeIDsIn`:
read-only `REPEATABLE READ` txn, supports-ready gate, per-statement 10 s
guard, `ROLLBACK`) against `svc/eshu-postgresql-read` (`ns eshu`) via
`kubectl port-forward`, read-only role DSN. Guard note (attempt-3
correction): pgx ignores `PGOPTIONS`, so the session env is NOT a
backstop for the Go harness — every harness-owned raw statement runs in
an explicit read-only `REPEATABLE READ` txn with `SET LOCAL
statement_timeout = '15s'`; every sample statement runs through the
production `readScopeIDsIn` path with its own 10 s in-txn guard.

Sample shape per round (one pass slice, vuln-filtered ids discovered
read-only on the replica: pg_stats MCV + worst id + prefix scans +
page-uniform micro-sample candidates, masses from the production
`EstimateHolderMasses` probe (chunked guarded statements, cap 10001 =
hot threshold + 1). Round = one real 5-id chunk plan rotating over shapes
A (5 hot singletons), B (4 hot + 1 cold singleton-pack), C (1 hot +
1 cold 4-pack, combined <= 8k); chunks run serially as in production,
concurrency only within the chunk under sem 4. Same multiset both arms
per round, only launch order differs (TODAY plan order vs H2 hot-first).
Mass-flush shapes (cold-first plan order) do not exist on this corpus
(no 5–9k ids) and are an explicit coverage gap:

- Arm TODAY: the round's statements through one FIFO queue, sem 4, plan
  order (shipped scheduling).
- Arm H2: same statements, hot singletons launched first into a hot lane,
  cold packs into a cold lane, one shared sem 4 (H2 scheduling).
- Arms interleave ABBA per round to control replica drift; identical id
  set both arms. Pool `SetMaxOpenConns(8)`; sem 4 is the bound under test.
- Cadence ~1 round / 15 s for >= 10 min usable (>= 40 rounds).
- Metrics per statement: exec latency, sem-wait (scheduling wait),
  pool-begin wait; per round: replica replay lag
  (`pg_last_xact_replay_timestamp`); counts: timeouts, errors.

No worker, batch, or guard knob moves: store defaults (10 s guard),
pool 8 (wider than the sem-4 bound, never the bottleneck), no SET beyond
the production read path plus the outer read-only/15 s backstop.

## 4. Bars (registered before the sample; judged on the full usable window)

PASS (all must hold):

- P1: >= 10 min usable sample (first discovery-only minute excluded).
- P2: zero statement timeouts, zero errors.
- P3: no single statement run >= 10 s.
- P4 (revised, mechanism-derived 2026-11-26): on the scored chunk
  shapes the first-4 semaphore sets are identical both arms (read the
  merged `readHolderScopesSplit`: sem acquired in launch-loop order;
  TODAY plan order vs H2 hot-first differ only past slot 4, and these
  shapes never reach past-slot-4 divergence), so H2 must show
  scheduling EQUALITY: cold completion p95 |H2 − today| <= 250 ms,
  no cold timeout, cold max < 10 s. A hot-priority shape where cold
  queues behind burning hots is accepted by design and is NOT in the
  scored set (no mass-flush shape exists on this corpus).
- P5: hot-singleton exec p95 H2 within ±15 % of today (byte-identical
  text and plan; larger drift means replica drift, not a lane effect —
  investigate, do not pass).
- P6: pool-begin wait p95 < 1 s both arms (no replica distress).
- P7 (added 2026-11-26): the scored sample runs ONLY on ids that warm
  serially to <= 2.5 s within 3 passes; anything else leaves the scored
  set with a finding, and fewer than 5 warm hots or no fully-warm cold
  pack is INCONCLUSIVE (replica cannot hold the working set), not FAIL.
  Cold-cache numbers (7.9 s single hot, attempt-10 concurrent guard
  fires) are reported as findings F1/F2, never as bar verdicts.

ABORT (stop the run immediately, verdict FAIL unless noted):

- A1: any statement timeout or any error → abort (P2 breached).
- A2: any single run >= 10 s → abort (P3 breached, guard fire).
- A3: replica replay lag > 60 s at any 2-min check → abort, verdict
  INCONCLUSIVE (replica distress, not a lane signal).
- A4: pool-begin wait p95 > 5 s over any 10-round window → abort,
  INCONCLUSIVE (same reason).
- A5: a peer live run appears on the replica or the shared host →
  abort, INCONCLUSIVE, reschedule.
- A6: wall clock > 20 min or usable sample < 10 min → INCONCLUSIVE,
  no PASS claim.

FAIL (run completes, a PASS bar misses): record which bar, keep the raw
per-statement log as the evidence artifact.

## 5. Commands (exact)

```sh
# worktree (clean main; H2 in history)
git worktree add <scratch worktree> origin/main
# replica (read-only; port-forward killed by trap at harness exit)
kubectl -n eshu port-forward svc/eshu-postgresql-read 54399:5432
# read-only DSN from the cluster secret (value never recorded)
# harness (throwaway test file owned by this lane, deleted after the run;
# only this manifest + the results note are kept; the file no longer
# exists, so this is a record, not a re-runnable command)
# scored via the throwaway TestH211563QAContentionCut harness (verbose,
# single count, 20 min timeout) against the read-only DSN above, with
# 15 s statement timeout and read-only transactions on harness SQL
```

## 6. Time box

Setup ~20 min, sample ~12 min, analysis ~10 min. Deadline ~60 min from
claim (2026-11-26 ~17:00 EST). A failed attempt does not restart the
budget; on expiry the lane reports INCONCLUSIVE with what ran.

## 7. What this lane owns

This manifest + the results note
(`11563-qa-contention-cut-results-2026-11-26.md`) + the #11563 comments.
The throwaway harness is deleted after the run; no source file is
edited; no push, no deploy, no cluster mutation.
