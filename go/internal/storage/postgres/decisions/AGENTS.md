# AGENTS.md — Postgres projection-decision store guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for Postgres storage conventions.
3. `decisions.go` for the store, DDL, and statements.
4. `decisions_test.go` for the store contract, including the in-memory
   `decisionTestDB` fake and `fake.Result` from
   `internal/storage/postgres/fake`.

## Invariants

- Keep the upsert, list, and evidence SQL byte-identical when moving code;
  predicate or lifecycle changes need their own issue with EXPLAIN and
  contention proof per `eshu-postgres-rigor`.
- Preserve the `ON CONFLICT ... DO UPDATE` idempotency on both
  `UpsertDecision` and `InsertEvidence`.
- Preserve the `(created_at, id)` ascending order on `ListDecisions` and
  `ListEvidence`.
- Keep the package clause as `package decisionsstore`; callers import the
  `storage/postgres/decisions` path without an alias.
- Never import the parent `postgres` package from here.

## Common changes

- Change the DDL only with `TestDecisionStoreSchemaSQL`'s expected
  fragments updated in lockstep.
- Change filter or ordering behavior only with a failing regression test
  added first per this repo's TDD policy.

## Failure modes

- Importing the parent `postgres` package creates an import cycle.
- Dropping `ON CONFLICT ... DO UPDATE` turns redelivery into a duplicate-key
  error instead of an idempotent overwrite.
- Returning an unbounded result set when `Limit` is non-positive instead of
  clamping to 1.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/... -count=1
go vet ./internal/storage/postgres/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
