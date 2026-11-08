# Poison unknown-trigger run wedges run reconciliation (#10369)

## Defect

One `workflow_runs` row with a trigger kind outside the closed
`workflow.TriggerKind` set aborted the whole `ReconcileWorkflowRuns` pass:
the list query orders by `updated_at ASC`, so the poison row sorted first,
`ReconcileRunProgress` failed its `Run.Validate` on that row, and the loop
returned zero reconciled with the error. Every later run stayed open, and
the same error at startup (`service.go` initial reconciliation) exited
`Service.Run`, crash-looping the coordinator. Production evidence in
#9879 (diagnosis 23:55Z) and #10001 (repair 00:31Z): one probe-residue row
with trigger kind `synthetic-test-10001`, 29 then 45 stuck open runs, 52
coordinator restarts, KEV lag past 3h against its 1h interval.

## Fix

`ReconcileWorkflowRuns` pre-validates `run.TriggerKind` at the top of the
loop. An unknown trigger is skipped, never aborted on: the skip is counted
on the unlabeled `eshu_dp_workflow_run_unknown_trigger_skips_total`
counter and WARN-logged (`workflow run skipped: unknown workflow trigger
kind`, with `run_id` and `trigger_kind`), the row stays open for a later
repair, and the pass keeps reconciling healthy runs. Any other per-run
error still aborts the pass with zero reconciled, exactly as before. The
list, progress, phase-count, and dead-letter SQL is byte-identical, so
query plans and row counts for healthy runs are unchanged; a skipped row
issues zero statements. Signal emitters moved to
`run_reconciliation_signals.go` only to honor the 500-line file cap.

## Evidence

No-Regression Evidence: seeded RED reproduction fails before the fix with
the exact production error, and the same seeds pass after it.

- Baseline (before): `TestReconcileSkipsUnknownTriggerRunAndCompletesHealthyRuns`
  and `TestReconcileWithOnlyUnknownTriggerRunsReturnsNoError` fail with
  `reconcile workflow run run-poison: unknown workflow trigger kind
  "synthetic-test-10001"`, zero reconciled — the production abort, verbatim.
- Input shape: two open runs, poison first by `updated_at` (trigger
  `synthetic-test-10001`, `collection_pending`) plus a healthy AWS
  schedule run (`reducer_converging`, 19/19 work items completed); and a
  poison-only pass.
- After (unit fake, no live Postgres in this lane):
  `go test ./internal/storage/postgres/ -run
  'TestReconcileSkipsUnknownTriggerRunAndCompletesHealthyRuns|TestReconcileWithOnlyUnknownTriggerRunsReturnsNoError|TestReconcileCountsDeadLetterBlockOnlyWhenTheRunTerminates|TestWorkflowControlStoreReconcileWorkflowRuns' -count=1`
  passes; the mixed pass reconciles exactly 1 run with exactly 1 exec (the
  healthy run's `UPDATE workflow_runs ... SET status='complete'`), the
  poison-only pass returns `(0, nil)` with 0 execs, and the skip counter
  reads exactly 1 in both cases via a real `telemetry.NewInstruments`
  meter with a manual reader.
- Full suites after: `go test ./internal/storage/postgres/ -count=1`
  passes except `TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline`,
  which fails identically on the clean `origin/main` base (missing
  `ESHU_DEPLOY_CHECKOUT`, environmental, unrelated);
  `./internal/coordinator/`, `./internal/telemetry/`, and
  `./internal/workflow/` all pass, including the new
  `TestServiceRunActiveModeCompletesInitialReconciliationWhenStoreSkipsPoisonRuns`
  startup-boundary test. `go vet` clean on all four packages.
- Why safe: the added check is a pure string-switch `Validate`, O(1) per
  open run per pass, outside any transaction; no new lock, retry, batch, or
  ordering change; genuine per-run failures keep the old abort-with-zero
  behavior, so no failure is swallowed.

Observability Evidence: the new counter
`eshu_dp_workflow_run_unknown_trigger_skips_total` is registered in
`go/internal/telemetry/instruments.go` with no labels (the trigger value is
unbounded storage data and must never be a label; `run_id` and
`trigger_kind` ride on the WARN log instead), documented in
`docs/public/observability/telemetry-coverage-runtime-surfaces.md`, and
emitted from `recordUnknownTriggerRun` in
`go/internal/storage/postgres/run_reconciliation_signals.go`. Test output
shows the WARN line per skipped row, and
`ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash
scripts/verify-telemetry-coverage.sh` passes. Operator rule: a nonzero rate
on this counter means poison rows are present — healthy runs keep
completing, but repair or remove the offending `workflow_runs` rows.
