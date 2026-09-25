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
responsibility, not this package's.

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

## Gotchas / invariants

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
