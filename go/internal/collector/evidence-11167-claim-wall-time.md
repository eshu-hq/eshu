# Evidence: finished_at-claimed_at is grant latency, not run wall time (#11167)

Live 2026-11-21: `workflow_claims` rows show 0.01-0.05s
`finished_at - claimed_at` deltas for attempts whose
`eshu_dp_workflow_claim_run_duration_seconds` samples run ~300s.

Cause: `processClaimed` (`claimed_service.go:250`) stamps
`ClaimMutation.ObservedAt` once at claim-start via `claimMutation`
(`claimed_service_mutation.go:19`); `withFailure`
(`claimed_service_validation.go:60-75`) never re-stamps, and every terminal
statement binds `finished_at = $1 = ObservedAt` on both the in-tree store
(`workflow_control_sql.go` complete/fail/reap paths,
`workflow_control_release_sql.go` release path, args in
`workflow_control_helpers.go`) and the claim-plane server
(`eshu-workflow/store/claim.go`, `claim_mutation_sql.go`). The 0.01-0.05s
delta is grant-to-`processClaimed`-entry latency.

The same frozen stamp overwrites `claim.updated_at` on every terminal
transition; `item.last_completed_at` is set only on the complete path
(`workflow_control_sql.go:212`), so it misreports (frozen grant-entry stamp)
only for completed rows. Neither measures wall time.

True per-attempt wall time: lower bound `heartbeat_at - claimed_at` per row
(20s heartbeat-tick granularity; runs shorter than one tick read 0 and are
indistinguishable from stillborn by columns alone); authoritative source
`eshu_dp_workflow_claim_run_duration_seconds` by (`collector_kind`,
`source_system`, `outcome`) (`recordClaimRunDuration`,
`claimed_service_run_metrics.go`; instrument def `telemetry/instruments.go`).

Reaper-expired rows (`status` expired/failed_terminal via lease_expired) are
exempt: `finished_at` = reaper `asOf` ~= lease expiry + reap cadence.

Live verification 2026-11-21 (read-only QA replica, counts/shapes only):
discriminator medians over sub-5s reported deltas show reported p50 0.018s
against heartbeat p50 300.0s on failed rows (completed rows heartbeat p50
0.1s); 100 full-wall misreports (`heartbeat_at - claimed_at` > 60s);
heartbeat-bound p50 0.1s / p95 300.0s; histogram mass near 300s where rows
show ~0s finished deltas.

No behavior change in this note; fix (re-stamp `ObservedAt` at terminal call
sites) is follow-up work.
