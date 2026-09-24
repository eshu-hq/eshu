# AGENTS.md — Postgres Terraform-state admin evidence guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Postgres storage conventions.
3. `status.go` for the two list queries, the row-assembly helpers, and
   `ReadTerraformStateAdminEvidence`.
4. `../../status.go` for how the root `StatusStore` calls through to this
   package to populate `RawSnapshot.TerraformStateLastSerials` and
   `RawSnapshot.TerraformStateRecentWarnings`.

## Invariants

- Keep `terraformStateLastSerialQuery` and `terraformStateRecentWarningsQuery`
  byte-identical when moving code; predicate changes need their own issue
  with EXPLAIN and cardinality proof per `eshu-postgres-rigor`.
- Keep `ReadTerraformStateAdminEvidence` and `TerraformStateAdminEvidence`
  exported until the status leaf moves; the root status family reads
  through them.
- Keep the package clause as `package statestore`; callers import the
  `storage/postgres/terraform/state` path without an alias.
- Never import the parent `postgres` package from here.

## Common changes

- Change either query's predicate or column list only with a query-shape
  test asserting the exact fragments the callers rely on
  (`rank <= $1`, the `collector_kind IN (...)` / `scope_kind IN (...)`
  filters, the `unresolved_backend_expression` Git-warning branch).
- New fields on `statuspkg.TerraformStateLocatorSerial` or
  `TerraformStateLocatorWarning` need a matching `SELECT` column and scan
  target in the corresponding list function.

## Failure modes

- Importing the parent `postgres` package creates an import cycle.
- Dropping the malformed-row skip in `listTerraformStateLastSerials` turns
  one bad `generation_id` into a failed admin-status read.
- Passing a non-positive `limit` straight to the SQL bind (instead of
  defaulting it) would return zero rows instead of the contract default.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/... -count=1
go vet ./internal/storage/postgres/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
