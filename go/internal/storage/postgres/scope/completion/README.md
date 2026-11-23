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
responsibility, not this package's. `Fanout` reports the manifest markers it
left alone in `ManifestMarkersSkipped`; the reducer's completion runner logs it
per event and adds it to
`eshu_dp_cross_scope_completion_markers_skipped_total` (#10972).

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

No-Regression Evidence (#10972): the fanout's `current_consumers` set carries
one residual `payload->>'trigger'` predicate, evaluated below the row lock, so
the manifest markers are never locked, re-pended, or flagged. Measured on the QA
replica at 2026-11-23 17:35Z with real payloads (read-only; the candidate
selection of the shipped statement for the `supply_chain_impact` consumer
domain, without the lock clause a replica cannot take): the base statement
selects 602 rows (claimed, running or succeeded at an active generation) and the
statement with the predicate selects 41, so the exclusion removes exactly the
561 markers (556 of them succeeded) and leaves the 41 findings rows unchanged;
execution time is 1,035 ms at base, 1,078 ms with the predicate (+4%, the
predicate adds no measurable cost: both plans read about 231k shared
buffers, almost all hits) and 1,082 ms for the extra
read-only `skipped_markers` count, which takes no lock and writes nothing. The
set that is locked and updated falls from 602 rows to 41. The raw output is in
the PR discussion. Lock order, lease fencing, and the event horizon are
unchanged. The 603-of-1,318 figure in the diagnosis is a baseline from four QA
bulk statements between 13:14Z and 16:29Z on 2026-11-23, not a number derived
from this repository. The plan tests that EXPLAIN the shipped constant
(`reducer_queue_ack_fanout_plan_test.go`, `reducer_queue_ack_scale_plan_test.go`)
need their own environment switches and were not run for this change; the unit
shape check is `TestFanoutQueryExcludesManifestMarkers`.

Observability Evidence (#10972): `CrossScopeCompletionResult.ManifestMarkersSkipped`
is logged per event as `manifest_markers_skipped` and added to
`eshu_dp_cross_scope_completion_markers_skipped_total{domain}`; with the
existing `manifest_trigger` marker signal an operator sees executions per hour
fall while the skipped count shows the exclusion firing.

## Gotchas / invariants

- The fanout never locks, re-pends, or flags a manifest marker
  (`supply_chain_impact` payload `trigger=manifest_dependency`): the marker
  reads no producer output and writes no finding. `manifestMarkerPredicate` is
  the one spelling, shared by the exclusion and the skipped count.
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
