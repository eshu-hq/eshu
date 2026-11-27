# 10972: amended drain-measurement plan (arbiter AMEND ruling, 2026-11-26)

Refs eshu-hq/eshu#10972, part of #10860. This is the plan text the AMEND
ruling requires BEFORE T0: entry point, named counters, floor, bars, primary
host, frozen key and years, per-read verification. No read in the R1/R2/R3
triplet starts until every field below is frozen and recorded.

Status at draft time: T0 NOT met. Last known primary state 18:02Z is
eshu-postgresql-0 at 25 restarts and climbing (23 restarts at ~17:22Z, last
finish 17:40:17Z), so the 60m restart-free floor is not satisfied; live
re-verification is NOT_CHECKED this turn (read-only cluster access refused:
expired SSO). Valid reads remain BLOCKED.

## 1. Phase source (ruling defect 1)

Phase comes ONLY from the Go entry point
`NVDBaselineDrainProbe.ReadNVDBaselineDrain`
(go/internal/storage/postgres/vulnerability/nvd_baseline_drain_probe.go:301;
phase switch :352-383, `drained` default :382). Raw SQL plus human
re-derivation is forbidden. Each read records the returned
`NVDBaselineDrainPhase` verbatim.

The hold label comes from planner telemetry, using the full closed 11-reason
set `nvdBaselineHoldReasons`
(go/internal/coordinator/vulnerability/nvd_baseline_planner.go:80-92). No
forced-fit mapping onto fewer buckets.

## 2. Named counters and numeric bars (ruling defect 2)

Absolute counters, read on the PRIMARY per read, from the queue snapshot
section (`activeWorkSummaryQueueSelect`,
go/internal/storage/postgres/status_active_work_summary.go:118; surfaced as
`QueueSnapshot`): `pending_count`, `retrying_count`, `claimed`/`in_flight`,
`succeeded_count`, `dead_letter_count`, `total_count`. Handler-side
corroboration only: `eshu_dp_reducer_executions_total`
(go/internal/telemetry/instruments.go:4174).

Per interval (Ri to Ri+1 over fixed Δt):

- `outflow/hr` = (succeeded_Ri+1 − succeeded_Ri) / Δt.
- `net/hr` = ((pending + retrying)_Ri+1 − (pending + retrying)_Ri) / Δt.
- `inflow/hr` = outflow/hr + net/hr (inflow is derived, never eyeballed).
- `absolute pending Ri` recorded for R1, R2, R3.

Bars (inequalities fixed now; thresholds H, B frozen at R0):

- D1 direction: outflow/hr > inflow/hr on BOTH R1→R2 and R2→R3.
- D2 headroom: (outflow − inflow) / max(inflow_floor, inflow) ≥ H per
  interval, H proposed 0.2, ratified at freeze.
- D3 bounded: canary phase == `drained` at R3, or the R2→R3 slope projects
  backlog exhaustion within B hours, B frozen at R0.
- Inflow floor: trailing-2h pre-R0 census inflow/hr, frozen at R0; D2
  references it so a quiet window cannot fake headroom.
- R2 rule: R1→R2 must already satisfy D1, else the triplet voids and
  restarts at a new R1. No cherry-pick, no post-hoc interval shopping.
- Reopen stratifier: per-interval reopen counts by domain
  (deployment_mapping, code_import_repo_edge, cross-scope correlation) ride
  alongside. The ~356/hr reopen stream voids a triplet ONLY by exceeding the
  pre-committed mix bound frozen at R0 (2× trailing-2h mean reopen/hr).

## 3. Window and T0 (ruling defect 3)

T0 is the first instant with a trailing-60m restart-free primary:
`restartCount` of eshu-postgresql-0 unchanged AND no container finish within
the last 60m. Effective floor is 60m after the last restart, recomputed live.

Sequence: R0 baseline read (fixes inflow floor, H, B, mix bound, frozen key
and years) → T0 declared → R1/R2/R3 consecutive at Δt (proposed 20min, fixed
at freeze) → verdict. Three consecutive valid reads, zero voids; any void
restarts the triplet, never edits it.

## 4. Primary host and per-read verification (ruling defects 3-4)

All reads run against PRIMARY `eshu-postgresql-0`. Replica reads are
inadmissible as primary proof. No post-hoc lag gate. Each read records:

- Pod UID, container `startTime`, container `imageID` of eshu-postgresql-0.
- `SELECT pg_is_in_recovery()` == false on the read connection (the
  primary proof, taken at read time).
- Statement timeout respected; read-only transaction (the probe sets both
  itself); client-side 15s guard on the identity checks.

Frozen key (immutable R0→R3): `$1` = checkpoint last scope id, `$2` = last
generation id per `NVDBaselineDrainProbeQuery` (:94-99), plus the named
baseline years carrying committed chunks at R0:

| Field | R0 value |
|---|---|
| $1 scope | TBD at R0 |
| $2 generation | TBD at R0 |
| named years | TBD at R0 |
| Δt | TBD at R0 (proposed 20min) |
| inflow floor | TBD at R0 |
| H / B / mix bound | TBD at R0 |

Any checkpoint advance ($2 changes) between R0 and R3 voids the triplet
fail-closed: a moved key measures a different chunk.
