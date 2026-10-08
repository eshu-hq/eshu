# Repository Selection Evidence

## Purpose

`internal/scope/selection` defines what a stored repository selection
observation means (#7625). The git collector writes one row per repository
scope and selector to `repository_selection_observations` (migration 163).
The repository freshness reader reads the rows back and reports a
`not_selected` verdict when the collector's org listing no longer selects the
repository.

Both sides import this package, so the state set, the liveness window, and the
two-cycle confirmation rule each have exactly one definition.

## Ownership boundary

This package owns the meaning of an observation. It does not read or write
Postgres (`internal/storage/postgres/membership` writes,
`internal/storage/postgres` reads for freshness), decide which scopes a
listing selects (`internal/collector/repo/git/membership`), or render the
verdict (`internal/status`, `internal/query/repository`).

| Rule | Definition |
|---|---|
| Live | `evaluated_at >= now - LiveIntervals * evaluation_interval` (`LiveIntervals = 3`). Rows with no positive interval are never live. |
| Confirmed | `state = not_listed AND unlisted_cycle_count >= 2 AND evaluated_at - first_unlisted_at >= evaluation_interval` |
| Excluded | `archived_excluded` or `rule_excluded` (immediate), or a confirmed `not_listed` |

`Summarize(rows, now)` keeps only live rows, then:

| Aggregate | When | Reason |
|---|---|---|
| `selected` | any live row is `selected` | empty |
| `not_selected` | no live `selected` row and every live row is Excluded | state of the newest evaluated live row, lowest selector id on a tie |
| `pending` | otherwise, so at least one live `not_listed` row is unconfirmed | `not_listed` |

With no live rows `Summarize` returns `false`, and the freshness verdict is
exactly what it was before #7625. Timestamps come from the deciding rows: the
live `selected` rows when the aggregate is `selected`, otherwise every live
row. `LastListedAt` and `EvaluatedAt` are the newest values; `UnlistedSince`
is the earliest `first_unlisted_at` among live `not_listed` rows and is zero
when the aggregate is `selected`.

## Exported surface

- `State` and its four constants: the stored per-selector states.
- `Observation`: one stored row.
- `LiveIntervals`, `Live`, `Confirmed`, `Excluded`: the row predicates.
- `Aggregate` and its three constants, `Summary`, `Summarize`: the
  scope-level outcome.

See `doc.go` for the contract.

## Dependencies

Standard library only. `internal/collector/repo/git/membership` (writer),
`internal/storage/postgres` (freshness reader), and `internal/status`
(verdict) import this package; it must import none of them.

## Telemetry

None. The collector records the selection gauge and counter; the freshness
reader records `eshu_dp_repository_freshness_query_duration_seconds` and
`eshu_dp_repository_freshness_query_errors_total`.

## Gotchas / invariants

- Do not copy these predicates into SQL or another package. The collector
  gauge and the freshness verdict must agree, and they only do while both
  call this package.
- A `false` from `Summarize` means "no evidence", not "selected".
- The state values match the migration 163 CHECK constraint.

## Related docs

- `go/internal/collector/repo/git/membership/README.md`
- `go/internal/storage/postgres/membership/README.md`
- `docs/public/reference/http-api/repositories-ingesters-bundles.md`
  (repository freshness route)
