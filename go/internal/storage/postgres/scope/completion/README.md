# Postgres scope completion store

## Purpose

This package is the Postgres-backed cross-scope completion queue. Producer
domains publish completion events when their reducer work commits; the
reducer claims events under a lease and fans them out to eligible consumers
(`cross_scope_completion_events`); a consumer defers while any producer
domain it reads through is not ready. It is the `scope/completion/` leaf of
the storage/postgres split (#6693), nested under the `scope/` leaf.

## Ownership boundary

This package owns `CrossScopeCompletionStore` (queue claim, heartbeat,
retry, fanout), `CrossScopeProducerReadinessStore` (per-domain readiness),
`ProducerScopeQuiescence` (the registered-vs-quiescent scope probe), and
their SQL constants. `cmd/reducer` constructs both stores in its wiring and
drives the queue through `reducer.CrossScopeCompletionRunner`; the staying
root completion live tests (concurrency, snapshot, plan) still prove the
queue against the exported surface. The harness-bound concurrency and
snapshot proofs stay in root until the `container/image/` step: they open
their database exclusively through the container-image ACK capability
harness, which that family owns.

## Exported surface

- `NewCrossScopeCompletionStore(database)` with `Claim`, `Heartbeat`,
  `Retry`, `Fanout`, and the `Now` clock field.
- `FanoutCrossScopeCompletionQuery`, exported for the staying root plan
  tests, which EXPLAIN the shipped query rather than a copy.
- `ErrCrossScopeCompletionClaimRejected` lease-conflict sentinel.
- `CrossScopeProducerReadinessStore{DB}` with `CrossScopeProducersReady`.
- `ProducerScopeQuiescence` with `ProducerScopeQuiescenceReport`.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the shared query contracts.
- `internal/storage/postgres/array` for the quiescence probe's array
  binding.
- `internal/reducer` for domains, leases, and the completion runner
  contract.
- `internal/scope` for collector kinds in the readiness catalog.

## Telemetry

None of its own. Claiming, fanout, and the readiness probe run inside the
caller's transaction or wiring, so tracing and metrics are the caller's
responsibility, not this package's. `Fanout` reports the number of manifest
markers in an eligible state (claimed, running or succeeded) at an active
generation in `ManifestMarkersSkipped`; the reducer's completion runner logs it
per fan-out call and adds it to
`eshu_dp_cross_scope_completion_markers_skipped_total` (#10972). It is a census
taken after a fan-out for a producer that feeds `supply_chain_impact`
committed: before the exclusion the same fan-out would have locked and
re-pended or flagged each of those markers, so it is an upper bound on the
re-pends avoided per fan-out call, not a measured saving, and a fan-out that scheduled
nothing still reports it. The count is observability only. It is a separate read-only statement `Fanout` runs after
the fan-out statement returned and committed, only for a producer with a
`supply_chain_impact` consumer edge, under its own bounded context
(`defaultManifestMarkerCountTimeout`, 10 s, about ten times the measured
1,082 ms). It is the one place this package tolerates a failure: the fan-out
already committed and the count only feeds a counter, so a query error, scan
error, or timeout logs the WARN `cross-scope completion manifest marker count
failed` (fields `error`, `timeout`; the store's `Logger`, wired from the
reducer's service logger, with `slog.Default` as the fallback), leaves
`ManifestMarkersSkipped` at zero, and returns the committed result. A gap in the
counter is therefore not proof of zero markers: look for the WARN.

No-Observability-Change: this extraction moves only the completion queue,
the producer-readiness store, the quiescence probe, and their SQL
constants. Lease fencing (`FOR UPDATE SKIP LOCKED`), claim/retry/heartbeat
transitions, and probe predicates are byte-identical apart from the package
clause and the necessary export of the fanout query constant (the staying
root plan tests EXPLAIN it), and no metric, span, or log name changes.

No-Regression Evidence: focused `internal/storage/postgres/...` tests
(including this package) run green on the moved tree with `-race`,
including the moved quiescence live proof and the staying concurrency,
snapshot, and plan proofs against the exported surface; the completion SQL
text is unchanged so no plan or lifecycle proof is re-owed.

No-Regression Evidence (#10972): the fanout statement's `current_consumers` set
carries one residual `payload->>'trigger'` predicate, evaluated below the row
lock, so the manifest markers are never locked, re-pended, or flagged. The
statement has no second pass over the marker rows and returns four counts; the
skipped-marker count is a separate statement that runs after the fanout
committed, so it adds nothing to the lock hold of the fanout or to a producer
ack waiting on the event row. Measured on the QA replica at 2026-11-23 17:35Z
with real payloads (read-only; no lock clause, which a replica cannot take),
on the two statements that now ship:

- Candidate selection of the fanout statement for the `supply_chain_impact`
  consumer domain: the base statement selects 602 rows (claimed, running or
  succeeded at an active generation) in 1,035 ms; with the predicate it selects
  41 in 1,078 ms (+4%; both plans read about 231k shared buffers, almost all
  hits). The exclusion removes exactly the 561 markers (556 of them
  succeeded) and leaves the 41 findings rows unchanged. The set that is locked
  and updated falls from 602 rows to 41.
- The marker count, `manifestMarkerCountQuery`: 1,082 ms, no lock, no write.
  It is a separate unlocked read after the fanout committed, not part of the
  locking statement. It runs only for producers that feed `supply_chain_impact`.
  QA showed at least four such fanouts in 8 h (the bulk re-pend statements at
  13:14:12, 13:14:40, 14:52:43 and 16:29:34Z on 2026-11-23, seen through
  `reopened_at`; a fanout that scheduled nothing leaves no such trace, so the
  total is not measured). The runner's `fanout_duration_ms` includes this
  count, about 1 s on QA.

The UPDATE saving from 561 fewer rows is not measured. Lock order, lease
fencing, and the event horizon are unchanged. The 603-of-1,318 figure in the
diagnosis is a QA baseline from four bulk statements between 13:14Z and 16:29Z
on 2026-11-23, not a number derived from this repository. Unit proof:
`TestFanoutQueryExcludesManifestMarkers` (shape of the fanout statement, with
seeded violations), `TestManifestMarkerCountQueryIsAPlainRead` (no FOR clause,
no DML, shared predicate and active-generation text),
`TestFanoutCountsSkippedMarkersAfterTheFanoutCommits`,
`TestFanoutSkipsTheMarkerCountForAProducerWithoutASupplyChainEdge` and
`TestFanoutMarkerCountFailureDoesNotFailTheCommittedFanout`. The live proof is
`TestCrossScopeCompletionFanoutLeavesManifestMarkersSucceededLive`, which
asserts the count through the separate read, and the plan, ACK-overlap and
lock-order live probes EXPLAIN the shipped fanout constant (see the PR for the
lab run).

Observability Evidence (#10972): `CrossScopeCompletionResult.ManifestMarkersSkipped`
is logged per fan-out call as `manifest_markers_skipped` and added to
`eshu_dp_cross_scope_completion_markers_skipped_total{domain}`; with the
existing `manifest_trigger` marker signal an operator sees executions per hour
fall, and the skipped count bounds what the exclusion could have spared per
event (a census, not a measured saving; divide by the fan-out count). A failed count logs the
WARN `cross-scope completion manifest marker count failed` (fields `error` and
`timeout`) and adds nothing to the counter.

No-Regression Evidence (#10972 wording follow-up): the counter's meaning is
restated as a census, an upper bound on the re-pends avoided; the SQL, the
runner code path, the instrument name and labels, and the instrument
registration code are unchanged (comments and the metric description string
only, plus a stronger unit-test guard on the statement text). No new queue,
lock, claim or query cost, so the baseline and after measurements of the
fan-out statement are the ones recorded above.

No-Observability-Change: the signal, its labels and the log field keep their
names; only the documented meaning changed, in this README, `doc.go`, the
reducer runner comment, the metric description, `metrics.md` and the telemetry
coverage row.

## Gotchas / invariants

- The fanout never locks, re-pends, or flags a manifest marker
  (`supply_chain_impact` payload `trigger=manifest_dependency`): the marker
  reads no producer output and writes no finding. `manifestMarkerPredicate` is
  the one spelling, shared by the exclusion and the post-commit skipped count,
  which also share the active-generation and consumer-state SQL constants.
- Never let two copies of a lock predicate, lease fence, or claim
  transition drift: shared production logic is exported, not duplicated.
  Only test-only seed helpers exist as twin copies (Go test-only symbols
  do not cross package boundaries), each carrying a pointer comment naming
  its twin.
- The `Now` clock field on `CrossScopeCompletionStore` is the seam the
  lease-expiry tests control; the unexported `now` method falls back to the
  wall clock when it is nil. Staying tests call the field, never the
  method.
- A collector kind with no registered scope is ready, not blocked: do not
  turn absence into a deferral.
- Do not import the parent `postgres` package: that is an import cycle.
  Tests import it only for the `SQLDB` wrapper type and `ApplyBootstrap`.

## Related docs

- [Postgres storage](../../../README.md)
- [Postgres scope helpers](../../README.md)
- [storage/postgres target tree](../../../../../../docs/internal/design/6693-postgres-target-tree.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/... -race -count=1` and
`go vet ./internal/storage/postgres/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
