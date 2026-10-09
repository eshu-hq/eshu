# AGENTS.md — go/internal/collector/repo/git/membership

## Read first

1. `go/internal/collector/repo/git/membership/README.md` — evaluation rules and telemetry
2. `go/internal/collector/repo/git/membership/doc.go` — the package contract
3. `go/internal/storage/postgres/membership/README.md` — the SQL this package mirrors
4. `go/internal/collector/repo/git/README.md` — where the native selector calls `Observe`

## The rules that shape this package

- This package is a leaf. `git` imports it; it must never import `git`.
- Do not rename it to `selection`: the dirgate naming rule would then demand
  that every `selection_*.go` file in `git/` move into it.
- It records evidence only. Do not add scope deletion, hiding, retirement, or
  graph writes here. The one deletion is the #7774 sweep of observation rows
  past their window plus `ExpiredObservationGrace`, run once per cycle on the
  request marked `SweepExpired` after an evaluated or guard-tripped outcome.
  It never deletes `not_listed` rows: the mass-miss guard reads them, and
  deleting them can hold a recovered selector's guard tripped forever. Do not
  remove that exclusion, and keep exactly one `SweepExpired` request per cycle.
- A sweep failure is the `expired_sweep` failure class and never changes the
  cycle's outcome.
- The state set, `Live`, and the uniform `Confirmed` rule live in
  `go/internal/scope/selection`, shared with the freshness `not_selected`
  verdict. Change them there, never by redefining them here.
- `project` and the Postgres upsert implement the same counter math. Change
  both together, and keep the live store parity test green.
- The three rails (complete listing only, mass-miss guard, confirmation over
  at least two cycles and `ConfirmationMinSpan`) protect against a false
  `not_selected`. Do not loosen one without
  a test that shows the false positive it can no longer produce.
- Changing the selector identity encoding changes every selector id and orphans
  existing rows. Bump the version field deliberately and say so in the PR.
- `Observer.Observe` never returns an error. A store failure must stay a WARN
  log plus `store_error`, never a failed collector cycle.

## Telemetry

Outcome and state values live in
`go/internal/telemetry/instruments_repository_selection.go`. A new outcome or
state needs the docs rows in `docs/public/reference/telemetry/` and
`docs/public/observability/telemetry-coverage.md` in the same change.

## Verification

```bash
cd go && go test ./internal/collector/repo/git/... -count=1
cd go && go test -race ./internal/collector/repo/git/membership -count=1
```
