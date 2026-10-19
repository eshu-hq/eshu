# #8625 batch-cancel drop windows — performance and observability evidence

No-Regression Evidence: record-at-drop for the two batch-cancel windows in
`runBatchConcurrent` (`go/internal/reducer/service_batch.go`): Site A
(mid-handoff `ctx.Done` in the claimer loop records the current item plus
all remaining unhanded items with `workerID: -1`) and Site B (worker
post-receive `ctx.Err` records the received item with its real worker ID).
Both sites reuse the pre-existing `recordBatchAckOutcome` path with
`ack_outcome_unknown`; leases still expire for reclaim.

- Baseline: `TestServiceBatchCanceledMidHandoffRecordsRemainder` RED on the
  pre-fix tree (cancelled remainder emitted no record — the silent drop).
- After: new test GREEN, `-race -count=50` green; whole
  `go/internal/reducer` package green; `go vet` and `gofmt` clean.
- Backend/version: in-process Go test only (`go1.26.9 linux/arm64`); no
  Neo4j/Postgres backend involved — both record sites run exclusively on
  already-cancelled shutdown paths, so there is no live-backend behavior
  to measure against.
- Input shape: deterministic staged batch over the worker limit blocking on
  the rendezvous `workCh` handoff, cancelled mid-handoff (Site A).
- Terminal counts: the current item plus every remaining unhanded item each
  emit exactly one `ack_outcome_unknown` record; nothing executes, nothing
  acks.
- Why safe: the two record calls execute only after `ctx.Done`/`ctx.Err`
  is already observed — the hot path (claim → handoff → execute) is
  instruction-identical when the run context is live. No locks added, no
  handoff reordering, no worker-count/batch-size/poll-interval/lease/queue
  change. No benchmark delta to measure: shutdown-path telemetry that
  replaces silence with a record.

Observability Evidence: the two drop windows now emit `ack_outcome_unknown`
records through the pre-existing `recordBatchAckOutcome` path (counter +
warn log + duration and queue-wait histograms) — Site A with `workerID: -1`
(never assigned; dropped between claim and handoff), Site B with the real
receiving worker ID. No new instruments, no new log shapes; claim telemetry
(`QueueClaimDuration`, `BatchClaimSize`) is recorded before either site and
unchanged.
