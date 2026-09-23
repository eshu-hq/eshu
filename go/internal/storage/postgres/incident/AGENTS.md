# AGENTS.md — Postgres incident-repository correlation reads guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for Postgres storage conventions.
3. `repository_correlation_loader.go` for the loader and resolver adapter.
4. `../incident_routing_sql_schema_lockstep_test.go` (kept at root: it also
   reads root-private `serviceIncidentEvidenceQuery`) for the payload-usage
   manifest lockstep proof over `ListAppliedPagerDutyServiceRoutingQuery`.

## Invariants

- Keep `ListAppliedPagerDutyServiceRoutingQuery` byte-identical when moving or
  refactoring code; its `resource_class='service'` predicate and
  `provider_object_id` ordering are load-bearing for the partial expression
  index `fact_records_incident_routing_applied_service_idx` (Decision #4683).
  A predicate or ordering change needs its own issue with EXPLAIN ANALYZE
  evidence per `eshu-postgres-rigor`.
- Keep the query constant exported as `ListAppliedPagerDutyServiceRoutingQuery`
  in the same lockstep with the schema-declared-fields test in the root
  package; renaming or unexporting it breaks that compile.
- Keep the package clause as `package incidentstore`; callers import the
  `storage/postgres/incident` path without an alias.
- Never import the parent `postgres` package from here.
- A row with a null `provider_object_id` must still be returned (blank
  `ProviderObjectID`), never dropped.

## Common changes

- Change the query predicates only with the EXPLAIN ANALYZE evidence from
  `docs/internal/design/4683-incident-routing-sql-decision.md` refreshed and
  the payload-usage manifest lockstep test kept green.
- Change `BackendRepositoryResolverAdapter`'s error mapping only alongside the
  three resolver outcome tests (single owner, ambiguous, no owner).

## Failure modes

- Importing the parent `postgres` package creates an import cycle.
- Dropping the `resource_class='service'` predicate or the
  `provider_object_id` ordering silently forces a sequential scan.
- Unexporting or renaming `ListAppliedPagerDutyServiceRoutingQuery` breaks the
  root schema-lockstep test compile.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/... -count=1
go vet ./internal/storage/postgres/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
