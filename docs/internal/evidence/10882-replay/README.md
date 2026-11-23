# Source backpressure replay: evidence record (#10882, #11186, #11187)

This record is rendered from the replay results directories (`render_evidence.py`) plus the raw check outputs in this directory. The results directories themselves (about 15 GB of exports, scrapes and logs) stay under `~/.cache/replay` and are not committed. Smoke figures are not evidence and are not used.

## Builds and runs

| Set | Build | Runs | Notes |
| --- | --- | --- | --- |
| Matrix `results-3980a07cad` | `3980a07cad`, product code of the evidence commit `487a0c53bd` (`git diff 487a0c53bd 3980a07cad -- go` is empty) | 26: C, C0, T1, T1TTL, C2, T2, T3, T3TTL x3; CF, CG x1 | 2026-11-23 19:01 to 20:39Z, driver `rc=1` (the checks below) |
| Supplementary quiet T1 `results-3980a07cad-T1q` | same binaries and driver | 3 (T1-q1..q3) | launched after matrix reps 2 and 3 were seen; labelled supplementary |
| Rerun and in-flight `results-fixbuild-9e63e08c11` | `9e63e08c11` = replay + cherry-pick of `3271cbf01b`; the fix is #11322 at `9322d85022` (merged as `5429946c0d`); non-test Go delta between them is comment text only | 9 (C2, T2, T2TTL x3), then 12 (IFC, IF, IWC, IW x3) | `git diff 487a0c53bd 9e63e08c11 -- go` lists the six fix files; sha256 of `eshu-ingester`, `eshu-projector`, `eshu-bootstrap-data-plane` and `eshu-collector-synth-zero` equal the matrix build's; only `eshu-collector-component-extension` differs |

A matrix start at 2026-11-23T01:19Z was aborted before its first arm finished and discarded (`results-f7f5c72361-ABORTED-not-evidence`); no result from it was judged.

## Host load, matrix

One sample a minute: min 1.3, median 4.2, max 40.6 over 98 samples.

## Per run, matrix (26 runs)

| Run | TTL | consult p50 ms | consult p95 ms | share in le=0.1 | K peak | parked peak | superseded scopes | last A item after release s | load1 max | B wait p50 / p95 s / claims per s |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| C-rep1 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 3.27 | 0.292 / 0.591 / 1.01 |
| C-rep2 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 26.03 | 0.255 / 0.848 / 1.01 |
| C-rep3 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 8.54 | 0.421 / 0.895 / 1.00 |
| CF-rep1 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 4.11 | 0.532 / 0.952 / 1.00 |
| C0-rep1 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 3.63 | 0.389 / 0.768 / 1.00 |
| C0-rep2 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 19.64 | 0.261 / 0.891 / 1.01 |
| C0-rep3 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 38.2 | 0.445 / 0.854 / 1.01 |
| T1-rep1 | 0s | 99.2 | 148.4 | 0.52 | 0 | 0 | 6 | 88.9 | 6.19 | 0.231 / 0.769 / 1.01 |
| T1-rep2 | 0s | 104.6 | 177.4 | 0.46 | 0 | 0 | 6 | 88.6 | 18.49 | 0.450 / 0.830 / 1.01 |
| T1-rep3 | 0s | 110.8 | 148.3 | 0.37 | 0 | 0 | 6 | 89.1 | 23.84 | 0.291 / 0.757 / 1.00 |
| T1TTL-rep1 | 5s | 0.6 | 115.6 | 0.94 | 0 | 0 | 6 | 84.7 | 5.55 | 0.350 / 0.829 / 1.00 |
| T1TTL-rep2 | 5s | 0.6 | 117.0 | 0.94 | 0 | 0 | 6 | 84.2 | 6.34 | 0.353 / 0.799 / 1.00 |
| T1TTL-rep3 | 5s | 0.6 | 99.8 | 0.95 | 0 | 0 | 6 | 85.0 | 5.49 | 0.519 / 0.882 / 1.01 |
| C2-rep1 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 2.48 | 0.457 / 0.882 / 1.13 |
| C2-rep2 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 4.36 | 0.570 / 0.946 / 1.13 |
| C2-rep3 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 4.77 | 0.526 / 0.926 / 1.13 |
| T2-rep1 | 0s | 120.3 | 170.0 | 0.20 | 0 | 0 | 0 | 87.3 | 9.51 | 0.304 / 1.038 / 1.13 |
| T2-rep2 | 0s | 119.2 | 160.0 | 0.22 | 0 | 0 | 0 | 87.2 | 4.81 | 0.303 / 1.027 / 1.12 |
| T2-rep3 | 0s | 115.2 | 160.0 | 0.31 | 0 | 0 | 0 | 87.9 | 5.9 | 0.286 / 1.281 / 1.12 |
| T3-rep1 | 0s | 172.2 | 410.9 | 0.00 | 6500 | 6500 | 0 | None | 40.6 | 0.385 / 0.932 / 1.01 |
| T3-rep2 | 0s | 296.1 | 479.6 | 0.00 | 6500 | 6500 | 0 | None | 3.74 | 0.399 / 0.782 / 1.01 |
| T3-rep3 | 0s | 219.5 | 455.5 | 0.00 | 6500 | 6500 | 0 | None | 5.43 | 0.315 / 0.780 / 1.00 |
| T3TTL-rep1 | 5s | 0.6 | 240.6 | 0.78 | 6500 | 6500 | 0 | None | 13.73 | 0.311 / 0.920 / 1.01 |
| T3TTL-rep2 | 5s | 0.6 | 237.7 | 0.78 | 6500 | 6500 | 0 | None | 4.07 | 0.283 / 0.853 / 1.01 |
| T3TTL-rep3 | 5s | 0.6 | 364.0 | 0.78 | 6500 | 6500 | 0 | None | 16.0 | 0.355 / 0.811 / 1.00 |
| CG-rep1 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 4 | None | 23.85 | 0.326 / 0.949 / 1.01 |

Consult figures are read from the engaged scrape of the product binaries' /metrics; B figures from the queue's own timestamps.

## Host load, rerun slot 1 and in-flight slot 3

Slot 1 (21:25 to 22:02Z, `loadavg-rerun-slot1.tsv`) and slot 3 (`loadavg-inflight-slot3.tsv`, per-minute peak 5.5). Start load1 of every rerun and in-flight rep is in its result; all are below the gate (start and run max under 15).

## Per run, rerun and in-flight (21 runs)

| Run | TTL | consult p50 ms | consult p95 ms | share in le=0.1 | K peak | parked peak | superseded scopes | last A item after release s | load1 max | B wait p50 / p95 s / claims per s |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| C2-rep1 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 4.0 | 0.599 / 0.937 / 1.13 |
| C2-rep2 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 6.16 | 0.465 / 1.028 / 1.12 |
| C2-rep3 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 3.26 | 0.534 / 0.924 / 1.13 |
| T2-rep1 | 0s | 121.1 | 170.0 | 0.18 | 0 | 0 | 0 | 86.4 | 7.98 | 0.314 / 0.998 / 1.13 |
| T2-rep2 | 0s | 127.7 | 223.3 | 0.11 | 0 | 0 | 0 | 87.6 | 3.94 | 0.290 / 2.276 / 1.12 |
| T2-rep3 | 0s | 118.6 | 160.0 | 0.24 | 0 | 0 | 0 | 86.9 | 4.87 | 0.295 / 1.219 / 1.13 |
| T2TTL-rep1 | 5s | 0.6 | 160.0 | 0.92 | 0 | 0 | 0 | 86.5 | 5.43 | 0.394 / 1.457 / 1.12 |
| T2TTL-rep2 | 5s | 0.6 | 160.0 | 0.93 | 0 | 0 | 0 | 85.7 | 10.06 | 0.428 / 0.919 / 1.13 |
| T2TTL-rep3 | 5s | 0.5 | 160.0 | 0.93 | 0 | 0 | 0 | 86.8 | 3.24 | 0.458 / 1.155 / 1.13 |
| IFC-rep1 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 3.62 | 0.303 / 0.694 / 1.00 |
| IFC-rep2 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 7.55 | 0.426 / 0.834 / 1.00 |
| IFC-rep3 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 4.17 | 0.380 / 0.646 / 1.00 |
| IF-rep1 | 0s | 98.1 | 144.8 | 0.52 | 0 | 0 | 0 | 22.6 | 5.5 | 0.394 / 0.885 / 1.00 |
| IF-rep2 | 0s | 101.0 | 145.1 | 0.49 | 0 | 0 | 0 | 21.6 | 5.13 | 0.431 / 0.973 / 1.01 |
| IF-rep3 | 0s | 106.1 | 145.6 | 0.43 | 0 | 0 | 0 | 22.0 | 2.59 | 0.451 / 0.969 / 1.00 |
| IWC-rep1 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 6.3 | 0.000 / 0.000 / 0.00 |
| IWC-rep2 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 3.92 | 0.000 / 0.000 / 0.00 |
| IWC-rep3 | 0s | 0.0 | 0.0 | 0.00 | 0 | 0 | 0 | None | 2.48 | 0.000 / 0.000 / 0.00 |
| IW-rep1 | 0s | 87.9 | 131.1 | 0.87 | 0 | 0 | 6 | 21.2 | 3.83 | 0.000 / 0.000 / 0.00 |
| IW-rep2 | 0s | 90.1 | 139.7 | 0.76 | 0 | 0 | 6 | 22.8 | 3.76 | 0.000 / 0.000 / 0.00 |
| IW-rep3 | 0s | 93.8 | 143.2 | 0.63 | 0 | 0 | 6 | 20.9 | 3.36 | 0.000 / 0.000 / 0.00 |

Consult figures are read from the engaged scrape of the product binaries' /metrics; B figures from the queue's own timestamps.

## T1 enablement gate: the quiet reps

```text
run | le0.1 count / n | share | p50 ms | p95 ms | start load1 (manifest) | run load1 min/median/max
T1-rep1 (matrix, quiet) | 143 / 277 | 0.516 | 99.2 | 148.4 | 2.25 | 1.9 / 5.2 / 6.2
T1-q1 (supplementary) | 157 / 277 | 0.567 | 97.1 | 151.1 | 3.63 | 2.9 / 4.1 / 5.2
T1-q2 (supplementary) | 133 / 277 | 0.480 | 101.9 | 145.8 | 4.11 | 1.6 / 2.9 / 5.2
T1-q3 (supplementary) | 143 / 277 | 0.516 | 99.2 | 145.5 | 1.63 | 1.0 / 1.3 / 1.7
T1-rep2 (matrix, loaded start) | 126 / 276 | 0.457 | 104.6 | 177.4 | 16.07 | 3.6 / 6.3 / 18.5
T1-rep3 (matrix, loaded start) | 102 / 276 | 0.370 | 110.8 | 148.3 | 23.84 | 2.2 / 5.4 / 23.8
pooled quiet rule S over the first four rows = 576/1108 = 0.5199
start loads of the three q reps in the launcher log supp.log: 3.08, 4.11, 1.51
```

The rule that fixed the reading (before the q reps ran): S = sum of le=0.1 counts over the quiet reps divided by their n; all three q >= 0.5 is met at the limit; a mix with S >= 0.5 and at least 2 of 3 q >= 0.5 is met with the count below; otherwise not met. S = 0.5199, 2 of 3 q reps at or above 0.5: met at the limit, p50 about 99 to 100 ms, no margin, one quiet rep (q2 0.480) below 0.5.

## Judged outcomes

The full checker output is in `matrix-report.txt`, `rerun-report.txt` and `inflight-report.txt`.

- Every treatment and control rep passes the AC4 checks: build identity, graph readings, queue settled, vacuity guards, latch engaged and released once, zero A writes while engaged with positive controls, zero dead letters and admission timeouts, attempt_count - source_paced_waits = 1, A items within the bound (maximum 89.1 s against 991 s), and final state by shape against C or CF (fact sets, intents, sha chain, graph and relationship counts, content identity, 144 = 144 content rows, committed generations).
- AC5 queue wait is INCONCLUSIVE (tolerance above 50% of the control mean) in every git-side arm: a measurement-design limit (1 s poll, 1 s feed). `per-item-b-wait.txt` is the per-item table the arbiter substituted: 12 hosted-B treatment runs 1,800 items max 1.91 s 11 above 1 s; 6 control runs 900 items max 1.90 s 7 above 1 s; per-run p50 mean 0.354 against 0.344 s, p95 0.829 against 0.808 s. The 10% clause was not demonstrated.
- T1 consult share within 100 ms: rep1 0.516 (quiet), reps 2 and 3 0.457 and 0.370 (loaded starts, reported not judged); see the gate above.
- T2 in the matrix: the fleet-summed engaged counter read 0.0 in all three reps (no process emitted the claim-pacing or `collector_kind` consult series). The fix (#11322) wires `Instruments`; the rerun passes counter 1.0, gauge series present and 0, claim-grant consult n 176 to 178 (p50 0.6 to 0.7 ms, p95 91.8 to 106.1 ms, kind `synth_zero_fact`).
- AC5 p95 in the rerun: T2 rep2 2.276 s and rep3 1.219 s FAIL, T2TTL rep1 1.457 s FAIL (C2 mean 0.963 s, tolerance 0.209 s); T2TTL reps 2 and 3 pass (0.919, 1.155). The registered prediction that T2TTL passes was not met. `per-worker-timeline.txt`, `per-item-b-wait.txt` (burst section) and `neo4j-plan-discards.txt` carry the mechanism: M1 one awake worker, M2 the paced wave sorts first, M3 a slow first graph write that coincides with Neo4j discarding about 100 cached plans (trigger by the pacing arms not shown).
- T3: consult p50 172, 296 and 220 ms at TTL 0 with 6,500 parked rows; `t3-explain-bounded-probe.txt` is the plan (162 to 163 ms, index-bounded by source, 13,192 candidate rows, planner estimate 167, four readiness subplans = about 52 ms).
- In-flight and warm-delta arms: 12 of 12 runs pass every check (`inflight-report.txt`).

## Files

| File | What it is |
| --- | --- |
| `matrix-report.txt`, `rerun-report.txt`, `inflight-report.txt` | Raw checker output (`report.sh` for the matrix; `check_arm.py` by hand for the C2/T2/T2TTL rerun root, which has no C control) |
| `per-item-b-wait.txt` | `b_item_table.py --burst` over the matrix and the rerun roots |
| `per-worker-timeline.txt` | `per_worker_timeline.py` for T2TTL rep1, T2 rep2 and C2 rep2 of the rerun |
| `neo4j-plan-discards.txt` | `neo4j_plan_discards.py` over both roots and a copy of the Neo4j `debug.log` |
| `t3-explain-bounded-probe.txt` | `explain_probe.sh` at the T3 shape (three warm runs) |
| `t1-quiet-reps.txt` | The pooled quiet rule over rep1 and the three q reps |
| `loadavg-*.tsv` | One host load sample a minute per slot |

## What the replay does not exercise

- No reducer ran. Release was driven by completing seeded rows through the production queue call.
- B's freshness is measured to the reducer-queue enqueue, not through reducer handlers.
- K and the parked counts come from the seed.
- T2 commits no hosted facts; pacing acts before the claim, so no partial commit exists to lose.
- The runs used `explicit` mode; the claims are scoped to explicit, githubOrg and gitlabGroup. The schedule leg is undefined for filesystem mode and startup refuses the pair (`TestWireSelectionBackpressureRefusesFilesystemMode`, `TestSelectRepositoriesFilesystemModeNeverDefersAnEditedRepository`). The first venue ran filesystem mode and was abandoned for that defect.
- The global admission marks were raised on the ingester in every arm except CG. The admission-writer leg and AC5 hold only while total reducer outstanding is below the ingester's high-water mark; the pre-write, claim-grant and selection legs do not depend on it.
- Seeded generations produce no reducer intents and no content rows, so the intent and content checks rest on the six real repositories.
- `fact_id` is compared through `stable_fact_key`; `payload.indexed_at` and `payload.source_run_id` are dropped.
- After release a paced projector item resumes only when its scheduled backoff expires, limited to min(retry cap, ReleaseAge) plus jitter per item. A source whose first generation is paced has no projected baseline, so each new commit is collected as a full generation and earlier ones are superseded unprojected.
- A matrix start at 2026-11-23T01:19Z was aborted before its first arm finished and discarded (`results-f7f5c72361-ABORTED-not-evidence`); no result from it was judged.
- Smoke figures are not evidence.
- Later `origin/main` commits (83 at the time of the matrix) are not in the run; the evidence commit's product code is `487a0c53bd`.
- The in-flight and warm-delta runs overlapped another lane's own work on its own Postgres (22:15 to 22:33Z, nice 19: a `-race` test, short live runs with a test compile, static gates, doc gates); load max 7.5 for any of those 12 runs, below the gate.
