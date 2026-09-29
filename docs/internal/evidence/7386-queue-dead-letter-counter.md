# #7386 Queue Dead-Letter Counter

A work item that a projector or reducer Fail path dead-letters had no counter:
nothing said how many died or why without reading rows.

## Change

`eshu_dp_queue_dead_letters_total` (`Instruments.QueueDeadLetters`, an Int64Counter)
counts each work item a Fail path moves to `dead_letter`. Labels: `queue`
(`projector` or `reducer`) and `failure_class`.

- It is recorded in `ProjectorQueue.Fail` and `ReducerQueue.failIntent`, on the
  dead-letter branch, immediately after the UPDATE reports exactly one affected
  row. A claim the lease fence rejected (zero rows) is not counted. The reducer's
  batch path reaches it through `failIntent`, and the container-image-identity
  variant of the statement shares the same branch.
- The `failure_class` label is the class stored on the row. It is bounded by
  `queuestore.BoundedFailureClassLabel`: a value that does not match
  `^[a-z0-9_]{1,64}$` is labeled `other`. The stored
  class is unchanged, and the message and details are never labels.
- Other queues that dead-letter (`semantic_extraction_jobs`, `collector_generation_dead_letters`) are not counted here; the metric is the projector and reducer Fail paths.
- Operator dead-letters through the admin API (`DeadLetterWorkItems`,
  `SkipRepositoryWorkItems`) are not a Fail path and are not counted.
- It is wired wherever the queue already carries `Instruments`: the reducer, the
  ingester and bootstrap-index projector queues, and the standalone projector.

## Proof

`queue_dead_letter_counter_test.go`, with an in-memory meter reader. With the
instrument registered and no emission, the two recording tests fail with no data
points.

| Test | Asserts |
| --- | --- |
| `TestProjectorFailRecordsDeadLetterByStoredClass` | a non-retryable projector Fail records one point, `queue=projector`, `failure_class` equal to the class written to Postgres |
| `TestProjectorFailRetryWithAttemptsLeftRecordsNoDeadLetter` | a retryable Fail with attempts left records nothing |
| `TestProjectorFailRejectedClaimRecordsNoDeadLetter` | zero affected rows returns `ErrProjectorClaimRejected` and records nothing |
| `TestProjectorFailSelfClassifyingCauseLabelsWithItsStoredClass` | a `GraphWriteTimeoutError` past its attempt budget is labeled `graph_write_timeout` |
| `TestProjectorFailLabelsAnUnboundedClassAsOther` | `Weird/Class:1`, an uppercase class, and a 65-character class are labeled `other` |
| `TestReducerFailIntentRecordsDeadLetterWithQueueReducer` | a reducer `failIntent` records `queue=reducer` with the stored class |
| `TestReducerFailIntentRejectedClaimRecordsNoDeadLetter` | zero affected rows records nothing |

The registration is asserted in `telemetry/instruments_test.go`. The public
reference row is in `reference/telemetry/metrics.md`, the counters table in
`telemetry/README.md`, and the stage row in `observability/telemetry-coverage.md`;
`verify-telemetry-coverage.sh` passes.

## Performance

No-Regression Evidence (#7386): the added work runs once per dead-lettered work
item, after the UPDATE, off the claim path, and adds no SQL. `BenchmarkRecordQueueDeadLetter`
(2,000,000 iterations, 3 runs, Apple M-series, the bounded-label check and one counter
`Add` as the two call sites do them): 510 to 906 ns depending on host load (three runs
each on two occasions; a reviewer's run read 510 to 529 ns) and 4 allocations (296 B) per
call. A dead-letter is a terminal outcome that already costs a Postgres round trip of
about a millisecond, so the counter adds under 0.1% of it. The nanosecond figures
move with load; the allocation counts do not.

Observability Evidence: this change is the signal. An operator at 3 AM reads
`sum by (queue, failure_class) (increase(eshu_dp_queue_dead_letters_total[15m]))`
for how many items died and why, then reads the rows of that `failure_class` for the
message and details. Cardinality is at most two queues by the classes the queues
store, plus `other`.
