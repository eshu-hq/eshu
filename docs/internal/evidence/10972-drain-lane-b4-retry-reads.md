# 10972 Lane-B4 drain retry: rated triplet, VOID at Interval 1 (2026-11-27)

Refs eshu-hq/eshu#10972, part of #10860. Lane: Lane-B4 10972 drain retry.
Plan: [10972-drain-measurement-amended-plan.md](10972-drain-measurement-amended-plan.md),
followed exactly. Read-only posture throughout
(`default_transaction_read_only=on`, 15s session statement timeout, port-forward
to the PRIMARY pod, production probe construction
`NewNVDBaselineDrainProbe(postgres.SQLDB{DB: db})` with no `WithReadTimeout`,
`StatusStore.ReadStatusSnapshotFiltered` with an empty selection,
`NVDBaselineCheckpointStore.ReadNVDBaselineCheckpoints`); zero cluster
mutations, zero deploys. Throwaway Go harness (single file, deleted after the
window); tree otherwise untouched. No commit, no push, no pre-push/pre-pr
(coordinator-only).

## Summary

We ran a rated drain-measurement triplet against the frozen baseline to test
whether the reducer queue is draining on QA. The triplet voided at the first
interval because reopen churn outpaced completions, so drain direction stays
NOT CONFIRMED and no new triplet should run until the fan-out reopen churn is
dispositioned.

## Frozen R0 (from Lane-B3, per lane order)

- T0 = 2026-11-27T06:10:12Z on PRIMARY `eshu-postgresql-0`.
- Run `qa-canary-20261119`, named years 2024/2025/2026.
- Inflow floor 2395.0/hr, mix bound 4697/hr, H 0.2, B 24h, delta-t 20min.
- UNRESOLVED: the Lane-B3 baseline note is present in neither this worktree
  nor the main checkout, so the frozen R0 key IDs ($1/$2 per year) and R0
  counters could not be compared directly. Key-equality fail-closed is proven
  for the measured window instead: checkpoint ($1,$2) byte-identical R1<->R2
  (full IDs below), and drained/not-done stamps byte-identical R1<->R2, so no
  checkpoint advance occurred inside the triplet. The IDs read here also match
  Lane-B2's recorded frozen key exactly.

## Per-read verification (all reads against PRIMARY eshu-postgresql-0)

Pod UID `34ea10fb-e4fb-4a0e-959b-497d609b6980` (same instance as Lane-B2),
podStart 2026-11-26T19:48:56Z; containers `postgresql` (started
2026-11-26T19:49:29Z) and `metrics` (started 2026-11-26T19:51:04Z):
`restartCount` 0, empty `lastState` (no finish ever on this instance).
Pod age at R1 ~23.5h; trailing-60m restart-free floor satisfied with wide
margin, recomputed live per read.

| read | t_start (UTC) | UID match | restarts | per-read primary proof |
| --- | --- | --- | --- | --- |
| R1 | 19:20:41.671656Z | yes | 0/0 | in_recovery=false, postmaster 2026-11-26T19:49:30.242813Z |
| R2 | 19:40:47.066030Z | yes | 0/0 | in_recovery=false, postmaster 2026-11-26T19:49:30.242813Z |
| R3 | NOT TAKEN (stop on void) | -- | -- | -- |

`pg_postmaster_start_time()` matches the container start every read.
`active_work_mode` served `live`, `ActiveWorkSource.Stale=false` on both
rated reads. One discarded pre-R1 harness attempt (pool deadlock from a held
connection, `list scope counts: context deadline exceeded`) produced no rated
data and no writes; the harness was fixed (single sequential pooled
connection) and R1 re-taken cleanly.

## Frozen key and phases (verbatim probe output)

Checkpoints for run `qa-canary-20261119` (identical R1 and R2):

- 2024: scope `vuln-intel://nvd/_baseline/qa-canary-20261119/2024/1781679173557000`,
  gen `vulnerability_intelligence:b766ea057cd7a90c61ed68bfb94229134ee0e2febd01789f85ef6d6f3d69adcf`
- 2025: scope `vuln-intel://nvd/_baseline/qa-canary-20261119/2025/1735689600000000`,
  gen `vulnerability_intelligence:5be632f4985e2c7c71d463ec8db902823655d07698a34967ba083e8ebe7f89d0`
- 2026: scope `vuln-intel://nvd/_baseline/qa-canary-20261119/2026/1781692064637000`,
  gen `vulnerability_intelligence:a66a54caf30cf5e79a5be327114bdd4d59c584774b48d01f9c75cd61ddc41e6f`

Phases verbatim (no timeout/not-found classes on any read):

| read | 2024 | 2025 | 2026 (canary) |
| --- | --- | --- | --- |
| R1 | drained (drained_at 2026-11-26T17:47:23.834371Z) | drained (drained_at 2026-11-26T17:47:20.225046Z) | projecting (not_done_since 2026-11-26T12:58:17.145093Z) |
| R2 | drained (same stamp) | drained (same stamp) | projecting (same stamp) |

## Rated triplet

| read | t_start (UTC) | pending | retrying | in_flight | succeeded | dead_letter | total |
| --- | --- | --- | --- | --- | --- | --- | --- |
| R1 | 19:20:41.672 | 7945 | 4230 | 26 | 254098 | 3465 | 279420 |
| R2 | 19:40:47.066 | 8073 | 4207 | 24 | 254014 | 3429 | 279406 |
| R3 | NOT TAKEN | -- | -- | -- | -- | -- | -- |

Interval 1 delta-t (actual timestamps): 1205.394374s = 0.3348318h.

- outflow/hr = (254014-254098)/0.3348318 = **-250.9/hr**
- net/hr = ((8073+4207)-(7945+4230))/0.3348318 = +105/0.3348318 = **+313.6/hr**
- inflow/hr (derived) = -250.9+313.6 = **+62.7/hr**
- D1 (outflow > inflow): -250.9 > 62.7 = **FALSE**
- D2 (for the record): (-250.9-62.7)/max(2395.0,62.7) = **-0.131 < 0.2**,
  fail.
- Interval-1 census [19:20:41.672Z,19:40:47.066Z]: fresh 5 (14.9/hr),
  reopened_any 723 (2159.4/hr), overlap 0, inflow events 728
  (2174.3/hr). Stratifier: reducer/advisory_group_projection 661,
  reducer/supply_chain_impact 62 = **2159.4/hr < mix bound 4697/hr**:
  the mix bound HOLDS this interval (0.46x).
- Absolute open backlog (pending+retrying): R1 12175 -> R2 12280 (+105).

## VOID declaration (stop, no R3, no restart)

The triplet **VOIDS** at Interval 1 on the R2 rule: R1->R2 does not satisfy
D1 (outflow -250.9/hr is not greater than inflow +62.7/hr), so the triplet
voids instead of proceeding. The negative outflow is reopen churn, not
measurement error: the succeeded stock fell by 84 while the open backlog grew
by 105, i.e. succeeded-to-pending reopens outpaced completions; dead_letter
fell by 36 in the same interval (replay/reopen of dead-lettered items).
Fresh intake stayed near-dead (14.9/hr). Total fact_work_items drifted
279420->279406 (-14 against +5 fresh arrivals): 19 rows left the table in
the interval (observed, unexplained, small; compatible with expiry of closed
items, which are not a durable record).

The canary (2026 pair) reads `projecting` with
not_done_since 2026-11-26T12:58:17Z (~30.7h at R2); 2024/2025 read `drained`
with unchanged stamps. No D3 slope projection: R3 was not taken per
stop-on-void, so there is no R2->R3 slope to project.

Context: the re-mark storm #11598 remains the suspected churn source
(advisory_group_projection reopens dominate this window at 661/723 = 91%,
vs the deployment_mapping/code_import_repo_edge mix Lane-B2 saw); storm
bound #11595 noted in force. Mix bound held this window, so the void rests
solely on the R2-rule direction failure.

## Verdict

**Drain-direction NOT CONFIRMED.** No drain claim: the rated triplet voided
at Interval 1 on the R2 rule, and the open backlog grew (+105) under
continued reopen churn. Floor intact (0 restarts throughout), keys intact
(byte-identical R1<->R2, stamps unchanged), phases returned verbatim (no
error classes on any rated read).

## Method note (query provenance)

Queue counters come from the production `activeWorkSummaryQuery` queue
section (`activeWorkSummaryQueueSelect`,
`go/internal/storage/postgres/status_active_work_summary.go:118`, sharing
`queueSnapshotLiveCounts`/`queueSnapshotTail` with `queueSnapshotSelect` in
`go/internal/storage/postgres/status_queries.go:172-211`), surfaced as
`QueueSnapshot` via `StatusStore.ReadStatusSnapshotFiltered` with an empty
selection. Drain phases come from verbatim `NVDBaselineDrainProbeQuery` legs
(`.../vulnerability/nvd_baseline_drain_probe.go:94-170`) via
`ReadNVDBaselineDrain` (`:301`, phase switch `:352-383`), read-only
transaction, default 3s probe timeout. Census definitions (this lane):
fresh = `created_at` in window, reopened = `reopened_at` in window,
stratifier = reopens grouped by (stage, domain). Every rated statement ran
under `statement_timeout='15s'` on the primary connection.
