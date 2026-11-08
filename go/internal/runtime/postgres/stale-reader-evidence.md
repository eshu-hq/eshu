# Stale reader: no-regression evidence (#10416 slice 0)

The `StaleReader` mechanism in this directory adds an opt-in standby
read path with no production caller. This note records why it cannot
regress production: routing defaults to `off`, the stale path is
unreachable unless explicitly wired, and a wiring guard blocks the
one dangerous misuse (fencing stores built from a reader).

## No-Regression Evidence:

- Baseline: new mechanism, no prior production load. `StaleReader()`
  is referenced only by its own tests; no `go/cmd` wiring constructs
  it, so the steady-state statement delta is zero.
- After: identical to baseline. With routing `off` (the default) every
  borrow attempt defers before touching any pool or connection, and the
  off-path borrows nothing. Enabling the path requires both
  `ESHU_POSTGRES_READ_ROUTING=stale-audits` and explicit wiring at a
  call site; neither ships in this slice.
- Backend/version: package unit proof with a fake database driver
  (`go test ./internal/runtime/postgres/`: 178 top-level PASS, 0 FAIL,
  covering serve-fresh, budget-exceeded, not-standby, never-replayed,
  split-brain, routing-off, no-observer, snapshot-set absence, fleet
  failover and all-lagged deferral, fixed error string, config parses,
  guard RED/GREEN including var-declared and nested aliases, OTEL
  closed labels); `go test -race` on the touched packages, exit 0.
- Input shape and counts: no production query changes shape; no
  row-count, retry, backoff, or queue change. The intended behavior
  change is the new mechanism itself, shown by the passing suite, not
  a timing shift.
- Why safe: the fencing borrow path is preserved by an exact split
  (reviewed), the stale store exposes no snapshot-set or Exec surface,
  invalid routing values fail startup, and deferral never falls back
  to the writer (which would return shed load under pressure).

## Observability Evidence:

- New counter `eshu_dp_postgres_reader_stale_deferrals_total`,
  labelled by closed family names only (no SQL, endpoints, or
  credential detail in labels or wire text).
- Family, replay age, and budget travel through `errors.As` for logs,
  never as metric labels.
- Existing reader/writer instruments are untouched; with zero callers
  the new counter can only report zero until a later slice wires one.

## No-Regression Evidence: runFleet tries the next member on a stale
deferral (#10416 slice-1 follow-up)

- Baseline: `TestFleetWrappedDeferralTriesNextMember` FAIL — a
  probe-wrapped `StaleDeferral` from member 0 returned as the fleet
  error with the sibling untried (tries: member 0 = 1, member 1 = 0).
- After: PASS — member 1 serves (tries: member 0 = 1, member 1 = 1).
  Only the fleet member-selection branch changed; no SQL, driver, or
  pgx behavior changed, so no new live proof is required for the delta.
- Backend/version: hermetic package unit proof, no live database.
  `go test -count=1 ./internal/runtime/postgres/` ok (2.5s),
  `./cmd/reducer/` ok (12.6s); `go test -race` on the fleet and wiring
  tests ok; `go vet`, `gofmt`, `git diff --check` clean.
- Input shape and counts: 2-member fleet, 4 connections each,
  `replayTimeout` 1s; member 0 refuses with a `fmt.Errorf` `%w`-wrapped
  `StaleDeferral`, exactly as standby probes wrap it. No production
  query changes shape; no row-count, retry, backoff, or queue change.
- Why safe: the new branch mirrors the merged single-borrow path
  line-for-line (join first so all-defer still returns a typed
  `StaleDeferral` via joined-error traversal, continue before the
  shared-failure return); the reservation is released before `continue`
  (no lease leak); the loop stays bounded by the existing
  `replayTimeout`; terminal error semantics for the audit layer are
  unchanged (the `StaleReadDeferred` check still resolves).

## No-Observability-Change: runFleet deferral handoff

- No new metrics, spans, or logs. The existing per-attempt member
  telemetry and `ObserveStaleDeferral` cover the new path: a tick where
  member 0 defers and member 1 serves records the same single deferral
  observation as before, with the outcome changed from error to success
  (the point of the fix; operator-visible as fewer stale-read errors on
  single-lag ticks, not as new telemetry).
