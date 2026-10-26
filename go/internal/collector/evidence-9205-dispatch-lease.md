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
 batch-size change). Before/after on the same host, `go test -count=1` of the
 three touched packages, two runs each: base `319f4da104` collector
 0.126s/0.123s, extensionhost 0.261s/0.320s, telemetry 1.261s/1.255s; head
 `7f1fbceb22` collector 0.120s/0.127s, extensionhost 0.214s/0.245s, telemetry
 1.269s/1.264s. No suite regressed beyond run-to-run noise. The histogram
 record is an in-memory OTEL observation on existing label sets
 (`collector_kind`, `source_system`); bucket storage is bounded by the shared
 ack-wait scale. Backend store behavior is unchanged (heartbeat SQL path
 untouched), and fencing-guard unit tests
 (`TestWorkflowControlStoreHeartbeatClaimUsesFencingAndOwnerGuards`,
 `...RejectsStaleClaim`) still pass unmodified.

 Observability Evidence: `processClaimed` now records
 `eshu_dp_workflow_claim_dispatch_lease_remaining_seconds` (remaining granted
 lease at satellite dispatch, by `collector_kind` and `source_system`;
 near-zero values precede fail-closed deadline expiry) and logs a warn
 (`claim dispatch re-stamped stale lease`, with `stale_skew_seconds`) when the
 incoming snapshot lease was already past, so a frozen-snapshot recurrence is
 diagnosable without a database dive.
