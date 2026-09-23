# Postgres incident-repository correlation reads

## Purpose

This package supplies the two Postgres-facing ports the incident-repository
correlation reducer domain (#2161) needs: the applied PagerDuty
service-routing loader and the backend-to-repository resolver adapter. The
reducer's pure builder turns their output into a durable
`reducer_incident_repository_correlation` edge, or a provenance-only rejected
decision when a signal is weak.

## Ownership boundary

This package owns `ListAppliedPagerDutyServiceRoutingQuery` and
`PostgresAppliedPagerDutyServiceRoutingLoader`, plus
`BackendRepositoryResolverAdapter`'s translation of
`tfstatebackend.Resolver` errors into the reducer's
`incident.BackendRepositoryResolution` shape. The parent `postgres` package
keeps the fact-table schema and its indexes (including the partial index this
query depends on); `internal/reducer/incident` owns the correlation domain
types, ports, and pure builder; `internal/relationships/tfstatebackend` owns
backend-to-repository resolution itself.

## Exported surface

- `ListAppliedPagerDutyServiceRoutingQuery`, the permanent raw-SQL query
  (Decision #4683).
- `PostgresAppliedPagerDutyServiceRoutingLoader` with
  `LoadAppliedPagerDutyServiceRouting`, implementing
  `incident.AppliedPagerDutyServiceRoutingLoader`.
- `BackendRepositoryResolverAdapter` with `ResolveBackendRepository`,
  implementing `incident.BackendRepositoryResolver`.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the shared `Queryer` contract.
- `internal/facts` for the `IncidentRoutingAppliedPagerDutyResourceFactKind`
  compile-time lockstep reference.
- `internal/reducer/incident` for the correlation domain types and ports this
  package implements.
- `internal/relationships/tfstatebackend` for backend-to-repository
  resolution.

## Telemetry

None. This package executes one bounded SQL query and one resolver call
through injected dependencies; the reducer's correlation runner owns
observability for the domain.

## Gotchas / invariants

- `ListAppliedPagerDutyServiceRoutingQuery`'s `resource_class='service'`
  predicate and `provider_object_id` ordering must stay in the query text
  verbatim: they are what makes the partial expression index
  `fact_records_incident_routing_applied_service_idx` eligible. See
  `docs/internal/design/4683-incident-routing-sql-decision.md` for the
  EXPLAIN ANALYZE evidence (indexed: 1.121ms/459 buffers; decode-in-Go
  equivalent: 11.454ms/1553 buffers, Seq Scan).
- A row with a null `provider_object_id` is returned with a blank
  `ProviderObjectID`, never dropped, so the builder can record it as
  provenance-only rejected.
- `BackendRepositoryResolverAdapter` maps `ErrNoConfigRepoOwnsBackend` to a
  blank (unresolved) resolution and `ErrAmbiguousBackendOwner` to
  `Ambiguous: true`; any other resolver error propagates.
- Do not import the parent `postgres` package: that is an import cycle.
  `internal/storage/postgres/incident_routing_sql_schema_lockstep_test.go`
  exercises `ListAppliedPagerDutyServiceRoutingQuery` through this exported
  path from the root package.

No-Observability-Change: this extraction moves only the applied-routing
loader, its query text, and the backend-resolver adapter. The query,
predicates, and resolver error mapping are byte-identical; no metric, span,
or log name changes.

No-Regression Evidence: focused package tests plus the reducer's compile-time
port assertions run green on the moved tree; the SQL text and resolver logic
are unchanged so no plan or correlation-outcome proof is re-owed.

## Related docs

- [Postgres storage](../README.md)
- [Shared database contracts](../db/README.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/... -count=1` and
`go vet ./internal/storage/postgres/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
