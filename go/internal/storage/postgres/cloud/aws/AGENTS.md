# AGENTS.md — Postgres AWS claim-fencing store guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Postgres storage conventions.
3. `pagination_checkpoint.go` for the checkpoint store, DDL, and queries.
4. `scan_status.go` for the scan-status store, DDL, and queries.
5. `pagination_checkpoint_test.go` / `scan_status_test.go` for both stores'
   fencing contracts.

## Invariants

- Keep the DDL and fencing predicates byte-identical when moving code;
  predicate or lifecycle changes need their own issue with EXPLAIN and
  contention proof per `eshu-postgres-rigor`.
- `AWSPaginationCheckpointStore.Save`/`Complete`/`ExpireStale` must keep
  their `fencing_token <=` conflict guards.
- `AWSScanStatusStore.ObserveAWSScan`/`CommitAWSScan` must keep an exact
  `(generation_id, fencing_token)` match; only `StartAWSScan`'s insert-time
  conflict guard widens across generations, and only for the terminal or
  orphaned-row cases `startAWSScanStatusQuery`'s comment documents
  (issue #612).
- Keep the package clause as `package awsstore`; callers import the
  `storage/postgres/cloud/aws` path without an alias.
- Never import the parent `postgres` package from here.

## Common changes

- Change either DDL only with its schema test's expected fragments updated
  in lockstep (`TestAWSPaginationCheckpointSchemaSQL`,
  `TestAWSScanStatusSchemaSQL`).
- Change a fencing predicate only with the store's fencing tests updated and
  a contention/idempotency proof for the affected `ON CONFLICT`/`UPDATE`.

## Failure modes

- Importing the parent `postgres` package creates an import cycle.
- Loosening `AWSPaginationCheckpointStore`'s fence guard lets a stale worker
  overwrite a newer claim's page state, corrupting resume.
- Loosening `AWSScanStatusStore`'s generation-handoff conditions beyond the
  documented terminal/orphaned cases lets a live worker's in-progress row be
  clobbered mid-scan.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/cloud/aws/... -race -count=1
go vet ./internal/storage/postgres/cloud/aws/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
