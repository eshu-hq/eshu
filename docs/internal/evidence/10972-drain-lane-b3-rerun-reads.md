# 10972 Lane-B3 drain re-measurement: R0 frozen, triplet VOID on validity (2026-11-27)

Refs eshu-hq/eshu#10972, part of #10860. Lane: Lane-B3 10972 drain rerun.
Plan: [10972-drain-measurement-amended-plan.md](10972-drain-measurement-amended-plan.md),
followed exactly. Read-only posture throughout
(`default_transaction_read_only=on`, 15s statement timeouts, kubectl against
the QA cluster); zero cluster mutations. Throwaway Go harness (production
probe construction, `NewNVDBaselineDrainProbe(postgres.SQLDB{DB: db})`, no
`WithReadTimeout`; `StatusStore.ReadStatusSnapshotFiltered` with empty
selection; `NVDBaselineCheckpointStore.ReadNVDBaselineCheckpoints`) removed
after the window; tree otherwise untouched.

## Summary

One frozen baseline read (R0) under a verified 60m restart-free primary, then
the rated triplet voided on validity before R1: the session's cluster
credential expired mid-window, so the plan-mandated per-read pod
UID/startTime/imageID verification became impossible and no rated R1/R2/R3
was started. Drain direction NOT CONFIRMED (no rated triplet). R0 stands as a
frozen baseline for a retry: same run, years, and key as Lane-B2 with
unchanged stamps 5.6h later. Verdict and numbers posted on #10972; this note
is the evidence record.

## T0 verification

- T0 = 2026-11-27T06:10:12Z (R0 freeze read start; read closed 06:10:27Z).
- PRIMARY `eshu-postgresql-0`, namespace `eshu`: pod UID
  `34ea10fb-e4fb-4a0e-959b-497d609b6980` (same instance Lane-B2 measured),
  podStart 2026-11-26T19:48:56Z. Containers `postgresql` (started
  2026-11-26T19:49:29Z) and `metrics` (started 2026-11-26T19:51:04Z):
  `restartCount` 0, empty `lastState` (no finish ever). Pod age at T0
  ~10.4h; trailing-60m restart-free floor satisfied with wide margin.
- Per-read primary proof on the read connection:
  `pg_is_in_recovery()=false`; `pg_postmaster_start_time()` =
  2026-11-26T19:49:30.242813Z (matches container start and Lane-B2 exactly).
- At 06:29:42Z the session cluster credential expired (SSO token lapsed
  02:32:34Z); per-read identity re-verification for R1 became impossible.
  The pre-established data path still answered at 06:30:00Z with identical
  postmaster start, but kubectl identity fields are plan-mandated, so no
  rated read was started after R0.

## Frozen at R0 (06:10:12Z--06:10:27Z)

- Key: run `qa-canary-20261119`, named years 2024/2025/2026 with checkpoint
  (`$1`,`$2`): 2024 `.../2024/1781679173557000`,
  `vulnerability_intelligence:b766ea05...69adcf`; 2025
  `.../2025/1735689600000000`, `vulnerability_intelligence:5be632f4...7f89d0`;
  2026 `.../2026/1781692064637000`,
  `vulnerability_intelligence:a66a54ca...c41e6f` (key prefixes identical to
  Lane-B2; drained/stuck stamps byte-identical, so no advance occurred).
- No-pair years 2002..2023 (no checkpoint rows): not probed by design.
- Literals: delta-t 20min, H 0.2, B 24h, canary = 2026 pair.
- Inflow floor: trailing-2h pre-R0 census (04:10:12Z--06:10:12Z) inflow
  events 4790 (fresh 93 + reopened 4697, overlap 0) = **2395.0/hr**.
- Mix bound: trailing-2h stratifier total 4697 = 2348.5/hr mean, bound
  **4697/hr**. Stratifier = reopened rows by domain (full GROUP BY):
  advisory_group_projection 1338, deployment_mapping 1211,
  code_import_repo_edge 784, workload_materialization 715,
  supply_chain_impact 634, source_local 14, service_catalog_correlation 1.
- R0 counters: pending 9001, retrying 4845, in_flight 23, succeeded 252154,
  dead_letter 3769, failed 0, total 279398.
- R0 phases: 2024 `drained` (drained_at 2026-11-26T17:47:23Z), 2025
  `drained` (2026-11-26T17:47:20Z), 2026 `projecting` (not_done_since
  2026-11-26T12:58:17Z).

## Rated reads (one frozen, triplet unrated)

| read | t_start (UTC) | pending | retrying | in_flight | succeeded | dead_letter | total | 2024/2025/2026 phase |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| R0 (freeze) | 06:10:12.917 | 9001 | 4845 | 23 | 252154 | 3769 | 279398 | drained/drained/projecting |
| R1* | NOT TAKEN | -- | -- | -- | -- | -- | -- | -- |
| R2* | NOT TAKEN | -- | -- | -- | -- | -- | -- | -- |
| R3* | NOT TAKEN | -- | -- | -- | -- | -- | -- | -- |

## VOID declaration (validity, stop, no rated triplet)

The triplet **VOIDS** before R1 on measurement validity, not on drain
numbers: with cluster access refused, the plan's per-read identity record
(UID/startTime/imageID) and live 60m floor recomputation cannot be produced,
and the plan forbids starting any R1/R2/R3 read until every field is
recorded. No rated interval exists, so D1/D2/D3 and the R2 rule have no
inputs; nothing is cherry-picked and nothing is forced. The surviving data
path (identical postmaster start at 06:30:00Z) corroborates continuity but
does not substitute for the mandated identity fields.

## Comparison against the Lane-B2 void

| read | t_start (UTC) | pending | retrying | succeeded | net/hr | inflow/hr | verdict |
| --- | --- | --- | --- | --- | --- | --- | --- |
| B2 R1* | 01:08:54 | 8871 | 5205 | 251877 | -- | -- | context |
| B2 R2* | 01:29:15 | 9067 | 5141 | 251695 | +389.3 | -147.5 | VOID (R2 rule + mix) |
| B3 R0 | 06:10:12 | 9001 | 4845 | 252154 | -- | floor 2395.0 | frozen, valid |

Storm regime pre-read: B2 trailing-2h inflow 1830.5/hr with Interval-1
reopen events at 6970/hr (stratifier 4775/hr over a 2721/hr bound). B3
trailing-2h inflow 2395.0/hr (reopened 4697); the B2 four-domain subset
inside this census is 2711 (1355.5/hr mean) versus B2's 2721 (1360.5/hr),
so the reopen level entering the window matches B2's storm, now with the
#11595 bound in force. Whether the bound holds an interval under the mix
bound remains unmeasured: no rated interval ran.

## Verdict

**Drain-direction NOT CONFIRMED.** No drain claim: the rated triplet voided
on validity before R1. R0 is a valid frozen baseline (key, years, floor
2395.0/hr, bound 4697/hr, H 0.2, B 24h, delta-t 20min) for a retry once
cluster access is restored. The canary (2026 pair) still reads `projecting`
with not_done_since 2026-11-26T12:58:17Z (~17.2h at R0).

## Context reads

None. R0 is the only read of the window; no off-cadence or unverified reads
were taken.
