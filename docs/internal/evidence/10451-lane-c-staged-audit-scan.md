# #10451 lane C: staged stale-winners audit classifier, affinity, telemetry

Lane C of the staged stale-winners audit scan (#10451) adds the
recovery-conflict classifier, the single-member tick runner, and the
operator telemetry in `go/internal/runtime/postgres` (`staged_audit_scan.go`,
`staged_audit_telemetry.go`, plus `staged_audit_scan_test.go`). It adds no
SQL, no writer handle, and no goroutine; the runner holds staged counts in
the abandoned result value, so an aborted tick discards them by returning.

## Scope

- `IsRecoveryConflict` defers only on SQLSTATE 40001 *with* the recovery
  message. `ClassifyStagedAuditError` maps conflict to
  `conflict-exhausted`, cap trips to `budget`, and every other batch failure
  (including 57014 timeouts) to `batch-abort`.
- `RunTick` pins one member per tick and runs census, each scope batch, and
  the stored-winners fetch each in its own short transaction on that member
  (`BatchTimeout` defaults to 25 s). Any failure aborts with
  `*StagedAuditDeferredError` and never calls the plan's `Verify`, so an
  aborted tick never contacts the writer.
- Fail-closed caps: 20,000 rows per batch, 1,000,000 staged facts,
  250,000 distinct keys.
- `StagedAuditTelemetry` records the deferral counter by closed reason, the
  last tick's batch count, max batch duration, staged gauges, member identity
  with replay lag at start/end, and the writer-verify discard rate.

## Proof

No-Regression Evidence: `go test ./internal/runtime/postgres/ -count=1`
passes whole-package with and without `-race`, and `go vet` is clean, at
branch `drive/10416-staged-scan-c` on top of `origin/main` (`fdcd9dd5ce`).
The seven `TestStagedAudit*` tests fail RED without the implementation
(undefined symbols) and pass GREEN with it: the classifier on the captured
live `ERROR: 40001 ... conflict with recovery` text (must defer) versus
bare-40001, phrase-only, and 57014 inputs (must not read as conflict);
affinity across 4 pinned statements with one `PinMember` and one connection
release per statement; batch abort with zero writer-verify calls and a
`batch-abort` deferral; over-budget batch as a `budget` deferral; a hung
batch failing on the configured timeout while `DefaultStagedAuditBatchTimeout`
stays pinned at 25 s; and the 3am telemetry surface (counts, gauges, member
`read-0` with 250 ms replay lag, discard rate 4.0 over 1 verify). Input shape
is the fake member-pinned fleet (no live standby needed); row counts are the
2-scope, 200-fact plans above. The legacy writer audit path
(`supply_chain_impact_winners_audit.go`) is byte-intact: `git status` shows
only the three new lane-C files.

No-Observability-Change: lane C adds no OTEL instrument, span, log line,
route, worker, lease, or runtime knob. Telemetry lives in the in-memory
`StagedAuditTelemetry` recorder the tick runner owns; the six
`eshu_dp_staged_audit_*` metric names are constants for the future
registration, never emitted. Operators diagnose the path through the tick
result (member, batches, staged counts, replay lag) and the deferral reason
on the returned `*StagedAuditDeferredError`.
