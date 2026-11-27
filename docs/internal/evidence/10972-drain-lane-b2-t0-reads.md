# 10972 Lane-B2 drain reads: T0 verification, rated triplet, VOID (2026-11-27)

Refs eshu-hq/eshu#10972, part of #10860. Lane: Lane-B2 10972 T0 reads.
Plan: [10972-drain-measurement-amended-plan.md](10972-drain-measurement-amended-plan.md),
followed exactly. Read-only posture throughout
(`default_transaction_read_only=on`, 15s statement timeouts, kubectl ops-qa);
zero cluster mutations. Throwaway Go harness (production probe construction,
`NewNVDBaselineDrainProbe(postgres.SQLDB{DB: db})`, no `WithReadTimeout`;
`StatusStore.ReadStatusSnapshotFiltered` with empty selection;
`NVDBaselineCheckpointStore.ReadNVDBaselineCheckpoints`) deleted after the
window; tree otherwise untouched.

## T0 verification

- T0 = 2026-11-27T00:30:16Z (R0 freeze read).
- PRIMARY `eshu-postgresql-0`, namespace `eshu`: pod UID
  `34ea10fb-e4fb-4a0e-959b-497d609b6980`, podStart 2026-11-26T19:48:56Z.
  Containers `postgresql` (started 2026-11-26T19:49:29Z) and `metrics`
  (started 2026-11-26T19:51:04Z): `restartCount` 0, empty `lastState` (no
  finish ever on this pod instance). Pod age at T0 ~4.7h; trailing-60m
  restart-free floor satisfied with wide margin.
- Per-read identity re-verified at 00:30 (R0), 00:48 (R1), 00:57, 01:11:22,
  01:28:54 (R2*): same UID, same container `startedAt`, 0 restarts every
  time. Floor never broke.
- Per-read primary proof on the read connection:
  `pg_is_in_recovery()=false`; `pg_postmaster_start_time()` =
  2026-11-26T19:49:30.242813Z every read (matches container start).

## Frozen at R0 (00:30:34Z--00:30:45Z)

- Key: run `qa-canary-20261119`, named years 2024/2025/2026 with checkpoint
  (`$1`,`$2`): 2024 `.../2024/1781679173557000`,
  `vulnerability_intelligence:b766ea05...69adcf`; 2025
  `.../2025/1735689600000000`, `vulnerability_intelligence:5be632f4...7f89d0`;
  2026 `.../2026/1781692064637000`,
  `vulnerability_intelligence:a66a54ca...c41e6f` (full IDs in the read
  logs; programmatic key-equality True across every rated read; drained/stuck
  stamps byte-identical across all reads, so no advance occurred).
- No-pair years 2002..2023 (no checkpoint rows): not probed by design.
- Literals: delta-t 20min, H 0.2, B 24h, canary = 2026 pair.
- Inflow floor: trailing-2h pre-R0 census (22:30:16Z--00:30:16Z) inflow
  events 3661 (fresh 112 + reopened 3549, overlap 0) = **1830.5/hr**.
- Mix bound: trailing-2h stratifier reopens 2721 (deployment_mapping 1229,
  code_import_repo_edge 784, workload_materialization 707,
  service_catalog_correlation 1; other 4 domains 0) = 1360.5/hr mean, bound
  **2721/hr**.
- R0 counters: pending 9605, retrying 5140, in_flight 25, succeeded 251209,
  dead_letter 3776, failed 0, total 279414.
- R0 phases: 2024 `drained` (drained_at 2026-11-26T17:47:23Z), 2025
  `drained` (2026-11-26T17:47:20Z), 2026 `projecting` (not_done_since
  2026-11-26T12:58:17Z).

## Rated triplet (consecutive at frozen cadence, zero error classes)

| read | t_start (UTC) | pending | retrying | in_flight | succeeded | dead_letter | total | 2024/2025/2026 phase |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| R1* | 01:08:54.789 | 8871 | 5205 | 26 | 251877 | 3773 | 279381 | drained/drained/projecting |
| R2* | 01:29:15.354 | 9067 | 5141 | 19 | 251695 | 3841 | 279378 | drained/drained/projecting |
| R3* | NOT TAKEN | -- | -- | -- | -- | -- | -- | -- |

Interval 1 delta-t (actual timestamps): 1220.565s = 0.339046h.

- outflow/hr = (251695-251877)/0.339046 = **-536.8/hr**
- net/hr = ((9067+5141)-(8871+5205))/0.339046 = +132/0.339046 = **+389.3/hr**
- inflow/hr (derived) = -536.8+389.3 = **-147.5/hr**
- D1 (outflow > inflow): -536.8 > -147.5 = **FALSE**
- D2 (for the record): (-536.8-(-147.5))/max(1830.5,-147.5) = **-0.213 < 0.2**,
  fail.
- Interval-1 census [01:08:54,01:29:15]: fresh 26 (77/hr), reopened_any
  2363 (6970/hr), inflow events 2389 (7046/hr); stratifier 1619
  (code_import_repo_edge 732, deployment_mapping 497,
  workload_materialization 389, service_catalog_correlation 1) =
  **4775/hr > mix bound 2721/hr**.

## VOID declaration (stop, no R3*, no restart)

The triplet **VOIDS** at Interval 1 on two independent grounds:

1. **R2 rule**: R1* to R2* does not satisfy D1 (outflow -536.8/hr is not
   greater than inflow -147.5/hr), so the triplet voids instead of
   proceeding. The negative derived rates are reopen churn, not measurement
   error: the succeeded stock fell by 182 while the open backlog grew by
   132, i.e. succeeded-to-pending reopens outpaced completions; dead_letter
   rose by 68 in the same interval.
2. **Mix bound**: stratifier reopen rate 4775/hr exceeds the frozen bound
   2721/hr (1.75x).

Per the lane order, stop on void: R3* not taken, no replacement triplet
forced here. Floor intact (0 restarts throughout), keys intact (programmatic
equality, stamps unchanged), phases returned verbatim (no timeout/not-found
classes on any read). Reopen rate escalated across the window (trailing-2h
1774/hr; R0-R1 gap ~4300/hr; Interval 1 6970/hr events) while fresh intake
stayed near-dead (77/hr in Interval 1).

## Verdict

**Drain-direction NOT CONFIRMED.** No drain claim: the rated triplet voided
at Interval 1, and the open backlog grew (+132) under a reopen storm. The
canary (2026 pair) reads `projecting` with
not_done_since 2026-11-26T12:58:17Z (~12.5h at R2*); 2024/2025 read
`drained` with unchanged stamps.

## Context reads (unrated, documented, never edited)

TRIAL 00:29, R1 00:51 (p 9334, r 5134, s 251470), R1b 00:57, R2x 01:03,
mid 01:14:58 -- off-cadence scheduling mistimings by this lane (early wakes),
kept as context only. Gap censuses: R0-R1 1520 events/1086 stratifier;
R1-R1b 381/65; R1b-R2x 417/87. Infra notes: two port-forward drops from a
`head`-pipe SIGPIPE in the lane's own command (no read reached the DB, so no
voids); fixed by redirecting to a file, stable for 5+ connections and all
subsequent reads.
