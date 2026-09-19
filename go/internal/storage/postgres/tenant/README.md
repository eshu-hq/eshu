# Postgres tenant workspace grant store

## Purpose

This package owns tenant workspace grant truth: tenant and workspace rows,
scope and repository grants, the primary-workspace resolution, and the DDL
behind them.

## Ownership boundary

This package owns the `tenants`, `workspaces`, `tenant_scope_grants`, and
`tenant_repository_grants` tables, their DDL and statements, and the
grant validation and normalization. The parent `postgres` package keeps the
schema bootstrap registry (embedded migrations are the source of truth),
the identity bootstrap that writes the first tenant rows, and the remaining
store families. `cmd/api`, `cmd/mcp-server`, and `cmd/workflow-coordinator`
own wiring: they construct the store and call it.

## Exported surface

- `TenantWorkspaceGrantStore` with
  `NewTenantWorkspaceGrantStore(database db.ExecQueryer)`.
- Record types `TenantRecord`, `WorkspaceRecord`, `TenantScopeGrant`,
  `TenantRepositoryGrant`, and query type `TenantWorkspaceGrantQuery`.
- `EnsureSchema`, upserts, grant listings, `PrimaryWorkspaceForTenant`.
- `TenantWorkspaceGrantSchemaSQL()` returns the DDL.
- `UpsertTenantRecordQuery` / `UpsertWorkspaceRecordQuery` stay exported
  for the identity bootstrap path until the identity leaf moves.
- Errors `ErrTenantWorkspaceAmbiguous`, `ErrTenantWorkspaceNotFound`.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the shared `ExecQueryer`/`Rows`
  contracts.
- `internal/storage/postgres/scalars` for the shared null/blank value
  helpers.

## Telemetry

None. This package executes bounded SQL through the injected database
handle; the instrumented handle and the callers own observability.

## Gotchas / invariants

- Grant writes never touch identity credentials; the bootstrap path writes
  tenant rows first in the same transaction.
- `PrimaryWorkspaceForTenant` enforces exactly-one-active-workspace: two
  active rows is an error, never a silent pick.
- Do not import the parent `postgres` package: that is an import cycle.
  Root tests exercise this package through its exported constructors.

No-Observability-Change: this extraction moves only the tenant grant store,
its SQL text, and its DDL. The statements are byte-identical, the callers
construct the same store, and no metric, span, or log name changes.

No-Regression Evidence: focused `postgres` package tests plus the tenant
grant, bootstrap-definition, and identity bootstrap tests run green on the
moved tree; the SQL text is unchanged so no plan or lifecycle proof is
re-owed.

## Related docs

- [Postgres storage](../README.md)
- [Shared database contracts](../db/README.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/... -count=1` and
`go vet ./internal/storage/postgres/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
