# Evidence: dispatch-time lease re-stamp (#9205)

Issue [#9205](https://github.com/eshu-hq/eshu/issues/9205): the extension host
shipped the frozen claim-time `LeaseExpiresAt` snapshot to the satellite, so a
claim that waited minutes before dispatch failed closed on an expired
deadline. The fix inserts `grantDispatchLease` into
`ClaimedService.processClaimed`: one dispatch-time `HeartbeatClaim`, then
`item.LeaseExpiresAt` is re-stamped to the granted lease
(`ObservedAt + LeaseDuration`) before `Source.NextClaimed`. A lost grant fails
retryable instead of dispatching into certain expiry.

 No-Regression Evidence (#9205): the change adds exactly one `HeartbeatClaim`
 UPDATE per claimed cycle, serialized inside `processClaimed` on the existing
 fenced path (claim/token/owner guards unchanged, no new goroutine, lock, or
 batch-size change); the P2 follow-up only reroutes the already-failing
 fail-write path (join causes, continue on fenced-away) with no new store
 call. Before/after on the same host, `go test -count=1` of the three touched
 packages, two runs each: base `fd09ae5410` collector 0.167s/0.137s,
 extensionhost 0.250s/0.311s, telemetry 1.631s/1.741s; 7e01e6434f collector
 0.121s/0.141s, extensionhost 0.302s/0.217s, telemetry 1.483s/1.369s. No suite
 regressed beyond run-to-run noise. The histogram record is an in-memory OTEL
 observation on existing label sets (`collector_kind`, `source_system`);
 bucket storage is bounded by the shared ack-wait scale. Backend store
 behavior is unchanged (heartbeat SQL path untouched), and fencing-guard unit
 tests (`TestWorkflowControlStoreHeartbeatClaimUsesFencingAndOwnerGuards`,
 `...RejectsStaleClaim`) still pass unmodified.

 Observability Evidence: `processClaimed` now records
 `eshu_dp_workflow_claim_dispatch_lease_remaining_seconds` (remaining granted
 lease at satellite dispatch, by `collector_kind` and `source_system`;
 near-zero values precede fail-closed deadline expiry) and logs a warn
 (`claim dispatch re-stamped stale lease`, with `stale_skew_seconds`) when the
 incoming snapshot lease was already past, so a frozen-snapshot recurrence is
 diagnosable without a database dive.

## P2 follow-up: fenced-away retryable-fail (review on #9212)

 No-Regression Evidence: `failRetryable` now joins the dispatch cause with a
 failed `FailClaimRetryable` write instead of dropping it, and returns the
 continue sentinel when the write is rejected with `ErrClaimRejected` (the
 grant belongs to another generation: nothing left to fail). No new store
 call, no new goroutine or lock; the fenced-away branch only skips a retry
 record the store refused. Same-host `go test -count=1` of the touched
 packages, two runs each: parent `4b760ca6a7d1` collector 0.129s/0.120s,
 extensionhost 0.222s/0.174s, telemetry 1.260s/1.252s, workflow 0.011s/0.009s;
 this head collector 0.121s/0.141s, extensionhost 0.302s/0.217s, telemetry
 1.483s/1.369s, workflow 0.010s/0.010s. No suite regressed beyond run-to-run
 noise. Covered by `TestFailRetryableJoinsOriginalCauseWhenFailWriteFails`
 and `TestClaimedServiceContinuesWhenDispatchGrantAndRetryableFailAreFencedAway`.

 Observability Evidence: the fenced-away drop logs
 (`retryable-fail fenced away, dropping stale claim`, with `collector_kind`,
 `source_system`, `failure_class`) so a fencing-loss recurrence stays visible
 without a database dive; the generic fail-write path returns both causes in
 the joined error.
