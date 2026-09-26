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
  the worker id and the consecutive-conflict count, waits `claimConflictWait`
  (500ms, the pipelined empty-queue poll interval; no new env var), and claims
  again.
- The retry is bounded by `maxConsecutiveClaimConflicts` (20). Each Claim call
  already makes 3 statement attempts inside the queue, so 20 consecutive
  conflicts is 60 conflicting statements plus at least 19 waits of 500ms
  (9.5s): far past a transient blip. On the 20th consecutive conflict
  `claimProjectorWork` returns a fatal error wrapping `errClaimConflictsExhausted`
  and the last `ErrWorkClaimConflict` and naming the count, and `drainProjector`
  fails the run. A successful or drained claim resets the count. bootstrap-index
  is a one-shot: a persistent conflict (a real bug rather than a deadlock blip)
  must fail loudly, not hang. This is stricter than `projector.Service`, a
  daemon that polls until shutdown.
- `eshu_dp_queue_claim_duration_seconds` is recorded per Claim call, so the wait
  and repeated calls of a conflicted item are not folded into it.
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

Tests in `bootstrap_projector_claim_cap_test.go`:

- `TestClaimProjectorWorkFailsAfterConsecutiveConflictCap`: a source that
  conflicts forever ends with `errClaimConflictsExhausted` wrapping the last
  conflict and naming the count after exactly N Claim calls. Before the cap it
  hung; the test's 5s guard failed it after 4047 Claim calls.
- `TestClaimProjectorWorkConflictCounterResetsOnSuccess` and
  `...ResetsOnDrained`: N-1 conflicts followed by work or a drained result do
  not fail, and the count does not carry into the next call.
- `TestClaimProjectorWorkCapErrorIsFatalToDrainProjector`: with the real
  constants, `drainProjector` fails after `maxConsecutiveClaimConflicts` Claim
  calls (skipped under `-short`; about 9.5s).
- `TestClaimProjectorWorkClaimDurationExcludesConflictWait`: three Claim calls
  produce three histogram samples whose sum stays under half of a 200ms wait.
- `TestDrainProjectorPipelinedSurvivesClaimConflictAfterCollectorDone`: the
  pipelined path (`drainingWorkSource`) survives conflicts after the collector
  finishes; a conflict is not an empty poll, so Claim is called exactly 8 times
  (2 conflicts, 1 item, 5 empty polls) and the item is acked.

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
