# Evidence: bootstrap-index survives projector claim conflicts (#7122)

## Defect

`drainProjectorWorkItem` returned every `workSource.Claim` error. The worker
loop treats any error other than `errProjectorDrained` and
`errProjectorItemFailed` as fatal: it appends the error, cancels the shared
context, and the whole one-shot run ends. `ProjectorQueue.Claim` returns an
error wrapping `failure.ErrWorkClaimConflict` after three conflicting attempts
(`40P01` deadlock or `40001` serialization failure). The statement has rolled
back and nothing changed, yet a single such conflict killed bootstrap-index.
The long-running `projector.Service` already survives it (#7108).

## Contract now

- `claimProjectorWork` (`bootstrap_projector_claim.go`) wraps `Claim`. On
  `ErrWorkClaimConflict` it logs `failure_class=projector_claim_conflict` with
  the worker id, waits `claimConflictWait` (500ms, the pipelined empty-queue
  poll interval; no new env var), and claims again.
- The wait selects on the context, so cancellation stops it and returns the
  context error. A conflict never cancels sibling workers.
- Every other Claim error stays fatal, as before.
- `wiring.go` sets `projectorQueue.Instruments`, so the queue emits
  `eshu_dp_queue_claim_conflict_retries_total{queue,failure_class}` for
  bootstrap-index as it does for the ingester and projector. Before this,
  bootstrap left `Instruments` nil and the counter never fired. Setting it also
  lets the queue emit its other instrument-gated signal
  (`ProjectorRetrySurge`) in bootstrap-index.

## Proof

Tests in `bootstrap_projector_claim_conflict_test.go`:

- `TestDrainProjectorWorkItemSurvivesClaimConflict`: first Claim conflicts,
  second returns work; the item is acked.
- `TestDrainProjectorSurvivesClaimConflictAcrossWorkers`: 1 and 4 workers, every
  worker sees conflicts, all six items are still acked.
- `TestDrainProjectorStillStopsOnNonConflictClaimError`: same contract as
  `TestServiceRunStillStopsOnNonConflictClaimError`.
- `TestDrainProjectorWorkItemClaimConflictWaitStopsOnContextCancel`: cancel
  stops the wait within half the wait interval and does not re-claim.
- `TestDrainProjectorWorkItemLogsClaimConflict`: `failure_class` and `worker_id`.
- `TestBuildBootstrapProjectorWiresQueueInstruments`.

No-Regression Evidence: the happy path is unchanged. `claimProjectorWork`
returns on the first Claim call when it succeeds, with no timer allocated and no
extra Claim round trip; the conflict branch runs only after a Claim error.
`go test ./cmd/bootstrap-index/... ./internal/projector/... -count=1` and
`go test -race ./cmd/bootstrap-index -count=1` pass, including the existing
drain, isolation, and pipelined tests.

Observability Evidence: a conflicting claim now logs
`failure_class=projector_claim_conflict` with `worker_id` and
`phase=projection`, and the queue counter
`eshu_dp_queue_claim_conflict_retries_total{queue=projector,failure_class}`
increments per retried attempt. An operator sees a burst of bootstrap claim
conflicts on the metric and joins the exhausted-retry log lines by
`failure_class`, instead of a bootstrap-index exit with no queue signal.
