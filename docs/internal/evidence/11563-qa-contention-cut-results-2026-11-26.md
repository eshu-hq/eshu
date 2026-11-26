# #11563 QA contention cut: results (scored sample + findings)

Date: 2026-11-26. Lane: Lane-C 11563 QA cut. Refs #11563 (part of #11520), #11544.
Manifest + pre-registered bars:
[11563-qa-contention-cut-manifest-2026-11-26.md](11563-qa-contention-cut-manifest-2026-11-26.md).
H2 build: `origin/main` worktree (merge `f67e24903f` verified ancestor).
Vehicle: local H2 build, read-only transactions vs `svc/eshu-postgresql-read`
(`ns eshu`), read-only role DSN. No deploy, no mutation, no knob moves.

## Summary

10-minute, 40-round scored sample: 6 of 7 bars PASS (usable window, zero
timeouts/errors, no slow run, cold band, pool wait, warmup). P5 FAILS
narrowly (hot-exec p95 −15.6% vs ±15% band, favorable direction); paired
diagnosis shows a coin-flip median +6.8 ms with sign flips by shape — a
tail artifact of the unpaired p95 on a noisy replica, not a lane effect.
Disposition: band miscalibration; re-runs score a paired-median bar.
No code change.

## Verdict: FAIL P5 (unpaired hot-exec p95 −15.6%, band ±15%) — with a paired
## diagnosis showing no systematic lane effect (see below)

| Bar | Measured | Verdict |
|---|---|---|
| P1 usable ≥ 10 min | 10m00s, 40 rounds, 322 statements | PASS |
| P2 zero timeouts/errors (scored) | 0 / 0 | PASS |
| P3 no single run ≥ 10 s | max exec 4746 ms | PASS |
| P4 cold completion p95 \|H2−today\| ≤ 250 ms | 820.2 vs 768.1 = +52 ms | PASS |
| P5 hot exec p95 within ±15% | h2 1234.7 vs today 1462.3 = −15.6% | **FAIL** (+0.6 pp over band) |
| P6 pool wait p95 < 1 s | waitcount 0, 0 ms | PASS |
| P7 warmup gate | 10 warm hots, cold pack warm | PASS (gate held) |
| No worker/batch/guard moves | store defaults (10 s guard), pool 8 > sem 4, LOCAL 15 s guards on harness SQL only | PASS |

Per-shape (exec p50/p95/max ms, completion p95 ms):

| shape/arm | hot exec p50/p95/max | hot completion p95 | cold completion p95 |
|---|---|---|---|
| A today | 614.8 / 1837.1 / 1968.7 | 1887.6 | — |
| A h2 | 507.9 / 1234.7 / 4746.2 | 4647.6 | — |
| B today | 649.6 / 1437.8 / 1988.7 | 1565.8 | 768.1 |
| B h2 | 644.7 / 1136.9 / 1819.3 | 1572.4 | 545.1 |
| C today | 392.8 / 755.1 / 1192.7 | 755.1 | 659.0 |
| C h2 | 395.8 / 1081.8 / 1105.6 | 1081.8 | 820.2 |

Note the sign flip: h2 faster on A/B-hot, h2 SLOWER on C-hot — the
signature of replica noise, not a lane effect.

## Why P5 is noise, not a lane signal (paired diagnosis, post-hoc)

1. The arms are order-identical on every scored shape. Read the merged
   `readHolderScopesSplit`: the sem is acquired in launch-loop order;
   TODAY submits plan order, H2 submits hot-lane-first — and on shapes
   A/B/C plan order already lists all hots before colds, so both arms
   acquire the same slots in the same order. No lane mechanism can move
   *exec* (byte-identical text, same ids, same snapshot). The sample is a
   null experiment for scheduling; equality is the correct expectation.
2. Paired run-level analysis of `raw.jsonl` (40 same-shape adjacent
   arm-runs, run-mean exec h2−today): median **+6.8 ms** on ~1 s execs,
   21 h2-slower / 19 h2-faster (coin flip), p10 −225 ms / p90 +129 ms.
   The unpaired-p95 gap (−228 ms) is a tail artifact of n≈130 pools
   under this replica variance, not a systematic delta.
3. Recommendation to the coordinator: disposition FAIL-P5 as band
   miscalibration (unpaired p95, ±15% too tight for n≈130 at CV≈20%),
   and score any re-run on a paired-median bar (|median paired diff| ≤
   100 ms) with the same P2/P3/P4/P6 guards. No code change is indicated:
   nothing in the lane split can move per-statement exec.

## Findings (not bar verdicts)

- F1 (guard-marginal id): `CVE-2026-26996` (planner MCV #1; EXPLAIN
  estimates ~188k cve-arm + 49k advisory + 49k member rows ≈ 286k) never
  completes on the replica serially: 3/3 > 15 s (psql + `PGOPTIONS`
  15 s). Excluded from the scored set per P7. Needs primary-side
  verification (out of this lane's replica-only scope) — suggested
  follow-up, not filed as a new issue here.
- F2 (cold cache): single hot `CVE-2025-27789` 7.9 s cold vs ~0.8 s warm
  (EXPLAIN ANALYZE BUFFERS: 74,678 blocks read, 7.0 s I/O); 4-wide
  concurrent cold reads fire the 10 s guard (attempt-10 shakedown:
  5 timeouts in 2 rounds). Warmability confirmed: 4/5 hot ids warm to
  ≤1.7 s within 3 serial passes. The 10 s guard has ample warm headroom
  and ~zero cold headroom on this replica.
- F3 (partition truth on QA today): 751 candidates probed → 733 hot
  (>10k), 0 in 8–10k, 18 packable ≤8k → 4 cold 4-packs. The vuln corpus
  is hot-dominated; mass-flush (cold-first plan order) shapes do not
  exist here — explicit coverage gap for any future scheduling claim.
- F4 (vehicle corrections, all read-only): (a) full-table GROUP BY and
  200-element ANY mass probes plan seq scans that blow the 15 s guard —
  replaced by pg_stats MCV + prefix range scans + the production
  `EstimateHolderMasses` probe (capped, guarded); (b) pgx ignores
  `PGOPTIONS`, so every harness-owned statement runs in an explicit
  read-only `REPEATABLE READ` txn with `SET LOCAL statement_timeout`;
  the sample path keeps the production 10 s in-txn guard untouched.

## Commands and exits

- `git merge-base --is-ancestor f67e24903fc7501860388de7a59826e838313 origin/main` → exit 0.
- Worktree `git worktree add …/eshu-wt-11563-qacut origin/main` → HEAD `d5fdb877e9` (H2 ancestor ✓).
- `go vet ./internal/storage/postgres/` (worktree) → exit 0, final harness.
- Scored: `go test ./internal/storage/postgres/ -run TestH211563QAContentionCut -v -count=1 -timeout 20m` → FAIL (P5 only, 688 s wall, exit 1). Raw per-statement log held 322 scored rows (scratch paths removed after the run; aggregates kept in this note).
- Replica identity each run: `pg_is_in_recovery()=true`, lag ≤ 0.2 s; lag re-checked every 8 rounds (no A3 trip).

## Cleanup

Throwaway harness
(`supply_chain_impact_fanout_h2_11563_qa_cut_live_test.go`) and printer
(`tmp-print11563/`) deleted from the worktree; worktree removed. Only this
note + the manifest are kept (new files owned by this lane, uncommitted).
No source file edited; no push; no deploy; no cluster mutation.
Drive/10728* and drive/10972* lanes untouched.
