# Repository Selection Evidence

## Purpose

`internal/scope/selection` defines what a stored repository selection
observation means (#7625). The git collector writes one row per repository
scope and selector to `repository_selection_observations` (migration 166).
The repository freshness reader reads the rows back and reports a
`not_selected` verdict when the collector's org listing no longer selects the
repository.

Both sides import this package, so the state set, the liveness rule, and the
confirmation rule each have exactly one definition.

## Ownership boundary

This package owns the meaning of an observation. It does not read or write
Postgres (`internal/storage/postgres/membership` writes,
`internal/storage/postgres` reads for freshness), decide which scopes a
listing selects (`internal/collector/repo/git/membership`), or render the
verdict (`internal/status`, `internal/query/repository`).

| Rule | Definition |
|---|---|
| Live | `evaluated_at + liveness_window >= now`. The writer stores its `ESHU_REPO_SELECTION_LIVENESS_WINDOW` on every row. Rows with no positive window are never live. |
| Confirmed | `state != selected AND state_cycle_count >= 2 AND evaluated_at - state_since >= ConfirmationMinSpan` (5 minutes). The same rule covers `not_listed`, `archived_excluded`, and `rule_excluded`. |

`Summarize(rows, now, latestGeneration)` keeps only the live rows L, then:

| Aggregate | When | Reason |
|---|---|---|
| `unknown` | L is empty | empty |
| `selected` | any row in L is `selected` | empty |
| `pending_confirmation` | no `selected` row and at least one row in L is not Confirmed | state of the newest evaluated live row, lowest selector id on a tie |
| `not_selected` | every row in L is Confirmed and G <= the latest `state_since` in L | as above |
| `excluded_still_ingested` | every row in L is Confirmed but G > the latest `state_since` in L | as above |

G is the newest `observed_at` over every generation of the scope.
`latestGeneration` supplies it and is called at most once, only after the
first three conditions hold, so a selected or pending scope never pays for the
read. `unknown` and `excluded_still_ingested` leave the freshness verdict
exactly as it was before #7625; only `not_selected` changes it.

Timestamps come from the deciding rows: the live `selected` rows when the
aggregate is `selected`, otherwise every live row. `LastListedAt` and
`EvaluatedAt` are the newest values. `StateSince` is the earliest live
selection when `selected`, otherwise the latest `state_since`: when the last
live selector stopped selecting, the value G is compared against.

## Exported surface

- `State` and its four constants: the stored per-selector states.
- `Observation`: one stored row.
- `ConfirmationMinSpan`, `Live`, `Confirmed`: the row predicates.
- `Aggregate` and its five constants, `Summary`, `Summarize`: the
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
- `unknown` means "no evidence", not "selected".
- The state values match the migration 166 CHECK constraint.

## Related docs

- `go/internal/collector/repo/git/membership/README.md`
- `go/internal/storage/postgres/membership/README.md`
- `docs/public/reference/http-api/repositories-ingesters-bundles.md`
  (repository freshness route)
