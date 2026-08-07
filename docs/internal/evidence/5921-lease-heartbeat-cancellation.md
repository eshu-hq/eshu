# #5921 / #5839 lease-heartbeat cancellation evidence

## What was broken

The code-call and generic shared-projection runners renew a partition lease
while graph work is in progress. Their orderly stop cancels the heartbeat
context and waits for any in-flight renewal to return. A real
`database/sql` query can return `context.Canceled` at that point. Both runners
recorded that self-inflicted error as a lost lease after the work had already
completed.

The issue assumed the repo-dependency runner had already fixed the same class
of bug in #5839. Live `main` still had #5839 open and no fixing pull request.
Its heartbeat suppressed an error-free lease rejection when that rejection
raced an orderly stop. The shared Postgres lease store also returned
`(false, nil)` whenever `Rows.Next()` was false without checking `Rows.Err()`.
Go's `database/sql.Rows.Next` documentation says false means either exhaustion
or an error and tells callers to use `Rows.Err()` to distinguish them.

## What changed

The code-call and shared-projection heartbeat loops now ignore renewal errors
that wrap `context.Canceled`. Each loop records only its first genuine failure,
so a later tick cannot replace the original cause or emit another failure
signal.

The repo-dependency loop now separates request errors from error-free lease
rejections. It ignores a cancellation error, but always records an explicit
`claimed=false, err=nil` result as lost ownership even when stop is racing it.
`SharedIntentStore.ClaimPartitionLease` now checks `Rows.Err()` before treating
a false `Next()` result as an error-free rejection.

The existing #5839 test no longer races a fixed writer delay against the
heartbeat ticker. Its writer waits for the heartbeat to cancel the work
context, which orders lease-loss recording before the test can finish.

## Concurrency and performance impact

The conflict key is unchanged: one
`(projection_domain, partition_id, partition_count)` lease row. The renewal
goroutine, TTL-derived tick rate, graph-write boundaries, completion calls, and
release path are also unchanged. The fix adds no goroutine, SQL statement,
retry, worker limit, batch change, or lock.

The only store-path cost is one `Rows.Err()` method call after `Rows.Next()`
returns false. It issues no database request. Heartbeat success still takes the
same branch and does no extra work beyond the cancellation test on the error
path. The rejection threshold for this change was any additional lease call,
database round trip, lock scope, or reduction in worker concurrency; none was
introduced.

## No-Regression Evidence:

The four new tests use channels to establish the ordering. The graph writer
cannot finish until a renewal is inside `ClaimPartitionLease`, and the fake
renewal cannot return until production stop cancels its own context. The
repo-dependency rejection test releases its blocked `(false, nil)` result only
after the stop context is done. The store test returns `Next()==false` with
`Err()==context.Canceled`.

Before the production edits, the reducer tests failed at all three intended
assertions:

```text
ProcessPartitionOnce() error = heartbeat shared projection partition lease: context canceled
processOnce() error = heartbeat code call lease: heartbeat code call lease: context canceled
stopHeartbeat() error = nil, want explicit lease rejection surfaced
```

The store test also failed with:

```text
ClaimPartitionLease() error = <nil>, want context.Canceled
```

After the fix, the focused reducer proof passes under the race detector:

```bash
cd go
go test ./internal/reducer -race -count=1 -v \
  -run 'Test(CodeCallProjectionRunnerOrderlyStopDoesNotMisreportInFlightRenewalCancellation|ProcessPartitionOnceOrderlyStopDoesNotMisreportInFlightRenewalCancellation|RepoDependencyLeaseHeartbeatRecordsExplicitRejectionDespiteConcurrentStop|RepoDependencyProjectionRunnerQuarantinesHeartbeatLossBeforeSuccess|RepoDependencyProjectionRunnerOrderlyHeartbeatStopNeverQuarantines)$'
```

The focused store proof passes:

```bash
cd go
go test ./internal/storage/postgres -count=1 -v \
  -run '^TestSharedIntentStoreClaimPartitionLease(SurfacesRowsError|.*)$'
```

Mutation checks removed both `context.Canceled` guards together. Both orderly
stop tests failed again. A second mutation suppressed an error-free rejection
when the heartbeat context was already canceled and removed the `Rows.Err()`
check; their two regressions failed again. Restoring the code reproduced these
pre-mutation SHA-256 hashes byte for byte:

```text
d8b3d5f772efc1f02345209573eaa371f7d944fcf0c605cdd6dd3fbf05648e76  code_call_projection_runner_lease.go
76f124d79e2decb05f585d0d9237c097762135eeabc9913ff27dd72ec1e07ff9  shared_projection_worker_lease_heartbeat.go
06fb1e1306d5edcbfa287465cd4edd0af5f7e420c7626c870cd59ec61bd22bdd  repo_dependency_projection_runner.go
00607739de27dc1f01b62d26bcc55eba83ce34cdf4a08f6d3dd79b3e2e318481  shared_intents.go
```

## No-Observability-Change:

No metric, span, log key, status field, or runtime setting changed. The
existing code-call error log and shared-projection missed-heartbeat counter no
longer fire for the runner's own orderly cancellation. They still fire once
for the first genuine renewal error or explicit rejection. Repo-dependency
keeps its existing warning and now emits it when a real error-free rejection
races stop instead of silently dropping that event.
