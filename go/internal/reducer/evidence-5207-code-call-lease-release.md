# #5207 code-call lease-release evidence

## Runtime finding

The fresh 896-repository merged-code validation completed all `438,584`
`code_calls` shared intents, but the code-call runner continued renewing all
eight partition leases while the domain had zero open work. The terminal proof
requires zero active shared leases, so the validation could never enter its
30-minute stable window.

The conflict domain remains one code-call partition. This change does not alter
partitioning, worker count, lease TTL, polling, claim order, graph-write
concurrency, retry behavior, or queue ordering.

## Root cause

`CodeCallProjectionRunner.processPartitionOnce` stops the lease heartbeat and
then releases the partition. Stopping the heartbeat cancels its derived
`leaseCtx`; the runner previously passed that canceled context to
`ReleasePartitionLease`. A real Postgres `ExecContext` rejects the release, so
the lease remains owned until expiry and can be immediately reacquired by the
same runner.

The corrected lifecycle is:

1. Claim through the caller context.
2. Select, retract, write, mark complete, and renew through the heartbeat
   context.
3. Stop the heartbeat.
4. Release through the original still-live caller context.

## Local proof

No-Regression Evidence: the new production-path regression failed on current
`main` with `ReleasePartitionLease observed a canceled context: context
canceled`. After preserving the pre-heartbeat context for release, the same
test passed. The new regression plus the sibling generic shared-worker
heartbeat/release tests passed 10 repeated runs:

```text
go test ./internal/reducer \
  -run '^(TestCodeCallProjectionRunnerReleasesLeaseWithLiveContext|TestProcessPartitionOnceReleasesLeaseWithLiveContext|TestProcessPartitionOnceHeartbeatsLeaseDuringSlowWrite)$' \
  -count=10
```

The same three tests passed 10 runs under the Go race detector:

```text
go test -race ./internal/reducer \
  -run '^(TestCodeCallProjectionRunnerReleasesLeaseWithLiveContext|TestProcessPartitionOnceReleasesLeaseWithLiveContext|TestProcessPartitionOnceHeartbeatsLeaseDuringSlowWrite)$' \
  -count=10
```

Performance Evidence: the live before-state had eight renewed code-call leases
with zero open code-call intents, making stable terminal impossible. The fix
removes that TTL/reacquisition tail without changing useful concurrency or the
code-call processing path. A production-runner proof against a real Postgres 16
lease table passed locally:

```text
ESHU_SHARED_PROJECTION_HEARTBEAT_PROOF_DSN=<local-postgres-16-dsn> \
  go test ./internal/storage/postgres \
  -run '^TestCodeCallProjectionRunnerReleasesIdleLeaseAgainstPostgres$' \
  -count=1
```

The proof pauses the runner after its first empty partition cycle, reads the
actual `shared_projection_partition_leases` row, requires both `lease_owner`
and `lease_expires_at` to be `NULL`, and proves that a different worker can
immediately claim the released partition. The temporary proof schema is dropped
at test cleanup; retained application tables are not modified.

No-Observability-Change: the change adds no metric, span, log key, route,
runtime knob, queue table, graph statement, or graph truth change. Operators
continue to diagnose the lane through `shared_projection_partition_leases`,
shared-intent backlog, existing code-call cycle timing logs, and reducer queue
metrics. The observable intended delta is that an idle code-call domain has no
owned unexpired partition leases.
