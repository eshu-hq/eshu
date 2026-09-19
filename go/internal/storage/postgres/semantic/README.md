# Postgres semantic-extraction queue store

## Purpose

This package owns the semantic-extraction work queue: planned records flow
through queued, claimed, succeeded, retried, skipped, and dead-lettered
states under lease fencing, with redacted observability for the status
page.

## Ownership boundary

This package owns the semantic-extraction job table, its DDL and
statements, the claim/retry/dead-letter/skip lifecycle, and the redacted
status snapshot query. The parent `postgres` package keeps the schema
bootstrap registry (embedded migrations are the source of truth) and the
status page that renders the snapshot. `cmd/workflow-coordinator` owns
wiring: it constructs the store and runs the claim loop.
`internal/semanticqueue` owns the plan and record domain types.

## Exported surface

- `SemanticExtractionQueueStore` with
  `NewSemanticExtractionQueueStore(database db.ExecQueryer)`.
- `ApplyPlan`, `StatusSummary`, `ClaimNext`, `RetryClaim`,
  `DeadLetterClaim`, `SkipClaimByPolicy`, `SucceedClaim`,
  `ObservabilitySnapshot`.
- `SemanticExtractionJobSchemaSQL()` returns the DDL.
- `ReadSemanticExtractionObservability` and
  `SemanticExtractionObservabilityQuery` stay exported for the status
  family and the proof-domain harness until the status leaf moves.
- Error `ErrSemanticExtractionClaimRejected` for lost-lease mutations.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the shared `ExecQueryer`/`Rows`
  contracts.
- `internal/semanticqueue` for plan and record types.
- `internal/status` for snapshot shapes.

## Telemetry

None. This package executes bounded SQL through the injected database
handle; the instrumented handle and the coordinator claim loop own
observability.

## Gotchas / invariants

- Every claim mutation carries the lease fence: a stale lease owner gets
  `ErrSemanticExtractionClaimRejected`, never a silent overwrite.
- The claim holds `FOR UPDATE SKIP LOCKED` so concurrent claimants never
  double-deliver.
- Backoff and non-provider rows are skipped, never claimed.
- Storing a plan never marks extraction truth complete; only `SucceedClaim`
  advances a row.
- Do not import the parent `postgres` package: that is an import cycle.
  Root tests exercise this package through its exported constructors.

No-Observability-Change: this extraction moves only the semantic queue
store, its SQL text, and its DDL. The claim, fence, retry, dead-letter,
and snapshot statements are byte-identical, the coordinator constructs the
same store, and no metric, span, or log name changes.

No-Regression Evidence: focused `postgres` package tests plus the lease
fencing, SKIP LOCKED, stale-lease, dead-letter, and skip-policy tests run
green on the moved tree; the SQL text is unchanged so no plan or lifecycle
proof is re-owed.

## Related docs

- [Postgres storage](../README.md)
- [Shared database contracts](../db/README.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/... -count=1` and
`go vet ./internal/storage/postgres/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
