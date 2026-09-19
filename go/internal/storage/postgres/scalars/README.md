# internal/storage/postgres/scalars

Shared null/blank value shaping for the Postgres store families: `Blank`,
`NullTime`, `NullTimePtr`, and `TimePtrFromNull`.

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
