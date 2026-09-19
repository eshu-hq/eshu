# AGENTS.md — Postgres tenant workspace grant store guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for Postgres storage conventions.
3. `workspace_grants.go`, `workspace_grants_schema.go`,
   `workspace_grants_primary.go`, and `workspace_grants_query.go` for the
   store, statements, DDL, and primary-workspace resolution.
4. `../tenant_workspace_grants_test.go` for the store contract (kept at
   root beside the bootstrap registry it asserts against).

## Invariants

- Keep the grant SQL byte-identical when moving code; predicate or lifecycle
  changes need their own issue with EXPLAIN and contention proof per
  `eshu-postgres-rigor`.
- Preserve the exactly-one-active-workspace rule: ambiguity and absence are
  distinct typed errors, never a silent pick or an empty string.
- Keep `UpsertTenantRecordQuery` / `UpsertWorkspaceRecordQuery` exported
  until the identity leaf moves; the identity bootstrap writes through them.
- Keep the package clause as `package tenantstore`; callers import the
  `storage/postgres/tenant` path without an alias.
- Never import the parent `postgres` package from here.

## Common changes

- Change the DDL only with the bootstrap-definition test's expected
  fragments updated in lockstep; that test asserts exact DDL substrings
  against the embedded migration source of truth.
- Change grant validation only with the store's own validation tests; the
  validators reject empty ids, bad times, and half-supplied queries before
  any SQL runs.

## Failure modes

- Importing the parent `postgres` package creates an import cycle.
- Picking a workspace when two are active silently mis-scopes every grant
  read behind it.
- Unexporting the upsert queries before the identity leaf moves breaks the
  identity bootstrap build.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/... -count=1
go vet ./internal/storage/postgres/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
