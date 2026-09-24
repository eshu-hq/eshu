# Postgres Terraform-state admin evidence

## Purpose

This package reads bounded Terraform-state admin evidence for the operator
status page: one row per `state_snapshot` scope keyed by safe locator hash
(the last observed serial), plus up to
`statuspkg.MaxTerraformStateRecentWarnings` recent `warning_fact` rows per
locator or Git backend-source handle.

## Ownership boundary

This package owns the two read-only admin-status statements
(`terraformStateLastSerialQuery`, `terraformStateRecentWarningsQuery`) and
the row-scan/assembly logic that turns them into
`statuspkg.TerraformStateLocatorSerial` / `TerraformStateLocatorWarning`
rows. It owns no schema, DDL, or write path: the underlying
`ingestion_scopes`, `scope_generations`, and `fact_records` tables belong to
the root package and the collectors that populate them. The parent
`postgres` package (`status.go`, still in root until its own `#6693` status
leaf) owns rendering this evidence into `statuspkg.RawSnapshot`.

## Exported surface

- `ReadTerraformStateAdminEvidence(ctx, queryer, limit, asOf)` runs both
  list queries and returns a `TerraformStateAdminEvidence`. It stays
  exported because the status family reads through it until the status
  leaf moves.
- `TerraformStateAdminEvidence` — `LastSerials` and `RecentWarnings`.

Everything else (`listTerraformStateLastSerials`,
`listTerraformStateRecentWarnings`, the two query constants) is
family-private.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the `Queryer` contract.
- `internal/status` for `TerraformStateLocatorSerial`,
  `TerraformStateLocatorWarning`, and `MaxTerraformStateRecentWarnings`.

## Telemetry

None. This package executes bounded, read-only SQL through the injected
queryer; the instrumented status-read handle in the root package owns
observability for the call.

## Gotchas / invariants

- Malformed `generation_id` rows (a non-numeric serial suffix) are skipped
  rather than failing the whole admin-status query: this is observability
  data, not a correctness-critical read.
- `listTerraformStateRecentWarnings` defaults `limit` to
  `statuspkg.MaxTerraformStateRecentWarnings` whenever the caller passes a
  non-positive value, so the result size is always hard-capped.
- Git-scope unresolved-backend warnings key on repo id plus repo-relative
  source path as the safe locator handle instead of a state locator; they
  never invent one.
- Do not import the parent `postgres` package: that is an import cycle.

No-Observability-Change: this extraction moves only the two read-only
Terraform-state admin-status queries and their Go reader, byte-identical.
No metric, span, or log name changes.

No-Regression Evidence: focused package tests (5 cases covering serial
parsing, malformed-row skipping, warning bounding, Git backend-expression
warnings, and the contract default limit) run green on the moved tree; the
SQL text is unchanged so no query-shape proof is re-owed.

## Related docs

- [Postgres storage](../../README.md)
- [Shared database contracts](../../db/README.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/... -count=1` and
`go vet ./internal/storage/postgres/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
