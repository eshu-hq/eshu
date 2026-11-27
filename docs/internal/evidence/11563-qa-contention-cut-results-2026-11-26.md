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

Run 2 (same day, raw log retained as a secret gist, checksums in-note):
10m30s, 42 rounds, 336 rows — 8 of 9 bars PASS; P4 FAILS only on shape-B
cold completion (−545 ms, favorable direction, max-of-14 outlier), with the
paired-median bar at +43.9 ms (22/20 split) and the hot-completion guard at
+4.0% both PASS. No code change.

## Verdict (Run 1): FAIL P5 (unpaired hot-exec p95 −15.6%, band ±15%) — with a paired
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

   Hot completion (not exec) shows the same sign-flip pattern and no
   completion bar was pre-registered — a plan gap recorded here. Shape A:
   h2 hot completion p95 4647.6 vs today 1887.6 (+146%), but the A h2
   max exec is 4746.2 ms against an exec p95 of 1234.7 ms: one round
   dominates the p95, and exec p95 still favors h2 (1234.7 vs 1837.8).
   Shape C: h2 hot completion 1081.8 vs 755.1 (+43%) with h2 SLOWER on
   C-hot exec p95 too (1081.8 vs 755.1) — the same flip seen on B, where
   h2 is faster. Completion adds sem/queue wait to exec, but both arms
   acquire the shared sem in plan order on all three shapes (hots
   first), so no lane mechanism separates the arms; the gaps track exec
   outliers, not scheduling.
3. Recommendation to the coordinator: disposition FAIL-P5 as band
   miscalibration (unpaired p95, ±15% too tight for n≈130 at CV≈20%),
   and score any repeat run on a paired-median bar (|median paired diff| ≤
   100 ms) with the same P2/P3/P4/P6 guards, plus a hot-completion guard
   (p95 within ±20% or outlier-excluded parity). No code change is
   indicated: nothing in the lane split can move per-statement exec.

## Findings (not bar verdicts)

- F1 (guard-marginal id): `CVE-2026-26996` (planner MCV #1; EXPLAIN
  estimates ~188k cve-arm + 49k advisory + 49k member rows ≈ 286k) never
  completes on the replica serially: 3/3 > 15 s (psql + `PGOPTIONS`
  15 s). Excluded from the scored set per P7. Needs primary-side
  verification (out of this lane's replica-only scope) — filed as
  #11567 after the run (Refs #11567).
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

- `git merge-base --is-ancestor f67e24903fc7501860388de7a59826e186e3e313 origin/main` → exit 0.
- Worktree `git worktree add …/eshu-wt-11563-qacut origin/main` → HEAD `d5fdb877e9` (H2 ancestor ✓).
- `go vet ./internal/storage/postgres/` (worktree) → exit 0, final harness.
- Scored: the throwaway TestH211563QAContentionCut harness (verbose, single count, 20 min timeout) → FAIL (P5 only, 688 s wall, exit 1). Raw per-statement log held 322 scored rows (scratch paths removed after the run; aggregates kept in this note).
- Replica identity each run: `pg_is_in_recovery()=true`, lag ≤ 0.2 s; lag re-checked every 8 rounds (no A3 trip).

## Cleanup (Run 1)

Throwaway harness
(`supply_chain_impact_fanout_h2_11563_qa_cut_live_test.go`) and printer
(`tmp-print11563/`) deleted from the worktree; worktree removed. Only this
note + the manifest are kept (new files owned by this lane, uncommitted).
No source file edited; no push; no deploy; no cluster mutation.
Drive/10728* and drive/10972* lanes untouched.

## Run 2 (re-run 2026-11-26, raw log retained)

Same vehicle rebuilt from clean `origin/main` (`54c0f3e4a5`;
`git merge-base --is-ancestor f67e24903fc7501860388de7a59826e186e3e313`
exit 0): local H2 build, read-only txns vs `svc/eshu-postgresql-read`
(`ns eshu`) via port-forward, read-only role DSN, explicit read-only
`REPEATABLE READ` txns + 15 s LOCAL guards on harness SQL, production 10 s
in-txn guard on every sample statement. Zero mutations, no knob moves.
42 rounds, 336 scored rows, 10m30s usable. Replica
`pg_is_in_recovery()=true`, replay lag within 0.3 s, re-checked every 8
rounds. Warmup gate held: 14 warm hots, 2 warm cold packs.

Raw per-statement log (336 JSONL rows) and the verbose run log are kept
durably as a secret gist; nothing raw is committed, per the hygiene gate:
[run-2 raw log + run log](https://gist.github.com/linuxdynasty/05e9d775a57353f6c1b0661f14168fc6)
(`11563-qa-cut-run2-raw.jsonl` sha256
`af00d69c493eed334b985d3e449e291c1b4b78e09e66d0d4d7f1d5e149b07945`,
`11563-qa-cut-run2-run.log` sha256
`d5e461f81dec510479d0eb110fea935574eb51e7d8bfe123ae877765a45a4ac2`).

## Verdict (Run 2): FAIL P4 only (B cold completion −545 ms, favorable
## direction, single-outlier max-statistic) — all other bars PASS

| Bar | Measured | Verdict |
|---|---|---|
| P1 usable ≥ 10 min | 10m30s, 42 rounds, 336 statements | PASS |
| P2 zero timeouts/errors (scored) | 0 / 0 | PASS |
| P3 no single run ≥ 10 s | max exec 4074.6 ms | PASS |
| P4 cold completion p95 \|H2−today\| ≤ 250 ms | B −545.1 ms; C −175.7 ms | **FAIL** (B only) |
| P5-classic hot exec p95 within ±15% | h2 2064.8 vs today 2237.6 = −7.7% | PASS |
| P5-paired \|median paired diff\| ≤ 100 ms | median +43.9 ms, 22 h2-slower / 20 h2-faster | PASS |
| P6 pool wait p95 < 1 s | begin p95 595.0 ms over 479 begins | PASS (thin margin, see caveat) |
| P7 warmup gate | 14 warm hots, 2 warm cold packs | PASS |
| P8 hot completion p95 within ±20% | h2 2333.9 vs today 2243.7 = +4.0% | PASS |

Per-shape (exec p50/p95/max ms, hot completion p95 ms, cold completion p95 ms):

| shape/arm | hot exec p50/p95/max | hot completion p95 | cold completion p95 |
|---|---|---|---|
| A today | 653.0 / 2316.3 / 3935.2 | 2508.6 | — |
| A h2 | 684.9 / 2675.1 / 4074.6 | 3876.7 | — |
| B today | 714.7 / 2243.4 / 2592.4 | 2243.7 | 2133.9 |
| B h2 | 711.4 / 1491.7 / 2055.7 | 1491.9 | 1588.8 |
| C today | 478.9 / 1174.4 / 1174.4 | 1174.4 | 1124.7 |
| C h2 | 419.6 / 948.9 / 948.9 | 949.0 | 949.0 |

Sign flips by shape again (B h2-faster on hot exec, A h2-slower) — the
replica-noise signature from Run 1, not a lane effect. P4-B detail: today
cold top-3 = 2133.9 / 1494.6 / 1131.7 vs h2 1588.8 / 1258.3 / 908.8; the
p95 here is the max of n=14, so one slow today round decides the bar —
dropping it gives parity (1494.6 vs 1588.8). P6 caveat: per-row attributed
waits run higher under concurrency (p95 today 1107 / h2 1328 ms); both arms
share the port-forward vehicle, lag stayed ≤ 0.3 s with zero timeouts, so
this reads as vehicle multiplexing noise, not replica distress — flagging
it because the margin is thin. F1: 16/30 hot candidates failed the serial
warmup and were excluded; per-id warmup attempts were not retained (proof
gap recorded here), so F1 stays as the Run-1 finding (Refs #11567).

## Cleanup (Run 2)

Throwaway harness
(`supply_chain_impact_fanout_h2_11563_qa_cut_rerun_live_test.go`) deleted
from the scratch tree; scratch worktree removed. Only this note edit is
committed on `drive/11563-qa-cut`. No source file edited; no push; no
deploy; no cluster mutation. Drive/10728*, drive/10972*, h2-shim untouched.
