# Evidence: shared-projection partition-failure log (#5555)

## What changed

`SharedProjectionRunner.processPartitionWithTelemetry` now emits an
`ErrorContext` record when `ProcessPartitionOnce` returns an error, carrying the
domain, partition id, partition count, and the error text.

## Why it was needed

Shared-projection domains have no `attempt_count` column. A `fact_work_items`
domain records its own retry history — `WorkSink.Fail` increments
`attempt_count`, which is what the fault-injection gate's
`ifa_fault_assert_retried_above` reads for `gcp_resource_materialization`. The
shared-projection path has no equivalent: `runOneCycleSequential` and
`runOneCycleConcurrent` drop the error with `continue` and retry the same
`(domain, partition)` on the next poll, so a write that failed and then
recovered left **no durable trace anywhere**. An operator looking for why a
partition took two cycles had nothing to find.

That gap is also why #5555's SQL fault cell could not copy cell 4's technique.
The naive port would have counted retried rows for a domain that does not record
retries, producing a check that can never fail — the exact vacuity #5555 exists
to remove.

## No-Regression

No-Regression Evidence: the log fires only on the error branch. `ProcessPartitionOnce` returns a nil
error when another worker holds the lease
(`shared_projection_worker.go:284-286`), which is the common contended outcome,
so a normal cycle emits nothing and the success path is unchanged — no added
allocation, no added call, no lock held longer.

Measured on the `ifa-fault-injection` matrix (Postgres 15642 + NornicDB 7801 via
`docker-compose.yaml`, driven by `scripts/verify-ifa-fault-injection.sh`), all
seven cells produced the identical graph digest
`5bfd92cacbb6758b29d3073426ce69e69c6cdcc87535981d8550873be86acfdb` with zero
dead letters. Per-cell wall time on the clean run: baseline 13s, killworker 72s,
expirelease 11s, failgraphwrite 73s, restartbackend 10s, killworkersql 74s,
failgraphwritesql 19s. Nothing in that spread is attributable to the log; the
kill/lease cells dominate on their fixed 1-minute reducer lease.

## Observability

Observability Evidence: this adds one operator-facing signal:

```
shared projection partition processing failed; retrying on next poll cycle
  domain=<projection domain> partition_id=<n> partition_count=<n>
  error=<write error> phase=shared
```

At 3 AM this is the difference between "the drain is slow" and "partition 3 of
`sql_relationships` is failing its graph write and being retried." It is not a
new metric or span, so no telemetry-contract entry or dashboard panel is owed —
`scripts/verify-telemetry-coverage.sh` reports no untracked stages.

The gate reads this line as its fired-fault proof:
`ifa_fault_assert_sql_graph_write_fired` correlates domain, message, and injected
fault text on a single decoded record, so an unrelated failure elsewhere cannot
satisfy it.

## Reproduce

```
go test ./internal/reducer -run TestSharedProjectionRunnerLogsPartitionProcessingError -count=1
```

Fails without the change: the test drives a partition whose processor returns an
error and asserts the record appears. Reverting
`shared_projection_runner.go` to its pre-change form makes it fail at
`shared_projection_runner_partition_error_test.go:81`.
