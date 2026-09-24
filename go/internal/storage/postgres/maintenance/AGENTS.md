# AGENTS.md — Postgres maintenance-request store guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for Postgres storage conventions.
3. `requests.go` for the store, the `runtime_ingester_control` DDL text, and
   every request/claim/complete/read statement.
4. `requests_test.go` for the store contract, including the export_test.go
   shim exposing `controlSchemaSQL`.

## Invariants

- Keep the request, claim, complete, and read SQL byte-identical when moving
  code; predicate or lifecycle changes need their own issue with EXPLAIN and
  contention proof per `eshu-postgres-rigor`.
- Preserve the pending-only claim guard and running-only complete guard on
  every mutation exactly; a claim against a non-pending row returns an
  explicit "no pending ... request" error, never a silent no-op.
- Keep the package clause as `package maintenancestore`; callers import the
  `storage/postgres/maintenance` path without an alias.
- Never import the parent `postgres` package from non-test code here; only
  `requests_test.go` imports it, to check `BootstrapDefinitions`.

## Common changes

- Change the DDL text only with `TestStatusRequestStoreControlSchemaIncludesExpectedColumns`
  updated in lockstep; that test asserts exact substrings.
- Change claim or complete predicates only with the pending/running-guard
  tests and an idempotency proof.

## Failure modes

- Importing the parent `postgres` package from non-test code would couple
  this leaf back to root and block root from ever importing it.
- Dropping the pending-only or running-only guard lets a stale or
  out-of-order caller silently skip a lifecycle state.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/... -count=1
go vet ./internal/storage/postgres/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
