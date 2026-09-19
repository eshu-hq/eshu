# AGENTS.md — Postgres webhook trigger store guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for Postgres storage conventions.
3. `trigger_store.go`, `trigger_store_sql.go`, and
   `trigger_store_schema_sql.go` for the store, statements, and DDL.
4. `../webhook_trigger_store_test.go` for the unit contract (kept at root
   beside the shared queue fakes it reuses).

## Invariants

- Keep the claim, handoff, and failure SQL byte-identical when moving code;
  lifecycle or predicate changes need their own issue with EXPLAIN and
  contention proof per `eshu-postgres-rigor`.
- Preserve `FOR UPDATE SKIP LOCKED` and the
  `(status, received_at ASC, trigger_id ASC)` claim order exactly.
- Preserve the refresh-key dedupe and the ignored-to-queued promotion.
- Keep the package clause as `package webhookstore`; callers import the
  `storage/postgres/webhook` path without an alias.
- Never import the parent `postgres` package from here.

## Common changes

- Change the DDL only with the schema test's expected fragments updated in
  lockstep; the schema test asserts exact DDL substrings.
- Change claim predicates only with the SKIP LOCKED contention test and an
  idempotency proof.

## Failure modes

- Importing the parent `postgres` package creates an import cycle.
- Dropping `SKIP LOCKED` serializes or double-delivers concurrent claimants.
- Reordering the claim index changes which trigger a fleet claims first.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/... -count=1
go vet ./internal/storage/postgres/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
