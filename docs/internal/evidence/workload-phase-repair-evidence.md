# Workload Phase Repair Evidence

Issue #2905 protects workload-materialization phase publications with the same
repair-queue contract used by semantic materialization. The conflict domain is
the non-atomic boundary where workload graph writes, endpoint presence, repo
workload presence, and optional workload-dependency writes can commit before the
`workload_materialization` phase rows are published. The idempotency key is the
exact `GraphProjectionPhaseKey` plus phase captured in
`GraphProjectionPhaseRepair`; replay republishes the readiness row and does not
repeat graph writes.

No-Regression Evidence: `go test ./internal/reducer -run
'TestWorkloadMaterializationHandlerEnqueuesRepairWhen.*PublishFails' -count=1`
fails before `WorkloadMaterializationHandler` accepts a
`GraphProjectionPhaseRepairQueue`, then passes after failed intent-key and
repo-readiness phase publishes enqueue the exact missed phase rows. The
candidate-path test proves the first phase publish can succeed, the per-repo
readiness publish can fail, and only the repo-keyed readiness row is queued for
repair. Review follow-up added
`TestWorkloadMaterializationHandlerEnqueuesRepoReadinessRepairWhenIntentPublishFailsAfterGraphWrite`,
which failed while an intent-keyed publish failure queued only the intent repair
after graph writes committed, then passed after the same failure path also
queued the repo-keyed readiness repair that handles_route and runs_in consume.
`go test ./internal/reducer -run
TestGraphProjectionPhaseRepairerRunOnceRepublishesWorkloadReadinessWithoutAcceptanceRow
-count=1` fails before the repairer replays generation-scoped
`workload_materialization` service-readiness rows that intentionally cross the
code/workload source-run boundary, then passes after the replay path no longer
requires a matching shared-projection acceptance row for that exact key shape.

No-Observability-Change: the change adds no worker, lease, queue table,
runtime knob, metric instrument, metric label, span, route, or graph query. It
uses the existing `graph_projection_phase_repair` queue and existing
`GraphProjectionPhaseRepairer` side runner, so operators continue to diagnose
the path through reducer execution logs/counters, phase-repair queue depth,
`graph_projection_repair_publish_failed` logs, shared-projection blocked counts,
and Postgres query instrumentation.
