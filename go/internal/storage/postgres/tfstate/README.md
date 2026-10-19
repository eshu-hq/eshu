# Postgres tfstate family

## Purpose

This package owns the Terraform-state backend-fact and drift-evidence reads
that back graph discovery (`internal/collector/terraformstate`), the reducer
drift wirings, and the root AWS runtime-drift consumer: backend candidates
(S3, local, Terragrunt), the canonical-backend locator query, prior-snapshot
metadata, git readiness, and the config/state/prior join that produces
per-address drift rows.

## Ownership boundary

This package owns its list-query constants, row scans, candidate assembly,
module-prefix resolution, and drift-row merging. It owns no schema, DDL, or
write path: `ingestion_scopes`, `scope_generations`, and `fact_records`
belong to the root package and the collectors that populate them. The root
`postgres` package keeps thin type aliases plus one forwarder
(`compat_tfstate.go`) until the remaining callers move; each entry is
deleted once its last caller has moved.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the `Queryer` contract.
- `internal/collector/terraformstate` for the discovery query/candidate
  types the readers implement.
- `internal/correlation/drift/tfconfigstate` for the drift row types.
- `internal/relationships/tfstatebackend` for commit anchors and resolvers.
- `internal/redact` and `internal/telemetry` for payload handling and
  loader instruments.
- `internal/storage/postgres/migrations` (tests only) for live-Postgres
  schema setup.

## Telemetry

The loader wraps `LoadDriftEvidence` in a tracer span when `Tracer` is set,
increments `DriftUnresolvedModuleCalls` plus a structured log per
unresolved module call when `Instruments` is wired, and logs WARN on payload
decode failures via `Logger`. All three are optional; nil disables emission.

No-Observability-Change: this move relocates the family byte-identical. No
metric, span, or log name changes.

## Gotchas / invariants

- SQL constants stay byte-identical on moves; predicate changes need their
  own issue with EXPLAIN proof per `eshu-postgres-rigor`.
- `flattenStateAttributes` must agree with the parser's
  `ctyValueToDriftString` at the leaf level (cross-package encoding test).
- Filter cleanup is `filters.CleanFactKinds` (issue #8552): do not
  reintroduce a local copy; the root imports this package, so a shared
  helper must live in the leaf.
- Parser-shape config fixtures are `fixtures.FixtureConfigParserRow` /
  `fixtures.FixtureConfigResourcesArray` (issue #8552); the remaining
  fixtures in this package's tests are tfstate-only shapes.
- Do not import the parent `postgres` package: that is an import cycle.

No-Regression Evidence: `go test ./internal/storage/postgres/...` green on
the moved tree (root plus new `tfstate` package, 101 tests discovered in
`tfstate`); SQL literals verified byte-identical to the pre-move tree.

## Related docs

- [Postgres storage](../../README.md)
- [Shared database contracts](../../db/README.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/... -count=1` and
`go vet ./internal/storage/postgres/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
