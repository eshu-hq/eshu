# AGENTS.md — Postgres governance audit store guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Postgres storage conventions.
3. `store.go` for `GovernanceAuditStore`, `GovernanceAuditQuery`, and the DDL.
4. `helpers.go` for `appendBatch`, `buildGovernanceAuditListQuery`,
   `scanGovernanceAuditEvent`, and the #6574 unknown-enum tally/warn.
   `tenant_test.go` and `tenant_eventid_test.go` share one fixture
   (`governanceAuditTenantMemoryDB`) across two files to stay under the
   500-line cap; a new tenant-isolation test belongs in whichever file its
   subject matter groups with, not automatically the shorter one.
5. `../../governance_audit_store_test.go` (kept at root: it asserts root's
   `BootstrapDefinitions()`/`orderedBootstrapDefinitionNames`) for the
   bootstrap-registry lockstep proof over this package's DDL.

## Invariants

- Keep `Append`'s validation-before-SQL order, its 500-row batching, and the
  `ON CONFLICT (event_id) DO NOTHING` idempotency byte-identical when moving
  or refactoring code.
- Keep the event id derived from every audit field including `TenantID`/
  `WorkspaceID`; dropping either from the hash reintroduces the cross-tenant
  collision regression (`TestGovernanceAuditEventIDDistinctAcrossTenants`)
  that silently drops the second `Append` via `ON CONFLICT DO NOTHING`.
- Keep `List` requiring `GovernanceAuditQuery.OperatorAuthorized` before it
  issues any query, and keep it excluding global/NULL-tenant events from a
  `TenantID`-scoped result.
- Keep `List`'s tolerant read (`governanceaudit.NormalizeStoredEvent`) and its
  once-per-field WARN (#6574) — an unknown enum value must never fail the
  whole page, and a page of known values must log nothing.
- Keep `Summary`/`SummaryForTenant` selecting only aggregate columns; never
  add `actor_id_hash`, `scope_id_hash`, `service_principal_id`,
  `policy_revision_hash`, or `correlation_id` to their SQL.
- Keep the package clause as `package auditstore`; callers import the
  `storage/postgres/governance/audit` path without an alias.
- Never import the parent `postgres` package from here.

## Common changes

- Change the DDL only with `TestGovernanceAuditSchemaDDLIncludesTenantID` (and
  root's `TestBootstrapDefinitionsIncludeGovernanceAuditEvents`) updated in
  lockstep.
- Change the unknown-enum tolerance or its WARN shape only with
  `list_warn_test.go` and `scan_tolerance_test.go` updated together — they
  pin the exact field/rows/values shape an operator greps for.
- Change tenant scoping only with `tenant_test.go`'s cross-tenant isolation
  and event-id-uniqueness regressions kept green.

## Failure modes

- Importing the parent `postgres` package creates an import cycle.
- Dropping the tenant/workspace fields from the event-id hash silently drops
  the second of two cross-tenant-identical events via `ON CONFLICT DO
  NOTHING`.
- Failing (instead of warning and keeping) an unknown enum value on `List`
  breaks reads during a rolling upgrade where a newer pod already wrote a
  value this build's registry lacks.
- Letting `Summary`/`SummaryForTenant` select a detailed column turns an
  aggregate-only status surface into a private-detail leak.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/governance/audit/... -count=1
go vet ./internal/storage/postgres/governance/audit/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
