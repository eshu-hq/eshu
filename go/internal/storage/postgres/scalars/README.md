# internal/storage/postgres/scalars

Shared null/blank value shaping for the Postgres store families: `Blank`,
`NullTime`, `NullTimePtr`, `TimePtrFromNull`, `DurationFromSeconds`,
`NullableTimeUTC`, `NullableTime`, `StringMapToAny`, and `CleanStringSet`.

## Why this package exists

The tenant grant store move under #6693 hoisted these four helpers into the
`db` contract leaf so the new `tenantstore` leaf and the families still in
the root could share them without one family importing another. That broke
`db`'s interfaces-only invariant, so they moved here: a cycle-free leaf with
the same stdlib-only, no-SQL-text, no-I/O shape as `pgarray`.

## Exported surface

- `Blank(value)` -- reports whether a string carries no non-space content.
  Stores use it to reject empty identifiers and scopes before SQL runs.
- `NullTime(value)` -- maps a possibly-zero time to `sql.NullTime`.
- `NullTimePtr(value)` -- maps a possibly-nil timestamp pointer to
  `sql.NullTime`, normalizing present values to UTC.
- `TimePtrFromNull(value)` -- maps `sql.NullTime` back to a timestamp
  pointer, normalizing present values to UTC.
- `DurationFromSeconds(value)` -- converts a non-negative seconds reading
  into a `time.Duration`, clamping a non-positive value to zero. Hoisted
  byte-identically from root's `status.go` under #6693.
- `NullableTimeUTC(value)` -- maps `sql.NullTime` to a zero `time.Time` when
  invalid, UTC when present. Hoisted from root's `status_aws_cloud.go`.
- `NullableTime(value)` -- binds a zero `time.Time` as SQL NULL and
  normalizes any other value to UTC before binding. Hoisted from root's
  `workflow_control_helpers.go`.
- `StringMapToAny(input)` -- widens a `map[string]string` to
  `map[string]any` for JSON payload marshaling, or nil when empty. Hoisted
  from root's `ingestion_queries.go`.
- `CleanStringSet(values)` -- trims, drops empties, and deduplicates a string
  slice while preserving first-seen order. Hoisted from root's
  `aws_cloud_runtime_drift_findings.go`, shared by the aws/multi/terraform
  drift-finding filters.

## Invariants

- Standard library only. Importing the parent `postgres` package (directly
  or transitively) would recreate the cycle this leaf exists to prevent.
- No SQL text, no I/O, no telemetry. Pure value shaping.
- Semantics are frozen: validators and scan loops across families depend on
  the zero/nil/UTC mappings. Change them with the families' tests.

## Verification

```bash
cd go && go test ./internal/storage/postgres/scalars -count=1
```
