# #8007 superseded-claim batch telemetry: regression and observability note

When a superseded claim and its successor for one work item land in the same
ack batch, `AckBatch` commits the successor yet rejects the whole batch. The
acker mapped that one error onto every item, so the committed successor was
logged as WARN `ack_outcome_unknown` with an empty result. The queue row was
already correct (`succeeded`, `attempt_count = 2`); only the telemetry lied.

The fix names the dropped claims: the split keeps the superseded claim
identities, `AckBatch` returns `reducer.BatchAckSupersededError` (which still
unwraps to `ErrExecutionClaimRejected`, so the batch acker survives instead
of stopping), and the acker records each dropped claim as `ack_claim_rejected`
with its work item id and each committed survivor as `succeeded` with its
result. A rejection with no supersession keeps the bare error and the previous
all-unknown telemetry, and survivors that may not have committed stay unknown,
so no path reports a success the queue did not write.

No-Regression Evidence (#8007): baseline `origin/main` 97994911b8, changed
head 2e405a89cd. No SQL text changed: the same ack UPDATE statements run over
the same surviving claims in the same order, and the split keeps the same
newest claim it kept before. No goroutine, lock, lease, batch-size, or retry
change: the only new work per batch is one slice append per dropped duplicate
claim and, on the rare lease-race batch, one WARN log line per dropped claim.
`go test ./internal/reducer/ ./internal/storage/postgres/` passes, including
the new failing-first tests and every pre-existing ack/reclaim test with
unchanged expectations. On disposable PostgreSQL 16.15, the #6162 live fixture
passes in both claim orders with the row ending `succeeded` and the lease
cleared, and `TestReducerQueueExpiryInterleaveLive` passes with the stale ACK
leaving the successor row and `cross_scope_completion_events` unchanged. A
throwaway end-to-end replay (real queue plus real Service flush over a
two-claim batch, deleted before commit) recorded `{succeeded: 1,
ack_claim_rejected: 1}` with the row `succeeded` and no `ack_outcome_unknown`.

Observability Evidence (#8007): the committed successor now emits the
`succeeded` record (`reducer execution succeeded` log plus
`eshu_dp_reducer_executions_total{status="succeeded"}`) with its handler
result, and each superseded claim emits its own `ack_claim_rejected` record
(`reducer ack rejected stale claim` WARN carrying the work item id). The
batch fence line (`reducer batch ack rejected stale claim` with `batch_size`)
is kept exactly for batches with unattributed items. The operator reference
(`docs/public/reference/telemetry/metrics-reducer-storage.md`) names the
changed statuses, and fault cell 3 now requires exactly one `succeeded`
record, with the contract proven by
`scripts/test-ifa-fault-lease-expiry-barrier.sh` (13/13 cases, including the
flipped `{ack_outcome_unknown: 2}` case).
