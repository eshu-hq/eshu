# Postgres projection-decision store

## Purpose

This package owns `projection_decisions` and `projection_decision_evidence`:
the reducer-owned record of what a projection decided to materialize, and the
evidence facts behind each decision.

## Ownership boundary

This package owns the `DecisionStore` type, its DDL, and its upsert/list
statements. `internal/projector` owns the `ProjectionDecisionRow` and
`ProjectionDecisionEvidenceRow` domain types this store reads and writes;
`BuildProjectionDecision`/`BuildProjectionEvidence` there construct them.
`internal/query/admin/store` is the current caller, reading through
`ListDecisions` for the admin decisions view. The parent `postgres` package
keeps the schema bootstrap registry (embedded migrations are the source of
truth); no runtime wiring currently calls this store's `EnsureSchema` or
write methods outside this package's own tests.

## Exported surface

- `DecisionStore` with `NewDecisionStore(database db.ExecQueryer)`.
- `EnsureSchema`, `UpsertDecision`, `InsertEvidence`, `ListDecisions`,
  `ListEvidence`.
- `DecisionFilter` bounds `ListDecisions` by repository, source run, an
  optional decision type, and a limit (non-positive clamps to 1).
- `DecisionSchemaSQL()` returns the DDL text.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the shared `ExecQueryer`/`Rows`
  contracts.
- `internal/projector` for `ProjectionDecisionRow` and
  `ProjectionDecisionEvidenceRow`.

## Telemetry

None. This package executes bounded SQL through the injected database
handle; the instrumented handle owns observability.

## Gotchas / invariants

- `UpsertDecision` and `InsertEvidence` write with `ON CONFLICT ... DO
  UPDATE`, so redelivering the same decision or evidence row is idempotent.
- `ListDecisions` and `ListEvidence` order by `(created_at, id)` ascending;
  a caller needing a stable page must not rely on any other order.
- `ListDecisions` clamps a non-positive `Limit` to 1 rather than returning
  an unbounded result set.
- Do not import the parent `postgres` package: that is an import cycle.

No-Observability-Change: this extraction moves only the decision store, its
SQL text, and its DDL. The upsert, list, and evidence statements are
byte-identical, the admin store constructs the same store, and no metric,
span, or log name changes.

No-Regression Evidence: focused package tests cover upsert-and-list,
upsert-overwrite, decision-type filtering, evidence insert/list, empty
evidence insert, and the default-limit clamp; they run green on the moved
tree with the SQL text unchanged, so no plan or lifecycle proof is re-owed.

## Related docs

- [Postgres storage](../README.md)
- [Shared database contracts](../db/README.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/... -count=1` and
`go vet ./internal/storage/postgres/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
