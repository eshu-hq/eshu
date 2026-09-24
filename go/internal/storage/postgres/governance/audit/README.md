# Postgres governance audit store

## Purpose

This package owns the private `governance_audit_events` sink: the durable,
retry-idempotent record of authorization decisions (allow/deny/unavailable)
the API, MCP server, workflow coordinator, and admin CLI make, plus the
bounded reads and aggregate summaries built on top of it. It is not a login
flow or session store — see `identity/` for those.

## Ownership boundary

This package owns the `governance_audit_events` table, its DDL and indexes,
`Append`'s validation and content-hash event-id derivation, the
operator-authorized `List` and its unknown-enum tolerance (#6574), and
`Summary`/`SummaryForTenant`/`DeleteExpired`. The `governanceaudit` package
(`internal/governanceaudit`) owns the `Event`/`Summary` domain types and the
`NormalizeEvent`/`NormalizeStoredEvent`/`UnknownEnums` validators this
package calls; it does not depend on Postgres. `internal/governanceauditasync`
owns the background appender that batches calls into this store's `Append`
from a single worker so a synchronous emission never adds latency to a
successful MCP/API read.

## Exported surface

- `GovernanceAuditStore` with `NewGovernanceAuditStore(database db.ExecQueryer)`,
  `WithLogger`, `EnsureSchema`, `Append`, `List`, `Summary`,
  `SummaryForTenant`, `DeleteExpired`.
- `GovernanceAuditQuery` — the bounded filter `List`/`SummaryForTenant` accept
  (`OperatorAuthorized`, event/actor/scope/decision filters, time bounds,
  `Limit`, `OrderDesc`, `TenantID`).
- `GovernanceAuditEventsSchemaSQL()` returns the private sink's DDL.
- `ErrGovernanceAuditQueryUnauthorized` — `List`'s rejection when
  `OperatorAuthorized` is unset.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the shared `ExecQueryer`/`Rows`
  contracts.
- `internal/governanceaudit` for the `Event`/`Summary` domain types and the
  write/read validators (`NormalizeEvent`, `NormalizeStoredEvent`,
  `UnknownEnums`).

## Telemetry

None in this package. `List`'s unknown-enum WARN
(`governance audit list kept a value this build does not know; this pod is
likely on an older build than the writer`, #6574) is a structured log through
the injected `*slog.Logger` (default `slog.Default`), not a metric or span:
one line per affected field per call, with `field`, `rows`, and `values`
(sorted, comma-joined, shape-checked tokens already validated safe to log).
`internal/governanceauditasync`'s background appender owns the
queue-depth/drop metrics for the async write path in front of this store.

No-Observability-Change: this extraction moves the store, its DDL, and its
existing #6574 WARN verbatim; no metric, span, log field, worker, queue,
lease, retry, or durable write shape changed.

## Gotchas / invariants

- `Append` validates every event with `governanceaudit.NormalizeEvent` before
  it reaches SQL and never echoes a rejected value in its error; it batches
  writes in groups of 500 rows and relies on `ON CONFLICT (event_id) DO
  NOTHING` for retry idempotency. The event id is a SHA-256 of every audit
  field including `TenantID`/`WorkspaceID`: two events identical except for
  tenant must not collide, or the second `Append` would be silently dropped
  (`TestGovernanceAuditEventIDDistinctAcrossTenants`).
- `List` requires `GovernanceAuditQuery.OperatorAuthorized`; an unauthorized
  call returns `ErrGovernanceAuditQueryUnauthorized` without ever issuing a
  query. `TenantID` scopes the result to one tenant and excludes
  global/NULL-tenant events; leave it empty for the shared-operator view that
  sees everything (#3717).
- `List` tolerates (rather than fails on) an `event_type`, `actor_class`,
  `scope_class`, or `decision` value this build's registry does not know —
  `governanceaudit.NormalizeStoredEvent` returns it verbatim — but still
  rejects a value that is not a bounded, shape-checked token, so a corrupt or
  hand-edited row cannot leak a raw value through the read path.
- `Summary`/`SummaryForTenant` return only aggregate counts; their SQL never
  selects `actor_id_hash`, `scope_id_hash`, `service_principal_id`,
  `policy_revision_hash`, or `correlation_id`.
- Do not import the parent `postgres` package: that is an import cycle. Root
  keeps `governance_audit_store_test.go`'s
  `TestBootstrapDefinitionsIncludeGovernanceAuditEvents` because it asserts
  root's `BootstrapDefinitions()`/`orderedBootstrapDefinitionNames` bootstrap
  registry; it exercises this package through `auditstore.NewGovernanceAuditStore`
  and `auditstore.GovernanceAuditQuery`.

No-Regression Evidence: `go test ./internal/storage/postgres/governance/audit/... -count=1`
covers Append/List/Summary/SummaryForTenant/DeleteExpired, the #6574
unknown-enum tolerance, and the #3717 tenant-isolation regressions;
`go test ./internal/storage/postgres/... -run TestBootstrapDefinitionsIncludeGovernanceAuditEvents -count=1`
covers the root-kept bootstrap-registry assertion. The SQL text, validation,
and event-id derivation are byte-identical to the pre-move code.

## Related docs

- [Postgres storage](../../README.md)
- [Shared database contracts](../../db/README.md)

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/governance/audit/... -count=1
go vet ./internal/storage/postgres/governance/audit/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
